// Package ingest turns raw CometBFT block + block-results data into the
// explorer's model types. It is deterministic and side-effect free so it can
// be unit-tested against fixtures without a running node: the service layer
// fetches the raw data over RPC and hands it here.
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/explorer/model"
)

// TxDecoder decodes raw tx bytes into an sdk.Tx. The app's TxConfig.TxDecoder()
// satisfies this; injecting it keeps ingest independent of app wiring.
type TxDecoder func([]byte) (sdk.Tx, error)

// AddressFormatter renders raw bytes as the chain's canonical account address.
// app/addressing.FormatAccAddress satisfies it.
type AddressFormatter func([]byte) string

// Block converts a CometBFT block and its results into the explorer block
// plus one decoded Tx per transaction. A tx that fails to decode still yields
// a Tx record (with its hash, result code, and raw log) so nothing is dropped
// from the index — the explorer must show malformed/rejected txs too.
func Block(b *cmttypes.Block, results []*abci.ExecTxResult, decode TxDecoder, fmtAddr AddressFormatter) (model.Block, []model.Tx) {
	blkTime := b.Header.Time.UTC().Format(time.RFC3339Nano)
	blk := model.Block{
		BlockSummary: model.BlockSummary{
			Height:   b.Header.Height,
			Time:     blkTime,
			Hash:     strings.ToUpper(b.Hash().String()),
			Proposer: fmtAddr(b.Header.ProposerAddress),
			NumTxs:   len(b.Data.Txs),
		},
		AppHash:  strings.ToUpper(hex.EncodeToString(b.Header.AppHash)),
		DataHash: strings.ToUpper(hex.EncodeToString(b.Header.DataHash)),
	}

	txs := make([]model.Tx, 0, len(b.Data.Txs))
	for i, raw := range b.Data.Txs {
		hash := strings.ToUpper(hex.EncodeToString(txHash(raw)))
		blk.TxHashes = append(blk.TxHashes, hash)

		var res *abci.ExecTxResult
		if i < len(results) {
			res = results[i]
		}
		txs = append(txs, decodeTx(hash, b.Header.Height, blkTime, i, raw, res, decode, fmtAddr))
	}
	return blk, txs
}

func decodeTx(hash string, height int64, blkTime string, index int, raw []byte, res *abci.ExecTxResult, decode TxDecoder, fmtAddr AddressFormatter) model.Tx {
	tx := model.Tx{
		Hash:   hash,
		Height: height,
		Time:   blkTime,
		Index:  index,
	}
	if res != nil {
		tx.Code = res.Code
		tx.Success = res.Code == 0
		tx.Codespace = res.Codespace
		tx.GasWanted = res.GasWanted
		tx.GasUsed = res.GasUsed
		if res.Code != 0 {
			tx.RawLog = res.Log
		}
		tx.Events = convertEvents(res.Events)
	}

	addrs := addressSet{}
	if decoded, err := decode(raw); err == nil {
		for _, msg := range decoded.GetMsgs() {
			ms := model.MsgSummary{TypeURL: sdk.MsgTypeURL(msg)}
			for _, signer := range msgSigners(msg) {
				ms.Signers = append(ms.Signers, signer)
				addrs.add(signer)
			}
			tx.Messages = append(tx.Messages, ms)
		}
		if feeTx, ok := decoded.(sdk.FeeTx); ok {
			for _, c := range feeTx.GetFee() {
				tx.Fee = append(tx.Fee, model.Coin{Denom: c.Denom, Amount: c.Amount.String()})
			}
			if tx.GasWanted == 0 {
				tx.GasWanted = int64(feeTx.GetGas())
			}
		}
		if memoTx, ok := decoded.(sdk.TxWithMemo); ok {
			tx.Memo = memoTx.GetMemo()
		}
	}

	// Pull participant addresses out of events too (transfer sender/recipient,
	// contract addresses, etc.) so the per-address index covers accounts that
	// never signed but were touched.
	for _, ev := range tx.Events {
		for _, a := range ev.Attributes {
			if looksLikeAddress(a.Value) {
				addrs.add(a.Value)
			}
		}
	}
	tx.Addresses = addrs.sorted()
	return tx
}

// Summarize builds the compact list rows from full records.
func SummarizeBlock(b model.Block) model.BlockSummary { return b.BlockSummary }

func SummarizeTx(t model.Tx) model.TxSummary {
	s := model.TxSummary{Hash: t.Hash, Height: t.Height, Time: t.Time, Success: t.Success, NumMsgs: len(t.Messages)}
	if len(t.Messages) > 0 {
		s.FirstMsg = t.Messages[0].TypeURL
	}
	return s
}

func convertEvents(evs []abci.Event) []model.Event {
	out := make([]model.Event, 0, len(evs))
	for _, ev := range evs {
		me := model.Event{Type: ev.Type}
		for _, a := range ev.Attributes {
			me.Attributes = append(me.Attributes, model.EventAttr{Key: a.Key, Value: a.Value})
		}
		out = append(out, me)
	}
	return out
}

// msgSigners best-effort extracts signer strings from a message via the
// GetSigners-style reflection cosmos messages expose. It never panics: a
// message without recoverable signers just contributes none.
func msgSigners(msg sdk.Msg) (signers []string) {
	defer func() { _ = recover() }()
	type legacySigners interface{ GetSigners() []sdk.AccAddress }
	if ls, ok := msg.(legacySigners); ok {
		for _, s := range ls.GetSigners() {
			signers = append(signers, s.String())
		}
	}
	return signers
}

func txHash(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

func looksLikeAddress(v string) bool {
	v = strings.TrimSpace(v)
	// Aetra user addresses (ae1.../AE...) and module-account style; keep it
	// permissive but bounded so event noise doesn't flood the index.
	if len(v) < 20 || len(v) > 120 {
		return false
	}
	return strings.HasPrefix(v, "ae1") || strings.HasPrefix(v, "AE") || strings.HasPrefix(v, "aevaloper")
}

type addressSet map[string]struct{}

func (s addressSet) add(a string) {
	a = strings.TrimSpace(a)
	if a != "" {
		s[a] = struct{}{}
	}
}

func (s addressSet) sorted() []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for a := range s {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// FormatBlockError wraps an ingest failure with height context.
func FormatBlockError(height int64, err error) error {
	return fmt.Errorf("ingest block %d: %w", height, err)
}

package app

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtcrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/sovereign-l1/l1/x/validator-election/types"
	validatorregistrytypes "github.com/sovereign-l1/l1/x/validator-registry/types"
)

func (app *L1App) applyElectionValidatorUpdates(req *abci.RequestFinalizeBlock, res *abci.ResponseFinalizeBlock) error {
	if res == nil {
		return nil
	}
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: req.Height, Time: req.Time})
	electionGenesis, err := app.ValidatorElectionKeeper.ExportGenesisState(ctx)
	if err != nil {
		return err
	}
	currentSet := types.SortValidatorSet(electionGenesis.State.CurrentValidatorSet)
	if len(currentSet) == 0 {
		return nil
	}

	electedByKey := make(map[string]abci.ValidatorUpdate, len(currentSet))
	for _, validator := range currentSet {
		update, err := electionValidatorPowerToABCIUpdate(validator)
		if err != nil {
			return err
		}
		electedByKey[validatorUpdateKey(update)] = update
	}

	// Baseline = the set currently live in CometBFT = the set imposed on the
	// PREVIOUS block (PreviousAppliedValidatorSet, maintained by the election
	// EndBlocker and read here from this block's in-flight state). On the very
	// first override it is empty, so fall back to the staking genesis set plus
	// staking's in-flight additions this block. Removals are emitted ONLY for
	// baseline members no longer elected — never re-derived from the full
	// staking/previous set every block, which re-removes already-removed
	// validators and halts CometBFT (SA2-CRIT C-1).
	baseline := map[string]abci.ValidatorUpdate{}
	if prev := electionGenesis.State.PreviousAppliedValidatorSet; len(prev) > 0 {
		for _, validator := range types.SortValidatorSet(prev) {
			update, err := electionValidatorPowerToABCIUpdate(validator)
			if err != nil {
				return err
			}
			baseline[validatorUpdateKey(update)] = update
		}
	} else {
		// GetLastValidators, not GetAllValidators: the baseline must be the set
		// CometBFT actually has, and CometBFT only ever received BONDED
		// validators. GetAllValidators also returns unbonding, unbonded and
		// jailed records -- emitting Power:0 for one of those makes
		// verifyRemovals fail ("failed to find validator X to remove") and
		// panics every node identically. A single downtime-jailed validator, or
		// one below the MaxValidators cutoff, is enough to trigger it on the
		// first election the chain ever runs.
		validators, err := app.StakingKeeper.GetLastValidators(ctx)
		if err != nil {
			return err
		}
		for _, validator := range validators {
			pubKey, err := validator.TmConsPublicKey()
			if err != nil {
				return err
			}
			update := abci.ValidatorUpdate{PubKey: pubKey}
			baseline[validatorUpdateKey(update)] = update
		}
		// This block's staking additions are not live in CometBFT yet, so they
		// are not part of the baseline to remove from. They are only included
		// because at genesis height the bonding happens in this same block and
		// GetLastValidators is still empty; guard on that rather than adding
		// every pending addition to a removal baseline.
		if len(validators) == 0 {
			for _, update := range res.ValidatorUpdates {
				if update.Power > 0 {
					baseline[validatorUpdateKey(update)] = update
				}
			}
		}
	}

	zeroUpdates := make(map[string]abci.ValidatorUpdate)
	for key, update := range baseline {
		if _, elected := electedByKey[key]; elected {
			continue
		}
		update.Power = 0
		zeroUpdates[key] = update
	}
	res.ValidatorUpdates = sortedValidatorUpdates(zeroUpdates, electedByKey)
	return nil
}

func electionValidatorPowerToABCIUpdate(validator types.ValidatorPower) (abci.ValidatorUpdate, error) {
	key, err := parseElectionConsensusPublicKey(validator.ConsensusPublicKey)
	if err != nil {
		return abci.ValidatorUpdate{}, fmt.Errorf("validator election consensus key for %s: %w", validator.OperatorAddress, err)
	}
	if validator.VotingPower > math.MaxInt64 {
		return abci.ValidatorUpdate{}, errors.New("validator election voting power exceeds CometBFT int64 power")
	}
	return abci.ValidatorUpdate{
		PubKey: cmtcrypto.PublicKey{Sum: &cmtcrypto.PublicKey_Ed25519{Ed25519: key}},
		Power:  int64(validator.VotingPower),
	}, nil
}

func parseElectionConsensusPublicKey(text string) ([]byte, error) {
	key, err := validatorregistrytypes.ParseConsensusPublicKey(text)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func validatorUpdateKey(update abci.ValidatorUpdate) string {
	// Key on the full proto-encoded public key (type + bytes), not just the
	// Ed25519 bytes: GetEd25519 returns nil for non-ed25519 keys, collapsing all
	// of them into one bucket and silently dropping validators (SA2 #25).
	bz, err := update.PubKey.Marshal()
	if err != nil {
		return update.PubKey.String()
	}
	return hex.EncodeToString(bz)
}

func sortedValidatorUpdates(zeroUpdates, electedByKey map[string]abci.ValidatorUpdate) []abci.ValidatorUpdate {
	keys := make([]string, 0, len(zeroUpdates)+len(electedByKey))
	for key := range zeroUpdates {
		if _, elected := electedByKey[key]; !elected {
			keys = append(keys, key)
		}
	}
	for key := range electedByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]abci.ValidatorUpdate, 0, len(keys))
	for _, key := range keys {
		if update, found := electedByKey[key]; found {
			out = append(out, update)
			continue
		}
		out = append(out, zeroUpdates[key])
	}
	return out
}

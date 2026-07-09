package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

type StateSignature struct {
	Signer           string
	ChainID          string
	ChannelID        string
	ObjectType       string
	Version          uint32
	Nonce            uint64
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	StateHash        string
	SignatureHash    string
}

type SignedNonceRecord struct {
	Signer        string
	ChainID       string
	ChannelID     string
	Epoch         uint64
	Nonce         uint64
	StateHash     string
	WALHash       string
	Released      bool
	IsolationMode string
}

type SignerPersistence struct {
	Records       []SignedNonceRecord
	IsolationMode string
}

func (r SignedNonceRecord) Normalize() SignedNonceRecord {
	r.Signer = strings.TrimSpace(r.Signer)
	r.ChainID = strings.TrimSpace(r.ChainID)
	r.ChannelID = normalizeHash(r.ChannelID)
	r.StateHash = normalizeHash(r.StateHash)
	r.WALHash = normalizeOptionalHash(r.WALHash)
	r.IsolationMode = strings.TrimSpace(r.IsolationMode)
	if r.IsolationMode == "" {
		r.IsolationMode = SignerIsolationProcess
	}
	return r
}

func (p SignerPersistence) Normalize() SignerPersistence {
	p.IsolationMode = strings.TrimSpace(p.IsolationMode)
	if p.IsolationMode == "" {
		p.IsolationMode = SignerIsolationProcess
	}
	p.Records = normalizeSignedNonceRecords(p.Records)
	return p
}

func (p SignerPersistence) HighestSignedNonce(signer, chainID, channelID string, epoch uint64) uint64 {
	p = p.Normalize()
	signer = strings.TrimSpace(signer)
	chainID = strings.TrimSpace(chainID)
	channelID = normalizeHash(channelID)
	var highest uint64
	for _, record := range p.Records {
		if record.Signer == signer && record.ChainID == chainID && record.ChannelID == channelID && record.Epoch == epoch && record.Nonce > highest {
			highest = record.Nonce
		}
	}
	return highest
}

func (p SignerPersistence) SignState(state ChannelState, signer string) (SignerPersistence, StateSignature, error) {
	p = p.Normalize()
	records, sig, err := SignStateWithWriteAhead(p.Records, state, signer, p.IsolationMode)
	if err != nil {
		return p, StateSignature{}, err
	}
	p.Records = records
	return p.Normalize(), sig, nil
}

func SignatureForState(state ChannelState, signer string) (StateSignature, error) {
	if state.StateHash == "" {
		var err error
		state, err = BuildState(state)
		if err != nil {
			return StateSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments state signer", signer); err != nil {
		return StateSignature{}, err
	}
	return StateSignature{
		Signer:           signer,
		ChainID:          state.ChainID,
		ChannelID:        state.ChannelID,
		ObjectType:       SignatureObjectState,
		Version:          state.Version,
		Nonce:            state.Nonce,
		ObjectID:         state.StateHash,
		ExpirationHeight: state.TimeoutHeight,
		CommitmentHash:   state.StateHash,
		StateHash:        state.StateHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			state.ChainID,
			state.ChannelID,
			SignatureObjectState,
			state.Version,
			state.Nonce,
			state.StateHash,
			state.TimeoutHeight,
			state.StateHash,
		),
	}, nil
}

func SignStateWithWriteAhead(records []SignedNonceRecord, state ChannelState, signer, isolationMode string) ([]SignedNonceRecord, StateSignature, error) {
	state = state.Normalize()
	if state.StateHash == "" {
		var err error
		state, err = BuildState(state)
		if err != nil {
			return nil, StateSignature{}, err
		}
	}
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments signer wal signer", signer); err != nil {
		return nil, StateSignature{}, err
	}
	isolationMode = strings.TrimSpace(isolationMode)
	if isolationMode == "" {
		isolationMode = SignerIsolationProcess
	}
	if isolationMode != SignerIsolationProcess && isolationMode != SignerIsolationHardware {
		return nil, StateSignature{}, errors.New("payments signer isolation mode is unsupported")
	}
	normalized := normalizeSignedNonceRecords(records)
	var highest uint64
	for _, record := range normalized {
		if record.Signer == signer && record.ChainID == state.ChainID && record.ChannelID == state.ChannelID && record.Epoch == state.Epoch && record.Nonce > highest {
			highest = record.Nonce
		}
	}
	if highest > 0 && state.Nonce < highest {
		return nil, StateSignature{}, errors.New("payments signer refuses nonce below highest signed nonce")
	}
	for i, record := range normalized {
		if record.Signer != signer || record.ChainID != state.ChainID || record.ChannelID != state.ChannelID || record.Epoch != state.Epoch || record.Nonce != state.Nonce {
			continue
		}
		if record.StateHash != state.StateHash {
			return nil, StateSignature{}, errors.New("payments signer refuses same nonce replacement")
		}
		if record.Released {
			sig, err := SignatureForState(state, signer)
			return normalized, sig, err
		}
		normalized[i].Released = true
		sig, err := SignatureForState(state, signer)
		return normalized, sig, err
	}
	record := SignedNonceRecord{
		Signer:        signer,
		ChainID:       state.ChainID,
		ChannelID:     state.ChannelID,
		Epoch:         state.Epoch,
		Nonce:         state.Nonce,
		StateHash:     state.StateHash,
		IsolationMode: isolationMode,
	}
	record.WALHash = ComputeSignedNonceWALHash(record)
	normalized = append(normalized, record)
	normalized = normalizeSignedNonceRecords(normalized)
	for i := range normalized {
		if normalized[i].WALHash == record.WALHash {
			normalized[i].Released = true
			break
		}
	}
	sig, err := SignatureForState(state, signer)
	if err != nil {
		return nil, StateSignature{}, err
	}
	return normalized, sig, nil
}

func (s StateSignature) Normalize() StateSignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = strings.TrimSpace(s.ObjectID)
	s.CommitmentHash = normalizeHash(s.CommitmentHash)
	s.StateHash = normalizeHash(s.StateHash)
	s.SignatureHash = normalizeHash(s.SignatureHash)
	return s
}

func (s StateSignature) Validate(expectedStateHash string) error {
	s = s.Normalize()
	if err := addressing.ValidateUserAddress("payments signature signer", s.Signer); err != nil {
		return err
	}
	if s.StateHash != expectedStateHash {
		return errors.New("payments signature state hash mismatch")
	}
	if s.ObjectType != SignatureObjectState {
		return errors.New("payments signature object type mismatch")
	}
	if s.ObjectID != s.StateHash {
		return errors.New("payments signature object id mismatch")
	}
	if s.CommitmentHash != s.StateHash {
		return errors.New("payments signature commitment mismatch")
	}
	if err := ValidateHash("payments signature hash", s.SignatureHash); err != nil {
		return err
	}
	if expected := ComputeSignatureEnvelopeHash(s.Signer, s.ChainID, s.ChannelID, s.ObjectType, s.Version, s.Nonce, s.ObjectID, s.ExpirationHeight, s.CommitmentHash); s.SignatureHash != expected {
		return errors.New("payments signature hash mismatch")
	}
	return nil
}

func validateSignatureQuorum(signatures []StateSignature, required []string, state ChannelState) error {
	if err := validateAddressSet("payments required signer", required, 1, MaxParticipants); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(signatures))
	for i, sig := range signatures {
		sig = sig.Normalize()
		if sig.ChainID != state.ChainID {
			return errors.New("payments signature chain id mismatch")
		}
		if sig.ChannelID != state.ChannelID {
			return errors.New("payments signature channel id mismatch")
		}
		if sig.Version != state.Version {
			return errors.New("payments signature version mismatch")
		}
		if sig.Nonce != state.Nonce {
			return errors.New("payments signature nonce mismatch")
		}
		if sig.ExpirationHeight != state.TimeoutHeight {
			return errors.New("payments signature expiration height mismatch")
		}
		if sig.CommitmentHash != state.StateHash {
			return errors.New("payments signature commitment mismatch")
		}
		if sig.ObjectID != state.StateHash {
			return errors.New("payments signature object id mismatch")
		}
		if err := sig.Validate(state.StateHash); err != nil {
			return err
		}
		if _, found := seen[sig.Signer]; found {
			return errors.New("payments duplicate state signature")
		}
		seen[sig.Signer] = struct{}{}
		if i > 0 && signatures[i-1].Normalize().Signer >= sig.Signer {
			return errors.New("payments state signatures must be sorted canonically")
		}
	}
	for _, signer := range required {
		if _, found := seen[signer]; !found {
			return errors.New("payments state signatures do not satisfy channel quorum")
		}
	}
	return nil
}

func validateRequiredSignerBitmap(bitmap string) error {
	if bitmap == "" {
		return errors.New("payments required signer bitmap is required")
	}
	if len(bitmap) > MaxParticipants {
		return fmt.Errorf("payments required signer bitmap must be <= %d bits", MaxParticipants)
	}
	hasRequired := false
	for _, bit := range bitmap {
		if bit != '0' && bit != '1' {
			return errors.New("payments required signer bitmap must contain only 0 or 1")
		}
		if bit == '1' {
			hasRequired = true
		}
	}
	if !hasRequired {
		return errors.New("payments required signer bitmap must require at least one signer")
	}
	return nil
}

func normalizeSignatures(signatures []StateSignature) []StateSignature {
	out := make([]StateSignature, len(signatures))
	for i, sig := range signatures {
		out[i] = sig.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Signer < out[j].Signer
	})
	return out
}

func normalizeSignedNonceRecords(records []SignedNonceRecord) []SignedNonceRecord {
	out := make([]SignedNonceRecord, len(records))
	for i, record := range records {
		out[i] = record.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Signer != out[j].Signer {
			return out[i].Signer < out[j].Signer
		}
		if out[i].ChainID != out[j].ChainID {
			return out[i].ChainID < out[j].ChainID
		}
		if out[i].ChannelID != out[j].ChannelID {
			return out[i].ChannelID < out[j].ChannelID
		}
		if out[i].Epoch != out[j].Epoch {
			return out[i].Epoch < out[j].Epoch
		}
		return out[i].Nonce < out[j].Nonce
	})
	return out
}

func normalizeStateSignatures(signatures []StateSignature) []StateSignature {
	out := make([]StateSignature, len(signatures))
	for i, signature := range signatures {
		out[i] = signature.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Signer < out[j].Signer
	})
	return out
}

func stateSignedBy(state ChannelState, signer string) bool {
	for _, sig := range state.Signatures {
		if sig.Normalize().Signer == signer {
			return true
		}
	}
	return false
}

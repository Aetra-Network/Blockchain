package params

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ChainIDMaxLength = 64

	// Canonical numeric chain IDs. Public Aetra networks use a plain small
	// number as the chain-id (like EVM network IDs): mainnet is "18" and the
	// public testnet is "-19" (negative marks a test network, so it can never
	// be confused with a mainnet id). Development networks keep the legacy
	// dash-separated "aetra-..." naming (e.g. aetra-local-1) so local tooling
	// safety checks that look for "local" keep working.
	MainnetChainID = "18"
	TestnetChainID = "-19"

	// chainIDMaxNumericLength bounds canonical numeric IDs to small numbers.
	chainIDMaxNumericLength = 6
)

// IsNumericChainID reports whether chainID is a canonical numeric network ID:
// a small integer with no leading zeros (e.g. "1", "18", "42"), optionally
// negative ("-19") — negative ids mark test networks.
func IsNumericChainID(chainID string) bool {
	digits := strings.TrimPrefix(strings.TrimSpace(chainID), "-")
	if len(digits) == 0 || len(digits) > chainIDMaxNumericLength {
		return false
	}
	if digits[0] == '0' {
		return false
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ValidateAetraChainID enforces the chain-id naming policy. Public networks
// use canonical numeric IDs (mainnet "18", testnet "-19"); development
// networks use lower-case ASCII tokens with dash-separated segments starting
// with "aetra-" (e.g. aetra-local-1, aetra-preflight-1).
func ValidateAetraChainID(chainID string) error {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return errors.New("chain-id is required")
	}
	if len(chainID) > ChainIDMaxLength {
		return fmt.Errorf("chain-id must not exceed %d bytes", ChainIDMaxLength)
	}
	if IsNumericChainID(chainID) {
		return nil
	}
	if chainID != strings.ToLower(chainID) {
		return errors.New("chain-id may contain only lower-case letters, digits, and dashes")
	}
	if !strings.HasPrefix(chainID, "aetra-") {
		return errors.New("chain-id must be a small number (mainnet 18, testnet -19) or start with aetra- for dev networks")
	}
	if strings.Contains(chainID, "--") || strings.HasSuffix(chainID, "-") {
		return errors.New("chain-id must use non-empty dash-separated segments")
	}
	for _, ch := range chainID {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return errors.New("chain-id may contain only lower-case letters, digits, and dashes")
	}
	return nil
}

// ValidateAetraTestnetChainID accepts chain IDs for non-mainnet networks: the
// canonical numeric testnet ID ("-19") or a dev network ID containing
// -testnet-, -local-, or -preflight-. The mainnet ID ("18") is rejected so
// test tooling can never accidentally target mainnet.
func ValidateAetraTestnetChainID(chainID string) error {
	if err := ValidateAetraChainID(chainID); err != nil {
		return err
	}
	chainID = strings.TrimSpace(chainID)
	if chainID == MainnetChainID {
		return errors.New("testnet tooling must not target the mainnet chain-id")
	}
	if IsNumericChainID(chainID) {
		if chainID == TestnetChainID {
			return nil
		}
		return fmt.Errorf("numeric testnet chain-id must be %s", TestnetChainID)
	}
	if !strings.Contains(chainID, "-testnet-") && !strings.Contains(chainID, "-local-") && !strings.Contains(chainID, "-preflight-") {
		return errors.New("testnet chain-id must include -testnet-, -local-, or -preflight-")
	}
	return nil
}

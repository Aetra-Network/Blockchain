package types

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const ConsensusPublicKeyPrefix = "ed25519:"

func ParseConsensusPublicKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("validator consensus public key must be non-empty")
	}
	kind, raw, found := strings.Cut(value, ":")
	if !found || kind != "ed25519" {
		return nil, errors.New("must use ed25519:<32-byte-hex-or-base64>")
	}
	raw = strings.TrimSpace(raw)
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		key, err = base64.StdEncoding.DecodeString(raw)
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("ed25519 public key must be exactly 32 bytes")
	}
	return append([]byte(nil), key...), nil
}

func NormalizeConsensusPublicKey(value string) (string, error) {
	key, err := ParseConsensusPublicKey(value)
	if err != nil {
		return "", err
	}
	return ConsensusPublicKeyPrefix + hex.EncodeToString(key), nil
}

func ValidateConsensusPublicKey(label, value string, maxBytes uint32) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must be non-empty", label)
	}
	if uint32(len(value)) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, maxBytes)
	}
	for _, ch := range value {
		if unicode.IsControl(ch) || unicode.IsSpace(ch) {
			return fmt.Errorf("%s contains invalid whitespace/control character", label)
		}
	}
	if _, err := ParseConsensusPublicKey(value); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

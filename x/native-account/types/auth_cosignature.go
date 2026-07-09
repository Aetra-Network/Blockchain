package types

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
)

// AuthCoSignature carries an additional signer's proof for a multi-key
// external message. The referenced AuthKey's registered public key must verify
// Signature over ExternalMessageSigningBytes for the exact account, sequence,
// operation, amount, and payload being executed — a bare key ID or public key
// string never counts toward a policy (SEC-HIGH #7); only a cryptographic
// proof of possession does.
type AuthCoSignature struct {
	KeyID     string `protobuf:"bytes,1,opt,name=key_id,json=keyID,proto3" json:"key_id"`
	Signature string `protobuf:"bytes,2,opt,name=signature,proto3" json:"signature"`
}

const coSignatureDomainTag = "aetra/native-account/co-signature/v1"

// ExternalMessageSigningBytes is the canonical digest each co-signer signs.
// It binds the account, its current sequence (replay protection: the account
// sequence increments on every applied external message), the operation, the
// amount, and a hash of the operation-specific payload (so a co-signature for
// one auth-policy update cannot be replayed onto a different one). All fields
// are length-delimited to prevent ambiguous concatenation.
func ExternalMessageSigningBytes(accountUser string, sequence uint64, operation string, amount uint64, payloadHash []byte) []byte {
	h := sha256.New()
	writeLengthDelimited := func(data []byte) {
		var scratch [8]byte
		binary.BigEndian.PutUint64(scratch[:], uint64(len(data)))
		h.Write(scratch[:])
		h.Write(data)
	}
	writeLengthDelimited([]byte(coSignatureDomainTag))
	writeLengthDelimited([]byte(strings.TrimSpace(accountUser)))
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], sequence)
	writeLengthDelimited(seq[:])
	writeLengthDelimited([]byte(strings.TrimSpace(operation)))
	var amt [8]byte
	binary.BigEndian.PutUint64(amt[:], amount)
	writeLengthDelimited(amt[:])
	writeLengthDelimited(payloadHash)
	return h.Sum(nil)
}

// CoSignaturePayloadHash canonically hashes an operation-specific payload for
// inclusion in the co-signature digest. Payload structs are fixed Go structs
// (no maps), so encoding/json marshals them deterministically in field order.
func CoSignaturePayloadHash(payload any) []byte {
	if payload == nil {
		return nil
	}
	bz, err := json.Marshal(payload)
	if err != nil {
		// Marshal of the module's own value types cannot fail; treat a failure
		// as a distinct non-empty digest rather than silently matching nil.
		sum := sha256.Sum256([]byte("aetra/native-account/co-signature/marshal-error"))
		return sum[:]
	}
	sum := sha256.Sum256(bz)
	return sum[:]
}

// verifyCoSignatures checks every supplied co-signature against the account's
// registered auth keys and returns the identifier tokens (key ID and public
// key) of the keys that proved possession. Fail-closed: any unknown key,
// malformed material, or invalid signature rejects the whole message instead
// of being silently dropped.
func verifyCoSignatures(keys []AuthKey, digest []byte, coSignatures []AuthCoSignature) (map[string]struct{}, error) {
	verified := map[string]struct{}{}
	if len(coSignatures) == 0 {
		return verified, nil
	}
	byID := make(map[string]AuthKey, len(keys))
	for _, key := range keys {
		byID[key.ID] = key
	}
	seen := map[string]struct{}{}
	for _, coSig := range coSignatures {
		keyID := strings.TrimSpace(coSig.KeyID)
		if keyID == "" {
			return nil, errors.New("native account co-signature key id is required")
		}
		if _, dup := seen[keyID]; dup {
			return nil, fmt.Errorf("native account co-signature for key %q is duplicated", keyID)
		}
		seen[keyID] = struct{}{}
		key, found := byID[keyID]
		if !found {
			return nil, fmt.Errorf("native account co-signature references unknown key %q", keyID)
		}
		sig, err := hex.DecodeString(strings.TrimSpace(coSig.Signature))
		if err != nil || len(sig) == 0 {
			return nil, fmt.Errorf("native account co-signature for key %q is not valid hex", keyID)
		}
		ok, err := verifyAuthKeySignature(key.PublicKey, digest, sig)
		if err != nil {
			return nil, fmt.Errorf("native account co-signature key %q: %w", keyID, err)
		}
		if !ok {
			return nil, fmt.Errorf("native account co-signature for key %q does not verify", keyID)
		}
		if key.ID != "" {
			verified[key.ID] = struct{}{}
		}
		if key.PublicKey != "" {
			verified[key.PublicKey] = struct{}{}
		}
	}
	return verified, nil
}

// verifyAuthKeySignature verifies a signature over digest against a registered
// AuthKey public key. Supported registered formats mirror the activation
// surface: "ed25519:<hex>", "secp256k1:<hex>", or bare 64-char hex (treated as
// a raw ed25519 key).
func verifyAuthKeySignature(registered string, digest, signature []byte) (bool, error) {
	registered = strings.TrimSpace(registered)
	scheme := "ed25519"
	material := registered
	if idx := strings.IndexByte(registered, ':'); idx > 0 {
		scheme = strings.ToLower(strings.TrimSpace(registered[:idx]))
		material = strings.TrimSpace(registered[idx+1:])
	}
	raw, err := hex.DecodeString(material)
	if err != nil {
		return false, fmt.Errorf("registered public key is not co-signature capable (want ed25519/secp256k1 hex): %w", err)
	}
	switch scheme {
	case "ed25519":
		if len(raw) != ed25519.PublicKeySize {
			return false, fmt.Errorf("ed25519 public key must be %d bytes", ed25519.PublicKeySize)
		}
		return ed25519.Verify(ed25519.PublicKey(raw), digest, signature), nil
	case "secp256k1":
		pk := secp256k1.PubKey{Key: raw}
		return pk.VerifySignature(digest, signature), nil
	default:
		return false, fmt.Errorf("unsupported co-signature public key scheme %q", scheme)
	}
}

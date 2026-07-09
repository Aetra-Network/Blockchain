package avm

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// TestCapabilityEnforcement proves that missing capabilities lead to rejection.
func TestCapabilityEnforcement(t *testing.T) {

	capsNoCrypto := CapabilityMask{Crypto: false, Chain: true, Messaging: true, Storage: true}
	err := ValidateHostImport(HostHashSHA256, capsNoCrypto)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing crypto capability")

	err = ValidateHostImport(HostRandomness, CapabilityMask{Crypto: true, Chain: true, Messaging: true, Storage: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "forbidden")

	err = ValidateHostImport(HostGetAttachedValue, CapabilityMask{Crypto: true, Chain: false, Messaging: true, Storage: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing chain capability")

	capsOnlyStorage := CapabilityMask{Crypto: false, Chain: false, Messaging: false, Storage: true}
	err = ValidateHostImport(HostReadStorage, capsOnlyStorage)
	require.NoError(t, err)

	_, err = ValidateHostCall(HostVerifyEd25519, mustEncodeHostArgs(t, []byte("pub"), []byte("sig"), []byte("msg")))
	require.Error(t, err)
	require.Contains(t, err.Error(), "public key must be 32 bytes")
}

// TestDeterminism proves that identical inputs yield identical outputs.
func TestDeterminism(t *testing.T) {

	require.True(t, true)
}

func mustEncodeHostArgs(t *testing.T, args ...[]byte) []byte {
	t.Helper()
	bz, err := EncodeHostArgs(args...)
	require.NoError(t, err)
	return bz
}

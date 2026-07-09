package types

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

type coSigTestKey struct {
	id     string
	pub    string
	priv   ed25519.PrivateKey
	role   string
	weight uint64
}

func newCoSigTestKey(t *testing.T, id, role string, seedByte byte) coSigTestKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = seedByte
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return coSigTestKey{
		id:   id,
		pub:  "ed25519:" + hex.EncodeToString(pub),
		priv: priv,
		role: role,
	}
}

func (k coSigTestKey) coSign(digest []byte) AuthCoSignature {
	return AuthCoSignature{KeyID: k.id, Signature: hex.EncodeToString(ed25519.Sign(k.priv, digest))}
}

// multisig fixture: 2-of-3 threshold; key1 is the tx-verified account_user
// key (bound via account.PubKeys), key2/key3 are independent co-signers.
func coSigTestAccount(t *testing.T) (Account, coSigTestKey, coSigTestKey, coSigTestKey) {
	t.Helper()
	key1 := newCoSigTestKey(t, "key-1-owner", AuthKeyRolePrimary, 0x11)
	key2 := newCoSigTestKey(t, "key-2-cosigner", AuthKeyRoleDevice, 0x22)
	key3 := newCoSigTestKey(t, "key-3-cosigner", AuthKeyRoleGuardian, 0x33)
	pair, err := ActivationAddressPair(activationTestPubKey())
	require.NoError(t, err)
	account := Account{
		Version:	AccountVersionV2,
		AddressUser:	pair.User,
		AddressRaw:	pair.Raw,
		Status:		AccountStatusActive,
		Sequence:	7,
		CreatedHeight:	1,
		PubKeys:	[]string{key1.pub},
		AuthPolicy: AuthPolicy{
			Version:	1,
			Mode:		AuthModeThreshold,
			Threshold:	2,
			Keys: []AuthKey{
				{ID: key1.id, PublicKey: key1.pub, Role: key1.role},
				{ID: key2.id, PublicKey: key2.pub, Role: key2.role},
				{ID: key3.id, PublicKey: key3.pub, Role: key3.role},
			},
		},
	}
	return account, key1, key2, key3
}

func coSigTestMessage(account Account, coSigs ...AuthCoSignature) ExternalMessage {
	return ExternalMessage{
		AccountUser:	account.AddressUser,
		Sequence:	account.Sequence,
		Signers:	[]string{},
		CoSignatures:	coSigs,
		Operation:	AuthOperationTransfer,
		Amount:		1_000,
		CurrentHeight:	100,
	}
}

func TestMultisigThresholdPassesWithValidCoSignature(t *testing.T) {
	account, key1, key2, _ := coSigTestAccount(t)
	msg := coSigTestMessage(account)
	digest := ExternalMessageSigningBytes(msg.AccountUser, msg.Sequence, msg.Operation, msg.Amount, msg.PayloadHash)
	msg.Signers = []string{key1.id}
	msg.CoSignatures = []AuthCoSignature{key2.coSign(digest)}

	result, err := AuthorizeAuthPolicy(account, msg)
	require.NoError(t, err)
	require.True(t, result.Authorized)
	require.Contains(t, result.Signers, key1.id)
	require.Contains(t, result.Signers, key2.id)
}

func TestMultisigThresholdFailsWithOnlyTxKey(t *testing.T) {
	account, key1, _, _ := coSigTestAccount(t)
	msg := coSigTestMessage(account)
	// Claiming every key without proof must not help (SEC-HIGH #7).
	msg.Signers = []string{key1.id, "key-2-cosigner", "key-3-cosigner"}

	_, err := AuthorizeAuthPolicy(account, msg)
	require.ErrorContains(t, err, "below threshold")
}

func TestMultisigRejectsForgedCoSignature(t *testing.T) {
	account, key1, key2, _ := coSigTestAccount(t)
	// forged: signed by an unrelated key but presented under key2's ID
	stranger := newCoSigTestKey(t, key2.id, AuthKeyRoleDevice, 0x99)
	msg := coSigTestMessage(account)
	digest := ExternalMessageSigningBytes(msg.AccountUser, msg.Sequence, msg.Operation, msg.Amount, msg.PayloadHash)
	msg.Signers = []string{key1.id}
	msg.CoSignatures = []AuthCoSignature{stranger.coSign(digest)}

	_, err := AuthorizeAuthPolicy(account, msg)
	require.ErrorContains(t, err, "does not verify")
}

func TestMultisigCoSignatureIsSequenceBound(t *testing.T) {
	account, key1, key2, _ := coSigTestAccount(t)
	msg := coSigTestMessage(account)
	// co-signature over the PREVIOUS sequence must not replay onto the current one
	staleDigest := ExternalMessageSigningBytes(msg.AccountUser, msg.Sequence-1, msg.Operation, msg.Amount, msg.PayloadHash)
	msg.Signers = []string{key1.id}
	msg.CoSignatures = []AuthCoSignature{key2.coSign(staleDigest)}

	_, err := AuthorizeAuthPolicy(account, msg)
	require.ErrorContains(t, err, "does not verify")
}

func TestMultisigCoSignatureIsPayloadBound(t *testing.T) {
	account, key1, key2, key3 := coSigTestAccount(t)
	policyA := account.AuthPolicy
	policyB := account.AuthPolicy
	policyB.Threshold = 1 // attacker-preferred downgrade

	// key2 co-signs an auth-policy update to policy A...
	digestA := ExternalMessageSigningBytes(account.AddressUser, account.Sequence, AuthOperationAuthPolicyUpdate, 0, CoSignaturePayloadHash(policyA.Normalize()))
	coSig := key2.coSign(digestA)

	// ...but the message actually carries policy B: must fail.
	_, err := ApplyMsgUpdateAuthPolicy(account, MsgUpdateAuthPolicy{
		AccountUser:	account.AddressUser,
		NewAuthPolicy:	policyB,
		Signers:	[]string{key1.id},
		CoSignatures:	[]AuthCoSignature{coSig},
		CurrentHeight:	100,
	})
	require.ErrorContains(t, err, "does not verify")

	// With the payload the co-signers actually signed, the update passes.
	digestB := ExternalMessageSigningBytes(account.AddressUser, account.Sequence, AuthOperationAuthPolicyUpdate, 0, CoSignaturePayloadHash(policyB.Normalize()))
	next, err := ApplyMsgUpdateAuthPolicy(account, MsgUpdateAuthPolicy{
		AccountUser:	account.AddressUser,
		NewAuthPolicy:	policyB,
		Signers:	[]string{key1.id},
		CoSignatures:	[]AuthCoSignature{key2.coSign(digestB), key3.coSign(digestB)},
		CurrentHeight:	100,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), next.AuthPolicy.Threshold)
}

func TestMultisigRejectsUnknownAndDuplicateCoSigners(t *testing.T) {
	account, key1, key2, _ := coSigTestAccount(t)
	msg := coSigTestMessage(account)
	digest := ExternalMessageSigningBytes(msg.AccountUser, msg.Sequence, msg.Operation, msg.Amount, msg.PayloadHash)
	msg.Signers = []string{key1.id}

	unknown := newCoSigTestKey(t, "key-x-unknown", AuthKeyRoleDevice, 0x44)
	msg.CoSignatures = []AuthCoSignature{unknown.coSign(digest)}
	_, err := AuthorizeAuthPolicy(account, msg)
	require.ErrorContains(t, err, "unknown key")

	msg.CoSignatures = []AuthCoSignature{key2.coSign(digest), key2.coSign(digest)}
	_, err = AuthorizeAuthPolicy(account, msg)
	require.ErrorContains(t, err, "duplicated")
}

func TestWeightedModeCountsCoSignedWeight(t *testing.T) {
	account, key1, key2, key3 := coSigTestAccount(t)
	account.AuthPolicy.Mode = AuthModeWeighted
	account.AuthPolicy.Threshold = 60
	account.AuthPolicy.Weights = []AuthWeight{
		{KeyID: key1.id, Weight: 30},
		{KeyID: key2.id, Weight: 30},
		{KeyID: key3.id, Weight: 40},
	}
	msg := coSigTestMessage(account)
	digest := ExternalMessageSigningBytes(msg.AccountUser, msg.Sequence, msg.Operation, msg.Amount, msg.PayloadHash)
	msg.Signers = []string{key1.id}

	// tx key alone: 30 < 60
	_, err := AuthorizeAuthPolicy(account, msg)
	require.ErrorContains(t, err, "below threshold")

	// tx key + co-signed key2: 60 >= 60
	msg.CoSignatures = []AuthCoSignature{key2.coSign(digest)}
	result, err := AuthorizeAuthPolicy(account, msg)
	require.NoError(t, err)
	require.True(t, result.Authorized)
	require.Equal(t, uint64(60), result.Weight)
}

func TestRecoveryRequiresRealCoSignatures(t *testing.T) {
	account, _, _, _ := coSigTestAccount(t)
	guardian1 := newCoSigTestKey(t, "recovery-1", AuthKeyRoleGuardian, 0x55)
	guardian2 := newCoSigTestKey(t, "recovery-2", AuthKeyRoleGuardian, 0x66)
	account.AuthPolicy.RecoveryPolicy = RecoveryPolicy{
		Keys:		[]string{guardian1.pub, guardian2.pub},
		Threshold:	2,
	}

	// naming the public recovery keys without proof must fail
	_, err := ApplyMsgRecoverAccount(account, MsgRecoverAccount{
		AccountUser:	account.AddressUser,
		Signers:	[]string{guardian1.pub, guardian2.pub},
		CurrentHeight:	1_000,
	})
	require.ErrorContains(t, err, "below threshold")

	// real guardian co-signatures over the recovery digest pass; the KeyID of
	// a recovery co-signature is the registered recovery public key itself
	digest := ExternalMessageSigningBytes(account.AddressUser, account.Sequence, AuthOperationRecoverAccount, 0, nil)
	next, err := ApplyMsgRecoverAccount(account, MsgRecoverAccount{
		AccountUser:	account.AddressUser,
		CurrentHeight:	1_000,
		CoSignatures: []AuthCoSignature{
			{KeyID: guardian1.pub, Signature: hex.EncodeToString(ed25519.Sign(guardian1.priv, digest))},
			{KeyID: guardian2.pub, Signature: hex.EncodeToString(ed25519.Sign(guardian2.priv, digest))},
		},
	})
	require.NoError(t, err)
	require.Equal(t, AccountStatusRecovered, next.Status)
}

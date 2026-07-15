package types

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	authKeyPrimaryPub  = "ed25519:primary"
	authKeyDevicePub   = "ed25519:device"
	authKeyRecoveryPub = "ed25519:recovery"
	authKeyBackupPub   = "ed25519:backup"
)

func TestSingleKeyPolicyAuthorizesNormalTx(t *testing.T) {
	account := completeActiveAccount(t, 0xc1, 600, 1)
	account.AuthPolicy = AuthPolicy{Version: 1, Mode: AuthModeSingleKey}

	next, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{account.PubKeys[0]},
		Operation:   AuthOperationTransfer,
		Amount:      10,
	})

	require.NoError(t, err)
	require.Equal(t, account.Sequence+1, next.Sequence)
}

// TestStepUpNotSatisfiedByCallerControllingOnlyPrimaryKey is the regression
// guard for SEC-HIGH #7: multi-key auth was decided by string-matching the
// unverified msg.Signers body field against public on-chain key IDs/pubkeys, so
// a caller holding only the primary key could name the guardian's public key
// and pass a step-up. The fix credits only the one cryptographically-verified
// account_user key, so the bypass fails closed.
func TestStepUpNotSatisfiedByCallerControllingOnlyPrimaryKey(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version: 1,
		Mode:    AuthModeSingleKey,
		Keys: []AuthKey{
			{ID: "primary", PublicKey: authKeyPrimaryPub, Role: AuthKeyRolePrimary},
			{ID: "guardian", PublicKey: authKeyBackupPub, Role: AuthKeyRoleGuardian},
		},
		StepUp: &StepUpPolicy{Mode: "guardian"},
	})

	// Attacker controls only the primary key but names the guardian pubkey
	// (readable public state) to try to satisfy the guardian step-up.
	_, err := AuthorizeAuthPolicy(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{authKeyPrimaryPub, authKeyBackupPub},
		Operation:   AuthOperationAuthPolicyUpdate,
	})
	require.Error(t, err, "guardian step-up must not be satisfiable by naming a public key the caller does not control")
}

// TestAuthPolicyRejectsDuplicatePublicKey covers SA2 #22: two keys sharing a
// public key would let a single private key satisfy a multi-signature
// threshold, so the policy must be rejected at validation.
func TestAuthPolicyRejectsDuplicatePublicKey(t *testing.T) {
	policy := AuthPolicy{
		Version:   1,
		Mode:      AuthModeThreshold,
		Threshold: 2,
		Keys: []AuthKey{
			{ID: "a", PublicKey: authKeyPrimaryPub, Role: AuthKeyRolePrimary},
			{ID: "b", PublicKey: authKeyPrimaryPub, Role: AuthKeyRoleGuardian},
		},
	}
	require.ErrorContains(t, policy.Validate(), "distinct public keys")
}

func TestMultisigThresholdPolicyRejectsInsufficientSignatures(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version:   1,
		Mode:      AuthModeThreshold,
		Keys:      authKeys(),
		Threshold: 2,
	})

	_, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary"},
		Operation:   AuthOperationTransfer,
	})
	require.ErrorContains(t, err, "below threshold")

	// SEC-HIGH #7: only the one cryptographically-verified account_user key
	// ("primary") counts toward the threshold; naming "device" (public on-chain
	// state) in the unverified Signers body no longer helps, so a threshold-2
	// policy now fails closed. Real N-of-M needs multi-party signature
	// verification (a proto/tx/ante redesign).
	_, err = ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary", "device"},
		Operation:   AuthOperationTransfer,
	})
	require.ErrorContains(t, err, "below threshold")
}

func TestWeightedMultisigSumsWeightsDeterministically(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version:   1,
		Mode:      AuthModeWeighted,
		Keys:      authKeys(),
		Threshold: 7,
		Weights: []AuthWeight{
			{KeyID: "recovery", Weight: 1},
			{KeyID: "primary", Weight: 5},
			{KeyID: "device", Weight: 3},
		},
	})

	_, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary"},
		Operation:   AuthOperationTransfer,
	})
	require.ErrorContains(t, err, "below threshold")

	// SEC-HIGH #7: only the verified "primary" key (weight 5) is credited; the
	// named "device" weight is dropped, leaving 5 < threshold 7 -> fails closed.
	_, err = AuthorizeAuthPolicy(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"device", "primary"},
		Operation:   AuthOperationTransfer,
	})
	require.ErrorContains(t, err, "below threshold")

	normalized := account.AuthPolicy.Normalize()
	require.Equal(t, []AuthWeight{
		{KeyID: "device", Weight: 3},
		{KeyID: "primary", Weight: 5},
		{KeyID: "recovery", Weight: 1},
	}, normalized.Weights)
}

func TestTwoDevicePolicyRequiresBothKeysForProtectedOperations(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version:   1,
		Mode:      AuthModeTwoDevice,
		Keys:      authKeys(),
		Threshold: 2,
	})

	_, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary"},
		Operation:   AuthOperationStakingChange,
	})
	require.ErrorContains(t, err, "primary and device")

	// SEC-HIGH #7: naming "device" (public state) no longer satisfies two-device;
	// only the verified "primary" key counts, so it fails closed.
	_, err = ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary", "device"},
		Operation:   AuthOperationStakingChange,
	})
	require.ErrorContains(t, err, "primary and device")
}

func TestSpendingLimitAllowsSmallTransferAndRejectsLargeTransfer(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version: 1,
		Mode:    AuthModeTwoDevice,
		Keys:    authKeys(),
		SpendingLimits: []SpendingLimit{
			{Operation: AuthOperationTransfer, MaxAmount: 100},
		},
	})

	_, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary"},
		Operation:   AuthOperationTransfer,
		Amount:      100,
	})
	require.NoError(t, err)

	_, err = ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{"primary"},
		Operation:   AuthOperationTransfer,
		Amount:      101,
	})
	require.ErrorContains(t, err, "primary and device")
}

func TestHighRiskOperationRequiresSecondFactorWhenStepUpPolicyIsConfigured(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version: 1,
		Mode:    AuthModeSingleKey,
		Keys: []AuthKey{
			{ID: "primary", PublicKey: authKeyPrimaryPub, Role: AuthKeyRolePrimary},
			{ID: "guardian", PublicKey: authKeyBackupPub, Role: AuthKeyRoleGuardian},
		},
		StepUp: &StepUpPolicy{
			Mode: "guardian",
		},
		SpendingLimits: []SpendingLimit{
			{Operation: AuthOperationTransfer, MaxAmount: 100},
		},
	})

	_, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{authKeyPrimaryPub},
		Operation:   AuthOperationAuthPolicyUpdate,
	})
	require.ErrorContains(t, err, "second factor")

	// SEC-HIGH #7: naming the guardian pubkey (public state) no longer supplies a
	// second factor; only the verified primary key counts, so step-up fails closed.
	_, err = ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{authKeyPrimaryPub, authKeyBackupPub},
		Operation:   AuthOperationAuthPolicyUpdate,
	})
	require.ErrorContains(t, err, "second factor")
}

func TestLowRiskOperationStillUsesSingleSeedSignatureWithSpendingLimit(t *testing.T) {
	account := accountWithPolicy(t, AuthPolicy{
		Version: 1,
		Mode:    AuthModeSingleKey,
		Keys: []AuthKey{
			{ID: "primary", PublicKey: authKeyPrimaryPub, Role: AuthKeyRolePrimary},
			{ID: "guardian", PublicKey: authKeyBackupPub, Role: AuthKeyRoleGuardian},
		},
		StepUp: &StepUpPolicy{
			Mode: "guardian",
		},
		SpendingLimits: []SpendingLimit{
			{Operation: AuthOperationTransfer, MaxAmount: 100},
		},
	})

	next, err := ApplyExternalMessage(account, ExternalMessage{
		AccountUser: account.AddressUser,
		Sequence:    account.Sequence,
		Signers:     []string{authKeyPrimaryPub},
		Operation:   AuthOperationTransfer,
		Amount:      50,
	})
	require.NoError(t, err)
	require.Equal(t, account.Sequence+1, next.Sequence)
}

func TestTimelockPreventsEarlyRecoveryAndAuthChange(t *testing.T) {
	account := accountWithPolicy(t, recoveryPolicy(100))

	_, err := ApplyMsgRecoverAccount(account, MsgRecoverAccount{
		AccountUser:   account.AddressUser,
		Signers:       []string{authKeyRecoveryPub},
		CurrentHeight: 99,
	})
	require.ErrorContains(t, err, "timelock")

	_, err = ApplyMsgUpdateAuthPolicy(account, MsgUpdateAuthPolicy{
		AccountUser:   account.AddressUser,
		NewAuthPolicy: AuthPolicy{Version: 1, Mode: AuthModeSingleKey},
		Signers:       []string{"primary", "device"},
		CurrentHeight: 99,
	})
	require.ErrorContains(t, err, "timelock")
}

func TestRecoveryPolicyChangesStatusAfterValidAuthorization(t *testing.T) {
	// Recovery signers must PROVE possession with a co-signature over the
	// canonical recovery digest; naming a public recovery key is not enough.
	guardian := newCoSigTestKey(t, "guardian", AuthKeyRoleGuardian, 0x77)
	policy := recoveryPolicy(10)
	policy.RecoveryPolicy.Keys = []string{guardian.pub, authKeyBackupPub}
	account := accountWithPolicy(t, policy)
	account.Status = AccountStatusFrozen

	digest := ExternalMessageSigningBytes(account.AddressUser, account.Sequence, AuthOperationRecoverAccount, 0, nil)
	coSig := guardian.coSign(digest)
	coSig.KeyID = guardian.pub // recovery co-signatures are keyed by the registered public key

	recovered, err := ApplyMsgRecoverAccount(account, MsgRecoverAccount{
		AccountUser:   account.AddressUser,
		CoSignatures:  []AuthCoSignature{coSig},
		CurrentHeight: 10,
	})

	require.NoError(t, err)
	require.Equal(t, AccountStatusRecovered, recovered.Status)
	require.Equal(t, account.AddressUser, recovered.AddressUser)
	require.Equal(t, account.AddressRaw, recovered.AddressRaw)
}

func TestKeyRotationPreservesAEAndRawAddresses(t *testing.T) {
	// SEC-HIGH #7: single-key mode is authorized by the one verified primary key.
	// (Multi-key modes fail closed until multi-party signing exists.)
	account := accountWithPolicy(t, AuthPolicy{
		Version: 1,
		Mode:    AuthModeSingleKey,
		Keys:    authKeys(),
	})

	rotated, err := ApplyMsgRotateKey(account, MsgRotateKey{
		AccountUser: account.AddressUser,
		OldKeyID:    "device",
		NewKey:      AuthKey{ID: "device", PublicKey: "ed25519:new-device", Role: AuthKeyRoleDevice},
		Signers:     []string{"primary"},
	})

	require.NoError(t, err)
	require.Equal(t, account.AddressUser, rotated.AddressUser)
	require.Equal(t, account.AddressRaw, rotated.AddressRaw)
	require.Equal(t, "ed25519:new-device", authKeyByID(rotated.AuthPolicy.Keys, "device").PublicKey)
}

func TestAuthPolicyUpdateRequiresAuthorization(t *testing.T) {
	// SEC-HIGH #7: single-key mode; only the verified "primary" key authorizes a
	// policy update. Naming an unverified key ("device") is dropped and cannot
	// authorize. (Multi-key modes fail closed until multi-party signing exists.)
	account := accountWithPolicy(t, AuthPolicy{
		Version: 1,
		Mode:    AuthModeSingleKey,
		Keys:    authKeys(),
	})
	nextPolicy := AuthPolicy{
		Version: 1,
		Mode:    AuthModeSingleKey,
		Keys:    authKeys(),
	}

	_, err := ApplyMsgUpdateAuthPolicy(account, MsgUpdateAuthPolicy{
		AccountUser:   account.AddressUser,
		NewAuthPolicy: nextPolicy,
		Signers:       []string{"device"},
	})
	require.Error(t, err)

	updated, err := ApplyMsgUpdateAuthPolicy(account, MsgUpdateAuthPolicy{
		AccountUser:   account.AddressUser,
		NewAuthPolicy: nextPolicy,
		Signers:       []string{"primary"},
	})
	require.NoError(t, err)
	require.Equal(t, AuthModeSingleKey, updated.AuthPolicy.Mode)
	require.Equal(t, account.AddressUser, updated.AddressUser)
	require.Equal(t, account.AddressRaw, updated.AddressRaw)
}

func TestAuthPolicySerializationRejectsPrivateSeedSMSTOTPSecrets(t *testing.T) {
	fixtures := []AuthPolicy{
		{Version: 1, Mode: AuthModeSingleKey, Keys: []AuthKey{{ID: "primary", PublicKey: "private_key:bad", Role: AuthKeyRolePrimary}}},
		{Version: 1, Mode: AuthModeSingleKey, Keys: []AuthKey{{ID: "primary", PublicKey: "seed phrase bad", Role: AuthKeyRolePrimary}}},
		{Version: 1, Mode: AuthModeSingleKey, Keys: []AuthKey{{ID: "sms_secret", PublicKey: "ed25519:ok", Role: AuthKeyRolePrimary}}},
		{Version: 1, Mode: AuthModeSingleKey, Keys: []AuthKey{{ID: "totp_secret", PublicKey: "ed25519:ok", Role: AuthKeyRolePrimary}}},
	}

	for _, policy := range fixtures {
		require.Error(t, policy.Validate())
	}

	account := accountWithPolicy(t, AuthPolicy{Version: 1, Mode: AuthModeSingleKey})
	bz, err := json.Marshal(account)
	require.NoError(t, err)
	lower := strings.ToLower(string(bz))
	require.NotContains(t, lower, "private_key")
	require.NotContains(t, lower, "seed phrase")
	require.NotContains(t, lower, "sms_secret")
	require.NotContains(t, lower, "totp_secret")
}

func accountWithPolicy(t *testing.T, policy AuthPolicy) Account {
	t.Helper()
	account := completeActiveAccount(t, 0xc2, 601, 2)
	account.AuthPolicy = policy.Normalize()
	require.NoError(t, ValidateAccountInvariant(account))
	return account
}

func authKeys() []AuthKey {
	return []AuthKey{
		{ID: "primary", PublicKey: authKeyPrimaryPub, Role: AuthKeyRolePrimary},
		{ID: "device", PublicKey: authKeyDevicePub, Role: AuthKeyRoleDevice},
		{ID: "recovery", PublicKey: authKeyRecoveryPub, Role: AuthKeyRoleRecovery},
	}
}

func authKeyByID(keys []AuthKey, id string) AuthKey {
	for _, key := range keys {
		if key.ID == id {
			return key
		}
	}
	return AuthKey{}
}

func recoveryPolicy(height uint64) AuthPolicy {
	return AuthPolicy{
		Version: 1,
		Mode:    AuthModeTwoDevice,
		Keys:    authKeys(),
		RecoveryPolicy: RecoveryPolicy{
			Keys:              []string{authKeyRecoveryPub, authKeyBackupPub},
			Threshold:         1,
			TimelockEndHeight: height,
		},
		Timelock: TimelockPolicy{
			AuthPolicyUpdateEndHeight: height,
			RecoveryEndHeight:         height,
		},
	}
}

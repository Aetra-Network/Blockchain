package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func AuthorizeAuthPolicy(account Account, msg ExternalMessage) (AuthzResult, error) {
	policy := account.AuthPolicy.Normalize()
	if err := policy.Validate(); err != nil {
		return AuthzResult{}, err
	}
	risk := ClassifyAuthOperationRisk(msg.Operation)
	keys := effectiveAuthKeys(account)
	// SECURITY (SEC-HIGH #7): msg.Signers is an unverified request-body field.
	// A native-account transaction carries exactly one cryptographically
	// verified principal — the account_user, which is this account itself
	// (account.AddressUser is derived from account.PubKeys). Additional keys
	// count only when they PROVE possession via a co-signature over the
	// canonical message digest (auth_cosignature.go). The claimed signer list
	// is reduced to those two verified sources, so no threshold, weight, role,
	// or step-up requirement can be met by merely naming public key material
	// the caller does not actually control.
	digest := ExternalMessageSigningBytes(msg.AccountUser, msg.Sequence, msg.Operation, msg.Amount, msg.PayloadHash)
	coSigned, err := verifyCoSignatures(keys, digest, msg.CoSignatures)
	if err != nil {
		return AuthzResult{}, err
	}
	signers := authenticatedSigners(account, keys, msg.Signers, coSigned)
	switch policy.Mode {
	case AuthModeSingleKey:
		if risk == AuthOperationRiskLow && operationWithinSpendingLimit(policy, msg.Operation, msg.Amount) {
			if signedByAnyKey(keys, signers) {
				return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers}, nil
			}
			return AuthzResult{}, errors.New("external message missing authorized single-key signer")
		}
		if !policy.RequiresStepUp(msg.Operation) {
			if signedByAnyKey(keys, signers) {
				return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers}, nil
			}
			return AuthzResult{}, errors.New("external message missing authorized single-key signer")
		}
		if signedByAnyKey(keys, signers) && policy.hasRequiredStepUpSigners(keys, signers) {
			return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers}, nil
		}
		return AuthzResult{}, errors.New("external message missing required second factor")
	case AuthModeMultisig, AuthModeThreshold:
		threshold := policy.Threshold
		if policy.Mode == AuthModeMultisig && threshold == 0 {
			threshold = uint64(len(keys))
		}
		count := countSignedKeys(keys, signers)
		if count < threshold {
			return AuthzResult{}, fmt.Errorf("external message signatures %d below threshold %d", count, threshold)
		}
		if policy.RequiresStepUp(msg.Operation) && !policy.hasRequiredStepUpSigners(keys, signers) {
			return AuthzResult{}, errors.New("external message missing required second factor")
		}
		return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers}, nil
	case AuthModeWeighted:
		weight := signedWeight(policy.Weights, signers)
		if weight < policy.Threshold {
			return AuthzResult{}, fmt.Errorf("external message signer weight %d below threshold %d", weight, policy.Threshold)
		}
		if policy.RequiresStepUp(msg.Operation) && !policy.hasRequiredStepUpSigners(keys, signers) {
			return AuthzResult{}, errors.New("external message missing required second factor")
		}
		return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers, Weight: weight}, nil
	case AuthModeTwoDevice:
		if risk == AuthOperationRiskLow && operationWithinSpendingLimit(policy, msg.Operation, msg.Amount) && signedByRole(keys, signers, AuthKeyRolePrimary) {
			return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers}, nil
		}
		if signedByRole(keys, signers, AuthKeyRolePrimary) && (signedByRole(keys, signers, AuthKeyRoleDevice) || signedByRole(keys, signers, AuthKeyRoleChallenge) || signedByRole(keys, signers, AuthKeyRoleGuardian)) {
			return AuthzResult{Authorized: true, Mode: policy.Mode, Signers: signers}, nil
		}
		return AuthzResult{}, errors.New("two-device auth requires primary and device signatures")
	default:
		return AuthzResult{}, fmt.Errorf("unsupported external auth policy mode %q", policy.Mode)
	}
}

func AuthorizeRecoveryPolicy(account Account, msg MsgRecoverAccount) error {
	policy := account.AuthPolicy.Normalize()
	if err := policy.RecoveryPolicy.Validate(); err != nil {
		return err
	}
	if len(policy.RecoveryPolicy.Keys) == 0 {
		return errors.New("native account recovery policy is not configured")
	}
	if msg.CurrentHeight < policy.RecoveryPolicy.TimelockEndHeight || msg.CurrentHeight < policy.Timelock.RecoveryEndHeight {
		return errors.New("native account recovery timelock has not expired")
	}
	// SECURITY: recovery keys are public on-chain state, so merely naming them
	// must never count. Each recovery signer proves possession with a
	// co-signature over the canonical recovery digest, exactly like multi-key
	// external messages. See SEC-HIGH #7 / auth_cosignature.go.
	// Recovery keys are identified by their public key string itself, so the
	// co-signature KeyID is stable regardless of policy normalization order.
	recoveryKeys := make([]AuthKey, 0, len(policy.RecoveryPolicy.Keys))
	for _, pubKey := range policy.RecoveryPolicy.Keys {
		recoveryKeys = append(recoveryKeys, AuthKey{ID: pubKey, PublicKey: pubKey, Role: AuthKeyRoleGuardian})
	}
	digest := ExternalMessageSigningBytes(msg.AccountUser, account.Sequence, AuthOperationRecoverAccount, 0, nil)
	coSigned, err := verifyCoSignatures(recoveryKeys, digest, msg.CoSignatures)
	if err != nil {
		return err
	}
	count := uint64(0)
	for _, pubKey := range policy.RecoveryPolicy.Keys {
		if _, ok := coSigned[pubKey]; ok {
			count++
		}
	}
	if count < policy.RecoveryPolicy.Threshold {
		return fmt.Errorf("recovery signatures %d below threshold %d", count, policy.RecoveryPolicy.Threshold)
	}
	return nil
}

func effectiveAuthKeys(account Account) []AuthKey {
	policy := account.AuthPolicy.Normalize()
	if len(policy.Keys) > 0 {
		return policy.Keys
	}
	keys := make([]AuthKey, 0, len(account.PubKeys))
	for idx, pubKey := range account.PubKeys {
		keys = append(keys, AuthKey{ID: fmt.Sprintf("legacy-%020d", idx), PublicKey: pubKey, Role: AuthKeyRolePrimary})
	}
	return keys
}

func validateAuthKeys(keys []AuthKey, allowEmpty bool) error {
	if len(keys) == 0 {
		if allowEmpty {
			return nil
		}
		return errors.New("native account auth policy keys are required")
	}
	previous := ""
	for _, key := range keys {
		key = key.Normalize()
		if key.ID == "" || key.PublicKey == "" {
			return errors.New("native account auth key id and public key are required")
		}
		if containsSecretLikeText(key.ID) || containsSecretLikeText(key.PublicKey) || containsSecretLikeText(key.Role) {
			return errors.New("native account auth policy must not contain private keys or seed phrases")
		}
		if key.ID <= previous {
			return errors.New("native account auth keys must be sorted and unique")
		}
		previous = key.ID
	}
	return nil
}

func validateAuthWeights(keys []AuthKey, weights []AuthWeight, threshold uint64) error {
	if len(weights) == 0 {
		return errors.New("native account weighted auth weights are required")
	}
	keyIDs := map[string]struct{}{}
	for _, key := range keys {
		keyIDs[key.ID] = struct{}{}
	}
	total := uint64(0)
	previous := ""
	for _, weight := range weights {
		weight.KeyID = strings.TrimSpace(weight.KeyID)
		if weight.KeyID == "" || weight.Weight == 0 {
			return errors.New("native account weighted auth key id and weight are required")
		}
		if containsSecretLikeText(weight.KeyID) {
			return errors.New("native account auth policy must not contain private keys or seed phrases")
		}
		if _, found := keyIDs[weight.KeyID]; !found {
			return fmt.Errorf("native account weighted auth references unknown key %q", weight.KeyID)
		}
		if weight.KeyID <= previous {
			return errors.New("native account weighted auth weights must be sorted and unique")
		}
		previous = weight.KeyID
		total += weight.Weight
	}
	if total < threshold {
		return errors.New("native account weighted auth total weight below threshold")
	}
	return nil
}

func validateSpendingLimits(limits []SpendingLimit) error {
	previous := ""
	for _, limit := range limits {
		operation := strings.TrimSpace(limit.Operation)
		if operation == "" {
			return errors.New("native account spending limit operation is required")
		}
		if containsSecretLikeText(operation) {
			return errors.New("native account spending limits must not contain private keys or seed phrases")
		}
		key := fmt.Sprintf("%s/%020d", operation, limit.MaxAmount)
		if key <= previous {
			return errors.New("native account spending limits must be sorted and unique")
		}
		previous = key
	}
	return nil
}

const (
	AuthOperationRiskLow  = "low"
	AuthOperationRiskHigh = "high"
)

func ClassifyAuthOperationRisk(operation string) string {
	switch strings.TrimSpace(operation) {
	case AuthOperationTransfer:
		return AuthOperationRiskLow
	case AuthOperationStakingChange,
		AuthOperationAuthPolicyUpdate,
		AuthOperationRecoverAccount,
		AuthOperationFreezeAccount,
		AuthOperationPayStorageDebt,
		AuthOperationUnfreezeAccount,
		AuthOperationMetadataUpdate,
		AuthOperationParamsUpdate:
		return AuthOperationRiskHigh
	default:
		return AuthOperationRiskHigh
	}
}

func (p AuthPolicy) RequiresStepUp(operation string) bool {
	if p.StepUp == nil {
		return false
	}
	stepUp := p.StepUp.Normalize()
	if stepUp.Mode == "" {
		return false
	}
	if len(stepUp.ProtectedOperations) == 0 {
		return ClassifyAuthOperationRisk(operation) == AuthOperationRiskHigh
	}
	for _, protected := range stepUp.ProtectedOperations {
		if protected == strings.TrimSpace(operation) {
			return true
		}
	}
	return false
}

func (p AuthPolicy) hasRequiredStepUpSigners(keys []AuthKey, signers []string) bool {
	if p.StepUp == nil {
		return false
	}
	stepUp := p.StepUp.Normalize()
	roles := append([]string(nil), stepUp.RequiredRoles...)
	if len(roles) == 0 {
		switch stepUp.Mode {
		case "2fa":
			roles = []string{AuthKeyRoleDevice}
		case "challenge":
			roles = []string{AuthKeyRoleChallenge}
		case "guardian":
			roles = []string{AuthKeyRoleGuardian}
		default:
			return false
		}
	}
	for _, role := range roles {
		if !signedByRole(keys, signers, role) {
			return false
		}
	}
	return true
}

func hasAuthKeyRole(keys []AuthKey, role string) bool {
	for _, key := range keys {
		if key.Role == role {
			return true
		}
	}
	return false
}

func operationWithinSpendingLimit(policy AuthPolicy, operation string, amount uint64) bool {
	operation = strings.TrimSpace(operation)
	for _, limit := range policy.SpendingLimits {
		if limit.Operation == operation && amount <= limit.MaxAmount {
			return true
		}
	}
	return false
}

func canonicalSigners(signers []string) []string {
	out := make([]string, 0, len(signers))
	seen := map[string]struct{}{}
	for _, signer := range signers {
		signer = strings.TrimSpace(signer)
		if signer == "" {
			continue
		}
		if _, found := seen[signer]; found {
			continue
		}
		seen[signer] = struct{}{}
		out = append(out, signer)
	}
	sort.Strings(out)
	return out
}

// authenticatedSigners returns the signer tokens that are cryptographically
// backed: the transaction's single verified signer (account_user) plus every
// key that proved possession via a valid co-signature. Client-claimed strings
// that lack either proof are dropped: any AuthKey ID, public key, or guardian
// pubkey is public on-chain state and must never count toward a policy
// requirement just because the caller named it. See SEC-HIGH #7.
func authenticatedSigners(account Account, keys []AuthKey, claimed []string, coSigned map[string]struct{}) []string {
	verified := verifiedSignerKeyTokens(account, keys)
	for token := range coSigned {
		verified[token] = struct{}{}
	}
	out := make([]string, 0, len(claimed)+len(coSigned))
	seen := map[string]struct{}{}
	for _, signer := range claimed {
		signer = strings.TrimSpace(signer)
		if signer == "" {
			continue
		}
		if _, ok := verified[signer]; !ok {
			continue
		}
		if _, dup := seen[signer]; dup {
			continue
		}
		seen[signer] = struct{}{}
		out = append(out, signer)
	}
	// A co-signature is itself the claim: proven keys count even when the
	// client did not repeat them in the Signers list.
	for token := range coSigned {
		if _, dup := seen[token]; dup {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}

// verifiedSignerKeyTokens returns the identifier tokens (ID and public key) of
// the single AuthKey that the verified account_user controls. Preference is
// given to the key cryptographically bound to the account's on-chain public
// key material (account.PubKeys, from which account_user is derived); failing
// that, the account's designated primary key is used. Exactly one key is ever
// returned, so a single verified signature can never satisfy a multi-key
// policy. See SEC-HIGH #7.
func verifiedSignerKeyTokens(account Account, keys []AuthKey) map[string]struct{} {
	pubKeys := map[string]struct{}{}
	for _, pk := range account.PubKeys {
		if pk = strings.TrimSpace(pk); pk != "" {
			pubKeys[pk] = struct{}{}
		}
	}
	var owner *AuthKey
	for i := range keys {
		if _, ok := pubKeys[keys[i].PublicKey]; ok {
			owner = &keys[i]
			break
		}
	}
	if owner == nil {
		for i := range keys {
			if keys[i].Role == AuthKeyRolePrimary {
				owner = &keys[i]
				break
			}
		}
	}
	tokens := map[string]struct{}{}
	if owner != nil {
		if owner.ID != "" {
			tokens[owner.ID] = struct{}{}
		}
		if owner.PublicKey != "" {
			tokens[owner.PublicKey] = struct{}{}
		}
	}
	return tokens
}

func signedByAnyKey(keys []AuthKey, signers []string) bool {
	for _, key := range keys {
		for _, signer := range signers {
			if signer == key.PublicKey || signer == key.ID {
				return true
			}
		}
	}
	return false
}

func signedByRole(keys []AuthKey, signers []string, role string) bool {
	for _, key := range keys {
		if key.Role != role {
			continue
		}
		for _, signer := range signers {
			if signer == key.PublicKey || signer == key.ID {
				return true
			}
		}
	}
	return false
}

func countSignedKeys(keys []AuthKey, signers []string) uint64 {
	count := uint64(0)
	for _, key := range keys {
		for _, signer := range signers {
			if signer == key.PublicKey || signer == key.ID {
				count++
				break
			}
		}
	}
	return count
}

func countSignedStrings(keys []string, signers []string) uint64 {
	count := uint64(0)
	for _, key := range keys {
		for _, signer := range signers {
			if signer == key {
				count++
				break
			}
		}
	}
	return count
}

func signedWeight(weights []AuthWeight, signers []string) uint64 {
	total := uint64(0)
	signed := map[string]struct{}{}
	for _, signer := range signers {
		signed[signer] = struct{}{}
	}
	for _, weight := range weights {
		if _, found := signed[weight.KeyID]; found {
			total += weight.Weight
		}
	}
	return total
}

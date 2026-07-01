package txpreview

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	feegranttypes "github.com/cosmos/cosmos-sdk/x/feegrant"

	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

const (
	contractsEventCodeStored    = "contracts.code_stored"
	contractsEventInstantiated  = "contracts.instantiated"
	contractsEventExecuted      = "contracts.executed"
	contractsEventInternalSent  = "contracts.internal_message_sent"
	contractsEventParamsUpdated = "contracts.params_updated"
)

const (
	ModeNativeTxPreview = "native_tx_preview"
	MutationNone        = "none"
)

type Options struct {
	Account                     string                               `json:"account,omitempty"`
	DAppAddresses               []string                             `json:"dapp_addresses,omitempty"`
	KnownAccountPermissions     []string                             `json:"known_account_permissions,omitempty"`
	KnownSpendingAllowances     []string                             `json:"known_spending_allowances,omitempty"`
	KnownIBCApprovals           []string                             `json:"known_ibc_approvals,omitempty"`
	KnownContractAuthorizations []string                             `json:"known_contract_authorizations,omitempty"`
	IncludeQueryHints           bool                                 `json:"include_query_hints"`
	OriginDApp                  string                               `json:"origin_dapp,omitempty"`
	IntendedMessageDomain       string                               `json:"intended_message_domain,omitempty"`
	ExpectedSender              string                               `json:"expected_sender,omitempty"`
	Purpose                     string                               `json:"purpose,omitempty"`
	DangerousPermissions        []string                             `json:"dangerous_permissions,omitempty"`
	BlockOnMismatch             bool                                 `json:"block_on_mismatch"`
	SecurityBadge               *contracttypes.ContractSecurityBadge `json:"security_badge,omitempty"`
}

type Report struct {
	Mode             string            `json:"mode"`
	DryRun           bool              `json:"dry_run"`
	Mutation         string            `json:"mutation"`
	SigningRequired  bool              `json:"signing_required"`
	SigningBlocked   bool              `json:"signing_blocked,omitempty"`
	Memo             string            `json:"memo,omitempty"`
	Signers          []string          `json:"signers"`
	Fee              FeePreview        `json:"fee"`
	Messages         []MessagePreview  `json:"messages"`
	DAppAccess       DAppAccessPreview `json:"dapp_access"`
	Signing          SigningPreview    `json:"signing"`
	Warnings         []string          `json:"warnings,omitempty"`
	SimulationAdvice []string          `json:"simulation_advice,omitempty"`
}

type FeePreview struct {
	Amount       string   `json:"amount"`
	GasLimit     uint64   `json:"gas_limit"`
	FeePayer     string   `json:"fee_payer,omitempty"`
	FeeGranter   string   `json:"fee_granter,omitempty"`
	FeeStateDiff []string `json:"fee_state_diff,omitempty"`
}

type MessagePreview struct {
	Index              int          `json:"index"`
	TypeURL            string       `json:"type_url"`
	Summary            string       `json:"summary"`
	PayloadFormat      string       `json:"payload_format,omitempty"`
	Parties            []Party      `json:"parties,omitempty"`
	Coins              []CoinChange `json:"coins,omitempty"`
	StateDiff          []string     `json:"state_diff"`
	ExpectedEvents     []string     `json:"expected_events,omitempty"`
	ExecutionMessages  []string     `json:"execution_messages,omitempty"`
	PotentialApprovals []string     `json:"potential_approvals,omitempty"`
	Signers            []string     `json:"signers,omitempty"`
	RiskNotes          []string     `json:"risk_notes,omitempty"`
	RiskLabels         []string     `json:"risk_labels,omitempty"`
}

type SigningPreview struct {
	OriginDApp            string                               `json:"origin_dapp,omitempty"`
	IntendedMessageDomain string                               `json:"intended_message_domain,omitempty"`
	ExpectedSender        string                               `json:"expected_sender,omitempty"`
	Purpose               string                               `json:"purpose,omitempty"`
	DangerousPermissions  []string                             `json:"dangerous_permissions,omitempty"`
	Blocked               bool                                 `json:"blocked,omitempty"`
	Warnings              []string                             `json:"warnings,omitempty"`
	RiskLabels            []string                             `json:"risk_labels,omitempty"`
	SecurityBadge         *contracttypes.ContractSecurityBadge `json:"security_badge,omitempty"`
	MatchedSigners        []string                             `json:"matched_signers,omitempty"`
}

type Party struct {
	Role    string `json:"role"`
	Address string `json:"address"`
}

type CoinChange struct {
	Role    string `json:"role"`
	Address string `json:"address,omitempty"`
	Amount  string `json:"amount"`
}

type DAppAccessPreview struct {
	Account                     string   `json:"account,omitempty"`
	DAppAddresses               []string `json:"dapp_addresses,omitempty"`
	ExistingAccessStatus        string   `json:"existing_access_status"`
	KnownAccountPermissions     []string `json:"known_account_permissions,omitempty"`
	KnownSpendingAllowances     []string `json:"known_spending_allowances,omitempty"`
	KnownIBCApprovals           []string `json:"known_ibc_approvals,omitempty"`
	KnownContractAuthorizations []string `json:"known_contract_authorizations,omitempty"`
	AccessChanges               []string `json:"access_changes,omitempty"`
	QueryHints                  []string `json:"query_hints,omitempty"`
}

func Build(tx sdk.Tx, opts Options) (Report, error) {
	if tx == nil {
		return Report{}, fmt.Errorf("tx is required")
	}
	opts.DAppAddresses = normalizeStrings(opts.DAppAddresses)
	opts.DangerousPermissions = normalizeStrings(opts.DangerousPermissions)
	report := Report{
		Mode:            ModeNativeTxPreview,
		DryRun:          true,
		Mutation:        MutationNone,
		SigningRequired: true,
		Signers:         []string{},
		Messages:        []MessagePreview{},
		DAppAccess: DAppAccessPreview{
			Account:                     strings.TrimSpace(opts.Account),
			DAppAddresses:               opts.DAppAddresses,
			ExistingAccessStatus:        existingAccessStatus(opts),
			KnownAccountPermissions:     normalizeStrings(opts.KnownAccountPermissions),
			KnownSpendingAllowances:     normalizeStrings(opts.KnownSpendingAllowances),
			KnownIBCApprovals:           normalizeStrings(opts.KnownIBCApprovals),
			KnownContractAuthorizations: normalizeStrings(opts.KnownContractAuthorizations),
		},
		Signing: SigningPreview{
			OriginDApp:            strings.TrimSpace(opts.OriginDApp),
			IntendedMessageDomain: strings.TrimSpace(opts.IntendedMessageDomain),
			ExpectedSender:        strings.TrimSpace(opts.ExpectedSender),
			Purpose:               strings.TrimSpace(opts.Purpose),
			DangerousPermissions:  append([]string(nil), opts.DangerousPermissions...),
			SecurityBadge:         cloneSecurityBadge(opts.SecurityBadge),
		},
		SimulationAdvice: []string{
			"Use tx preview before signing to inspect signer intent.",
			"Use tx simulate against a node for ante/gas and keeper-level execution result.",
		},
	}
	if memoTx, ok := tx.(sdk.TxWithMemo); ok {
		report.Memo = memoTx.GetMemo()
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		report.Fee = previewFee(feeTx)
	}
	if report.Fee.Amount == "" {
		report.Fee.Amount = "0"
	}

	seenSigners := map[string]struct{}{}
	for i, msg := range tx.GetMsgs() {
		preview := PreviewMsg(i, msg, opts)
		report.Messages = append(report.Messages, preview)
		for _, signer := range preview.Signers {
			addUnique(seenSigners, &report.Signers, signer)
		}
		collectDAppAccessChanges(&report.DAppAccess, preview, opts.DAppAddresses)
	}
	sort.Strings(report.Signers)
	if len(report.Messages) == 0 {
		report.Warnings = append(report.Warnings, "transaction has no messages")
	}
	if opts.IncludeQueryHints {
		report.DAppAccess.QueryHints = buildQueryHints(opts)
	}
	finalizeSigningPreview(&report, opts)
	return report, nil
}

func finalizeSigningPreview(report *Report, opts Options) {
	if report == nil {
		return
	}
	report.Signing.DangerousPermissions = normalizeStrings(append(report.Signing.DangerousPermissions, collectDangerousPermissions(report.Messages)...))
	report.Signing.RiskLabels = normalizeStrings(collectRiskLabels(report.Messages))
	report.Signing.Warnings = normalizeStrings(report.Signing.Warnings)
	report.Warnings = normalizeStrings(append(report.Warnings, report.Signing.Warnings...))

	var blockReasons []string
	if report.Signing.OriginDApp != "" && report.Signing.IntendedMessageDomain != "" {
		if normalizeOriginKey(report.Signing.OriginDApp) != normalizeOriginKey(report.Signing.IntendedMessageDomain) {
			report.Signing.Warnings = append(report.Signing.Warnings, fmt.Sprintf("origin mismatch: origin %s does not match intended domain %s", report.Signing.OriginDApp, report.Signing.IntendedMessageDomain))
			blockReasons = append(blockReasons, "origin/domain mismatch")
		}
	}
	if report.Signing.ExpectedSender != "" && !containsString(report.Signers, report.Signing.ExpectedSender) {
		report.Signing.Warnings = append(report.Signing.Warnings, fmt.Sprintf("sender mismatch: expected %s but preview signers are %s", report.Signing.ExpectedSender, strings.Join(report.Signers, ", ")))
		blockReasons = append(blockReasons, "sender mismatch")
	}
	if badge := report.Signing.SecurityBadge; badge != nil {
		report.Signing.RiskLabels = normalizeStrings(append(report.Signing.RiskLabels, securityBadgeRiskLabels(*badge)...))
		report.Signing.Warnings = append(report.Signing.Warnings, securityBadgeWarnings(*badge)...)
		if badge.Badge == contracttypes.SecurityBadgeCritical {
			blockReasons = append(blockReasons, "critical security badge")
		}
	}
	report.Signing.Warnings = normalizeStrings(report.Signing.Warnings)
	report.Warnings = normalizeStrings(append(report.Warnings, report.Signing.Warnings...))
	if len(blockReasons) > 0 && opts.BlockOnMismatch {
		report.Signing.Blocked = true
		report.SigningBlocked = true
		report.Signing.Warnings = normalizeStrings(append(report.Signing.Warnings, "signing blocked: "+strings.Join(blockReasons, "; ")))
		report.Warnings = normalizeStrings(append(report.Warnings, report.Signing.Warnings...))
		report.Signing.Blocked = true
		report.Signing.Warnings = normalizeStrings(report.Signing.Warnings)
		report.Signing.MatchedSigners = append([]string(nil), report.Signers...)
		report.Signing.MatchedSigners = normalizeStrings(report.Signing.MatchedSigners)
		report.Signing.DangerousPermissions = normalizeStrings(report.Signing.DangerousPermissions)
		report.Signing.RiskLabels = normalizeStrings(report.Signing.RiskLabels)
		report.Signing.SecurityBadge = cloneSecurityBadge(report.Signing.SecurityBadge)
		report.Signing.ExpectedSender = strings.TrimSpace(report.Signing.ExpectedSender)
		report.Signing.OriginDApp = strings.TrimSpace(report.Signing.OriginDApp)
		report.Signing.IntendedMessageDomain = strings.TrimSpace(report.Signing.IntendedMessageDomain)
		report.Signing.Purpose = strings.TrimSpace(report.Signing.Purpose)
		return
	}
	report.Signing.Blocked = len(blockReasons) > 0 && opts.BlockOnMismatch
	report.SigningBlocked = report.Signing.Blocked
	report.Signing.MatchedSigners = append([]string(nil), report.Signers...)
	report.Signing.MatchedSigners = normalizeStrings(report.Signing.MatchedSigners)
	report.Signing.SecurityBadge = cloneSecurityBadge(report.Signing.SecurityBadge)
	report.Signing.ExpectedSender = strings.TrimSpace(report.Signing.ExpectedSender)
	report.Signing.OriginDApp = strings.TrimSpace(report.Signing.OriginDApp)
	report.Signing.IntendedMessageDomain = strings.TrimSpace(report.Signing.IntendedMessageDomain)
	report.Signing.Purpose = strings.TrimSpace(report.Signing.Purpose)
}

func collectDangerousPermissions(messages []MessagePreview) []string {
	var out []string
	for _, msg := range messages {
		out = append(out, msg.PotentialApprovals...)
	}
	return normalizeStrings(out)
}

func collectRiskLabels(messages []MessagePreview) []string {
	var out []string
	for _, msg := range messages {
		out = append(out, msg.RiskLabels...)
	}
	return normalizeStrings(out)
}

func normalizeOriginKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if host == "" {
		return value
	}
	return host
}

func securityBadgeRiskLabels(badge contracttypes.ContractSecurityBadge) []string {
	labels := append([]string(nil), badge.Categories...)
	labels = append(labels, badge.Flags...)
	if badge.Badge != "" {
		labels = append(labels, "security-badge:"+badge.Badge)
	}
	if badge.RiskScoreBps > 0 {
		labels = append(labels, fmt.Sprintf("risk-score:%dbps", badge.RiskScoreBps))
	}
	return normalizeStrings(labels)
}

func securityBadgeWarnings(badge contracttypes.ContractSecurityBadge) []string {
	warnings := append([]string(nil), badge.Flags...)
	if badge.Verified {
		warnings = append(warnings, "verified security attestation badge present")
	}
	if badge.RiskScoreBps >= 8_000 {
		warnings = append(warnings, fmt.Sprintf("security attestation risk score is high: %dbps", badge.RiskScoreBps))
	}
	if len(badge.GraphEdges) > 0 {
		warnings = append(warnings, fmt.Sprintf("security graph includes %d edge(s)", len(badge.GraphEdges)))
	}
	if len(badge.RelatedAddresses) > 0 {
		warnings = append(warnings, fmt.Sprintf("security attestation links %d related address(es)", len(badge.RelatedAddresses)))
	}
	return normalizeStrings(warnings)
}

func cloneSecurityBadge(badge *contracttypes.ContractSecurityBadge) *contracttypes.ContractSecurityBadge {
	if badge == nil {
		return nil
	}
	out := *badge
	out.Categories = append([]string(nil), badge.Categories...)
	out.Flags = append([]string(nil), badge.Flags...)
	out.RelatedAddresses = append([]string(nil), badge.RelatedAddresses...)
	out.GraphEdges = append([]contracttypes.SecurityGraphEdge(nil), badge.GraphEdges...)
	out.AttestationIDs = append([]string(nil), badge.AttestationIDs...)
	return &out
}

func PreviewMsg(index int, msg sdk.Msg, opts Options) MessagePreview {
	preview := MessagePreview{
		Index:         index,
		TypeURL:       msgTypeURL(msg),
		Summary:       fmt.Sprintf("review %s", msgTypeURL(msg)),
		PayloadFormat: "structured",
		StateDiff:     []string{},
	}

	switch m := msg.(type) {
	case *banktypes.MsgSend:
		preview.Summary = fmt.Sprintf("send %s from %s to %s", m.Amount.String(), m.FromAddress, m.ToAddress)
		preview.Parties = parties("from", m.FromAddress, "to", m.ToAddress)
		preview.Coins = []CoinChange{
			{Role: "debit", Address: m.FromAddress, Amount: m.Amount.String()},
			{Role: "credit", Address: m.ToAddress, Amount: m.Amount.String()},
		}
		preview.StateDiff = []string{
			fmt.Sprintf("bank: debit %s from %s", m.Amount.String(), m.FromAddress),
			fmt.Sprintf("bank: credit %s to %s", m.Amount.String(), m.ToAddress),
		}
		preview.ExpectedEvents = []string{"message", "coin_spent", "coin_received", "transfer"}
		preview.Signers = normalizeStrings([]string{m.FromAddress})
	case *banktypes.MsgMultiSend:
		preview.Summary = fmt.Sprintf("multi-send with %d inputs and %d outputs", len(m.Inputs), len(m.Outputs))
		for _, input := range m.Inputs {
			preview.Parties = append(preview.Parties, Party{Role: "input", Address: input.Address})
			preview.Coins = append(preview.Coins, CoinChange{Role: "debit", Address: input.Address, Amount: input.Coins.String()})
			preview.StateDiff = append(preview.StateDiff, fmt.Sprintf("bank: debit %s from %s", input.Coins.String(), input.Address))
			preview.Signers = append(preview.Signers, input.Address)
		}
		for _, output := range m.Outputs {
			preview.Parties = append(preview.Parties, Party{Role: "output", Address: output.Address})
			preview.Coins = append(preview.Coins, CoinChange{Role: "credit", Address: output.Address, Amount: output.Coins.String()})
			preview.StateDiff = append(preview.StateDiff, fmt.Sprintf("bank: credit %s to %s", output.Coins.String(), output.Address))
		}
		preview.ExpectedEvents = []string{"message", "coin_spent", "coin_received", "transfer"}
		preview.Signers = normalizeStrings(preview.Signers)
	case *authztypes.MsgGrant:
		authType := anyTypeURL(m.Grant.Authorization)
		preview.Summary = fmt.Sprintf("grant authz %s from %s to %s", valueOrUnknown(authType), m.Granter, m.Grantee)
		preview.Parties = parties("granter", m.Granter, "grantee", m.Grantee)
		preview.StateDiff = []string{fmt.Sprintf("authz: create or replace grant granter=%s grantee=%s authorization=%s", m.Granter, m.Grantee, valueOrUnknown(authType))}
		preview.ExpectedEvents = []string{"message", "grant"}
		preview.PotentialApprovals = []string{fmt.Sprintf("account permission: %s may execute %s for %s", m.Grantee, valueOrUnknown(authType), m.Granter)}
		if m.Grant.Expiration != nil {
			preview.PotentialApprovals = append(preview.PotentialApprovals, "expiration: "+m.Grant.Expiration.UTC().Format("2006-01-02T15:04:05Z"))
		}
		preview.Signers = normalizeStrings([]string{m.Granter})
	case *authztypes.MsgRevoke:
		preview.Summary = fmt.Sprintf("revoke authz %s from %s to %s", valueOrUnknown(m.MsgTypeUrl), m.Granter, m.Grantee)
		preview.Parties = parties("granter", m.Granter, "grantee", m.Grantee)
		preview.StateDiff = []string{fmt.Sprintf("authz: remove grant granter=%s grantee=%s msg=%s", m.Granter, m.Grantee, valueOrUnknown(m.MsgTypeUrl))}
		preview.ExpectedEvents = []string{"message", "revoke"}
		preview.PotentialApprovals = []string{fmt.Sprintf("account permission removed: %s can no longer execute %s for %s", m.Grantee, valueOrUnknown(m.MsgTypeUrl), m.Granter)}
		preview.Signers = normalizeStrings([]string{m.Granter})
	case *authztypes.MsgExec:
		preview.Summary = fmt.Sprintf("execute %d authz message(s) as %s", len(m.Msgs), m.Grantee)
		preview.Parties = parties("grantee", m.Grantee)
		preview.StateDiff = []string{fmt.Sprintf("authz: execute nested messages using existing grants for grantee=%s", m.Grantee)}
		preview.ExpectedEvents = []string{"message", "exec"}
		for _, nested := range m.Msgs {
			preview.ExecutionMessages = append(preview.ExecutionMessages, "authz nested msg: "+valueOrUnknown(anyTypeURL(nested)))
		}
		preview.RiskNotes = []string{"requires an existing authz grant from each nested message signer to the grantee"}
		preview.Signers = normalizeStrings([]string{m.Grantee})
	case *feegranttypes.MsgGrantAllowance:
		allowanceType := anyTypeURL(m.Allowance)
		preview.Summary = fmt.Sprintf("grant fee allowance %s from %s to %s", valueOrUnknown(allowanceType), m.Granter, m.Grantee)
		preview.Parties = parties("granter", m.Granter, "grantee", m.Grantee)
		preview.StateDiff = []string{fmt.Sprintf("feegrant: create or replace spending allowance granter=%s grantee=%s allowance=%s", m.Granter, m.Grantee, valueOrUnknown(allowanceType))}
		preview.ExpectedEvents = []string{"message", "set_feegrant"}
		preview.PotentialApprovals = []string{fmt.Sprintf("spending allowance: %s may spend fees from %s under %s", m.Grantee, m.Granter, valueOrUnknown(allowanceType))}
		preview.Signers = normalizeStrings([]string{m.Granter})
	case *feegranttypes.MsgRevokeAllowance:
		preview.Summary = fmt.Sprintf("revoke fee allowance from %s to %s", m.Granter, m.Grantee)
		preview.Parties = parties("granter", m.Granter, "grantee", m.Grantee)
		preview.StateDiff = []string{fmt.Sprintf("feegrant: remove spending allowance granter=%s grantee=%s", m.Granter, m.Grantee)}
		preview.ExpectedEvents = []string{"message", "revoke_feegrant"}
		preview.PotentialApprovals = []string{fmt.Sprintf("spending allowance removed: %s can no longer spend fees from %s", m.Grantee, m.Granter)}
		preview.Signers = normalizeStrings([]string{m.Granter})
	case *nativeaccounttypes.MsgActivateAccount:
		preview.Summary = fmt.Sprintf("activate native account %s", m.AddressUser)
		preview.Parties = parties("account", m.AddressUser, "raw", m.AddressRaw)
		preview.Coins = appendUintCoin(preview.Coins, "activation_fee", m.AddressUser, m.FeePaid)
		preview.StateDiff = []string{
			fmt.Sprintf("native-account: create account %s with initial sequence 0", m.AddressUser),
			fmt.Sprintf("native-account: register public key type=%s", valueOrUnknown(m.PublicKeyType)),
		}
		preview.ExpectedEvents = []string{nativeaccounttypes.EventTypeAccountActivated}
		preview.Signers = normalizeStrings([]string{m.AddressUser})
	case *nativeaccounttypes.MsgUpdateAuthPolicy:
		preview.Summary = fmt.Sprintf("update auth policy for %s", m.AccountUser)
		preview.Parties = parties("account", m.AccountUser)
		preview.StateDiff = []string{fmt.Sprintf("native-account: replace auth policy for %s", m.AccountUser)}
		preview.PotentialApprovals = []string{"account permission: signer policy changes; review threshold/signers before signing"}
		preview.ExpectedEvents = []string{"native_account.auth_policy_updated"}
		preview.Signers = normalizeStrings(append([]string{m.AccountUser}, m.Signers...))
	case *nativeaccounttypes.MsgRotateKey:
		preview.Summary = fmt.Sprintf("rotate auth key for %s", m.AccountUser)
		preview.Parties = parties("account", m.AccountUser)
		preview.StateDiff = []string{fmt.Sprintf("native-account: replace key %s for %s", valueOrUnknown(m.OldKeyID), m.AccountUser)}
		preview.PotentialApprovals = []string{"account permission: key set changes; old key may stop authorizing future transactions"}
		preview.ExpectedEvents = []string{"native_account.key_rotated"}
		preview.Signers = normalizeStrings(append([]string{m.AccountUser}, m.Signers...))
	case *nativeaccounttypes.MsgRecoverAccount:
		preview = nativeAccountSimple(index, preview.TypeURL, "recover", m.AccountUser, m.Signers)
	case *nativeaccounttypes.MsgFreezeAccount:
		preview = nativeAccountSimple(index, preview.TypeURL, "freeze", m.AccountUser, m.Signers)
	case *nativeaccounttypes.MsgUnfreezeAccount:
		preview = nativeAccountSimple(index, preview.TypeURL, "unfreeze", m.AccountUser, m.Signers)
	case *nativeaccounttypes.MsgPayStorageDebt:
		preview.Summary = fmt.Sprintf("pay native account storage debt for %s", m.AccountUser)
		preview.Parties = parties("account", m.AccountUser)
		preview.Coins = appendUintCoin(preview.Coins, "storage_debt_payment", m.AccountUser, m.Amount)
		preview.StateDiff = []string{fmt.Sprintf("native-account: reduce storage debt for %s by %d naet", m.AccountUser, m.Amount)}
		preview.ExpectedEvents = []string{"native_account.storage_debt_paid"}
		preview.Signers = normalizeStrings(append([]string{m.AccountUser}, m.Signers...))
	case *nativeaccounttypes.MsgUpdateAccountMetadata:
		preview.Summary = fmt.Sprintf("update native account metadata for %s", m.AccountUser)
		preview.Parties = parties("account", m.AccountUser)
		preview.StateDiff = []string{fmt.Sprintf("native-account: replace account metadata for %s", m.AccountUser)}
		preview.ExpectedEvents = []string{"native_account.metadata_updated"}
		preview.Signers = normalizeStrings(append([]string{m.AccountUser}, m.Signers...))
	case *nominatorpooltypes.MsgDepositToStakingPool:
		preview.Summary = fmt.Sprintf("deposit %d naet to staking pool %s", m.Amount, m.PoolID)
		preview.Parties = parties("wallet", m.WalletAddress, "pool", m.PoolID)
		preview.Coins = appendUintCoin(preview.Coins, "staking_pool_deposit", m.WalletAddress, m.Amount)
		preview.StateDiff = []string{fmt.Sprintf("nominator-pool: increase stake for wallet=%s pool=%s by %d naet", m.WalletAddress, m.PoolID, m.Amount)}
		preview.ExpectedEvents = []string{"nominator_pool.deposit"}
		preview.Signers = normalizeStrings([]string{m.WalletAddress})
	case *nominatorpooltypes.MsgRequestPoolUnbond:
		preview.Summary = fmt.Sprintf("request unbond %d shares from pool %s", m.Shares, m.PoolID)
		preview.Parties = parties("owner", m.OwnerAddress, "pool", m.PoolID)
		preview.StateDiff = []string{fmt.Sprintf("nominator-pool: create unbond request %s for %d shares", valueOrUnknown(m.RequestID), m.Shares)}
		preview.ExpectedEvents = []string{"nominator_pool.unbond_requested"}
		preview.Signers = normalizeStrings([]string{m.OwnerAddress})
	case *nominatorpooltypes.MsgWithdrawPoolStake:
		preview.Summary = fmt.Sprintf("withdraw pool stake request %s from pool %s", m.RequestID, m.PoolID)
		preview.Parties = parties("owner", m.OwnerAddress, "caller_contract", m.CallerContractUser, "pool", m.PoolID)
		preview.StateDiff = []string{fmt.Sprintf("nominator-pool: finalize withdrawal request %s", valueOrUnknown(m.RequestID))}
		preview.ExpectedEvents = []string{"nominator_pool.withdrawn"}
		preview.Signers = normalizeStrings([]string{m.OwnerAddress})
	case *nominatorpooltypes.MsgTopUpPoolReserve:
		preview.Summary = fmt.Sprintf("top up pool %s reserve by %d naet", m.PoolID, m.Amount)
		preview.Parties = parties("payer", m.PayerAddress, "pool", m.PoolID)
		preview.Coins = appendUintCoin(preview.Coins, "pool_reserve_top_up", m.PayerAddress, m.Amount)
		preview.StateDiff = []string{fmt.Sprintf("nominator-pool: increase reserve for pool=%s by %d naet", m.PoolID, m.Amount)}
		preview.ExpectedEvents = []string{"nominator_pool.reserve_topped_up"}
		preview.Signers = normalizeStrings([]string{m.PayerAddress})
	case *nominatorpooltypes.MsgClaimPoolRewards:
		preview.Summary = fmt.Sprintf("claim pool rewards pool=%s owner=%s", m.PoolID, m.OwnerAddress)
		preview.Parties = parties("owner", m.OwnerAddress, "delegator", m.Delegator, "pool", m.PoolID)
		preview.StateDiff = []string{fmt.Sprintf("nominator-pool: move claimable rewards to owner=%s", m.OwnerAddress)}
		preview.ExpectedEvents = []string{"nominator_pool.rewards_claimed"}
		preview.Signers = normalizeStrings([]string{m.OwnerAddress, m.Delegator, m.Authority})
	case *nominatorpooltypes.MsgDelegateToValidator:
		preview.Summary = fmt.Sprintf("delegate %d naet from %s to validator %s", m.Amount, m.UserAddress, m.ValidatorAddress)
		preview.Parties = parties("user", m.UserAddress, "validator", m.ValidatorAddress, "authority", m.Authority)
		preview.Coins = appendUintCoin(preview.Coins, "delegate", m.UserAddress, m.Amount)
		preview.StateDiff = []string{fmt.Sprintf("nominator-pool: increase validator delegation validator=%s user=%s by %d naet", m.ValidatorAddress, m.UserAddress, m.Amount)}
		preview.ExpectedEvents = []string{"nominator_pool.delegated"}
		preview.Signers = normalizeStrings([]string{m.UserAddress, m.Authority})
	default:
		if contractPreview, ok := previewContractMsg(index, preview.TypeURL, msg); ok {
			return contractPreview
		}
		preview.StateDiff = []string{"unknown: static preview cannot infer exact state changes for this message type"}
		preview.ExpectedEvents = []string{"message"}
		preview.RiskNotes = []string{"run tx simulate against a node for keeper-level result before broadcast"}
		preview.PayloadFormat = "opaque"
		preview.RiskLabels = []string{"non-human-readable-payload"}
		if legacy, ok := msg.(sdk.LegacyMsg); ok {
			for _, signer := range legacy.GetSigners() {
				preview.Signers = append(preview.Signers, signer.String())
			}
			preview.Signers = normalizeStrings(preview.Signers)
		}
	}

	if len(preview.StateDiff) == 0 {
		preview.StateDiff = []string{"no persistent state change inferred by static preview"}
	}
	return preview
}

func previewFee(tx sdk.FeeTx) FeePreview {
	fee := FeePreview{
		Amount:     tx.GetFee().String(),
		GasLimit:   tx.GetGas(),
		FeePayer:   addressBytes(tx.FeePayer()),
		FeeGranter: addressBytes(tx.FeeGranter()),
	}
	if !tx.GetFee().Empty() {
		if fee.FeeGranter != "" {
			fee.FeeStateDiff = append(fee.FeeStateDiff, fmt.Sprintf("feegrant/bank: charge %s from fee granter %s if allowance is valid", tx.GetFee().String(), fee.FeeGranter))
		} else if fee.FeePayer != "" {
			fee.FeeStateDiff = append(fee.FeeStateDiff, fmt.Sprintf("bank: charge fee %s from fee payer %s", tx.GetFee().String(), fee.FeePayer))
		} else {
			fee.FeeStateDiff = append(fee.FeeStateDiff, fmt.Sprintf("bank: charge fee %s from transaction signer/account", tx.GetFee().String()))
		}
	}
	return fee
}

func nativeAccountSimple(index int, typeURL string, op string, account string, signers []string) MessagePreview {
	return MessagePreview{
		Index:          index,
		TypeURL:        typeURL,
		Summary:        fmt.Sprintf("%s native account %s", op, account),
		Parties:        parties("account", account),
		StateDiff:      []string{fmt.Sprintf("native-account: %s account %s", op, account)},
		ExpectedEvents: []string{"native_account." + op},
		Signers:        normalizeStrings(append([]string{account}, signers...)),
	}
}

func collectDAppAccessChanges(access *DAppAccessPreview, preview MessagePreview, dapps []string) {
	if access == nil || len(dapps) == 0 {
		return
	}
	for _, party := range preview.Parties {
		if !containsString(dapps, party.Address) {
			continue
		}
		for _, approval := range preview.PotentialApprovals {
			access.AccessChanges = append(access.AccessChanges, fmt.Sprintf("message[%d]: %s; matched_dapp=%s role=%s", preview.Index, approval, party.Address, party.Role))
		}
		if len(preview.PotentialApprovals) == 0 && strings.Contains(strings.ToLower(preview.Summary), "execute") {
			access.AccessChanges = append(access.AccessChanges, fmt.Sprintf("message[%d]: dApp/address %s participates as %s in %s", preview.Index, party.Address, party.Role, preview.TypeURL))
		}
	}
	access.AccessChanges = normalizeStrings(access.AccessChanges)
}

func buildQueryHints(opts Options) []string {
	hints := []string{
		"aetrad query authz grants-by-grantee <dapp-address>",
		"aetrad query feegrant grants-by-grantee <dapp-address>",
		"aetrad query avm contract <contract-address>",
	}
	if strings.TrimSpace(opts.Account) != "" {
		hints = append(hints, "aetrad query auth account "+strings.TrimSpace(opts.Account))
	}
	return hints
}

func existingAccessStatus(opts Options) string {
	if len(normalizeStrings(opts.KnownAccountPermissions)) > 0 ||
		len(normalizeStrings(opts.KnownSpendingAllowances)) > 0 ||
		len(normalizeStrings(opts.KnownIBCApprovals)) > 0 ||
		len(normalizeStrings(opts.KnownContractAuthorizations)) > 0 {
		return "provided_by_caller"
	}
	return "not_queried_offline"
}

func parties(roleAddress ...string) []Party {
	if len(roleAddress)%2 != 0 {
		return nil
	}
	out := make([]Party, 0, len(roleAddress)/2)
	for i := 0; i < len(roleAddress); i += 2 {
		addr := strings.TrimSpace(roleAddress[i+1])
		if addr == "" {
			continue
		}
		out = append(out, Party{Role: roleAddress[i], Address: addr})
	}
	return out
}

func appendUintCoin(coins []CoinChange, role string, address string, amount uint64) []CoinChange {
	if amount == 0 {
		return coins
	}
	return append(coins, CoinChange{Role: role, Address: strings.TrimSpace(address), Amount: fmt.Sprintf("%d naet", amount)})
}

func anyTypeURL(any interface{ GetTypeUrl() string }) string {
	if any == nil {
		return ""
	}
	return strings.TrimSpace(any.GetTypeUrl())
}

func msgTypeURL(msg sdk.Msg) string {
	if msg == nil {
		return "<nil>"
	}
	if typeURL := strings.TrimSpace(sdk.MsgTypeURL(msg)); typeURL != "" {
		return typeURL
	}
	return fmt.Sprintf("%T", msg)
}

func valueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func addressBytes(bz []byte) string {
	if len(bz) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(bz))
	if text != "" && strings.HasPrefix(text, "AE") {
		return text
	}
	return "0x" + hex.EncodeToString(bz)
}

func normalizeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		addUnique(seen, &out, value)
	}
	sort.Strings(out)
	return out
}

func addUnique(seen map[string]struct{}, out *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*out = append(*out, value)
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func previewContractMsg(index int, typeURL string, msg any) (MessagePreview, bool) {
	typeURL = strings.TrimSpace(typeURL)
	if !strings.HasPrefix(typeURL, "/l1.contracts.v1.") {
		return MessagePreview{}, false
	}
	preview := MessagePreview{
		Index:     index,
		TypeURL:   typeURL,
		StateDiff: []string{},
	}
	rv := reflect.Indirect(reflect.ValueOf(msg))
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		preview.Summary = "review " + typeURL
		preview.StateDiff = []string{"unknown: static preview cannot inspect contract message payload"}
		preview.ExpectedEvents = []string{"message"}
		return preview, true
	}

	switch typeURL {
	case "/l1.contracts.v1.MsgStoreCode":
		authority := contractFieldString(rv, "Authority")
		codeHash := contractFieldString(rv, "CodeHash")
		codeBytes := contractFieldUint64(rv, "CodeBytes")
		bytecode := contractFieldBytes(rv, "Bytecode")
		if len(bytecode) > 0 {
			codeBytes = uint64(len(bytecode))
		}
		preview.Summary = fmt.Sprintf("store AVM code hash=%s bytes=%d", valueOrUnknown(codeHash), codeBytes)
		preview.Parties = parties("authority", authority)
		preview.StateDiff = []string{fmt.Sprintf("contracts: store code record code_hash=%s code_bytes=%d", valueOrUnknown(codeHash), codeBytes)}
		preview.ExpectedEvents = []string{contractsEventCodeStored}
		preview.Signers = normalizeStrings([]string{authority})
		preview.PayloadFormat = "structured"
	case "/l1.contracts.v1.MsgDeployContract":
		creator := contractFieldString(rv, "Creator")
		codeID := contractFieldString(rv, "CodeID")
		admin := contractFieldString(rv, "Admin")
		initialBalance := contractFieldUint64(rv, "InitialBalance")
		height := contractFieldUint64(rv, "Height")
		preview.Summary = fmt.Sprintf("deploy AVM contract code_id=%s creator=%s", valueOrUnknown(codeID), valueOrUnknown(creator))
		preview.Parties = parties("creator", creator, "admin", admin)
		preview.Coins = appendUintCoin(preview.Coins, "initial_balance", "", initialBalance)
		preview.StateDiff = []string{
			fmt.Sprintf("contracts: instantiate contract from code_id=%s", valueOrUnknown(codeID)),
			fmt.Sprintf("contracts: initialize contract storage and metadata at height=%d", height),
		}
		if initialBalance > 0 {
			preview.StateDiff = append(preview.StateDiff, fmt.Sprintf("contracts/bank: move initial balance %d naet into contract account", initialBalance))
		}
		if strings.TrimSpace(admin) != "" {
			preview.PotentialApprovals = append(preview.PotentialApprovals, fmt.Sprintf("contract authorization: admin %s can manage upgrades/admin flows", admin))
		}
		preview.ExpectedEvents = []string{contractsEventInstantiated}
		preview.Signers = normalizeStrings([]string{creator})
		preview.PayloadFormat = "structured"
	case "/l1.contracts.v1.MsgExecuteExternal":
		sender := contractFieldString(rv, "Sender")
		contractAddress := contractFieldString(rv, "ContractAddress")
		payload := contractFieldBytes(rv, "Payload")
		funds := contractFieldUint64(rv, "Funds")
		gasLimit := contractFieldUint64(rv, "GasLimit")
		height := contractFieldUint64(rv, "Height")
		preview.Summary = fmt.Sprintf("execute AVM contract %s as %s", valueOrUnknown(contractAddress), valueOrUnknown(sender))
		preview.Parties = parties("sender", sender, "contract", contractAddress)
		preview.Coins = appendUintCoin(preview.Coins, "funds", contractAddress, funds)
		preview.StateDiff = []string{
			fmt.Sprintf("contracts: contract storage may change for %s", valueOrUnknown(contractAddress)),
			fmt.Sprintf("contracts: receipt expected for external execution at height=%d", height),
		}
		if json.Valid(payload) {
			preview.PayloadFormat = "json"
		} else if len(payload) == 0 {
			preview.PayloadFormat = "structured"
		} else {
			preview.PayloadFormat = "binary"
			preview.RiskLabels = append(preview.RiskLabels, "non-human-readable-payload")
			preview.RiskNotes = append(preview.RiskNotes, "external payload is not valid JSON")
		}
		if funds > 0 {
			preview.StateDiff = append(preview.StateDiff, fmt.Sprintf("contracts/bank: transfer %d naet from %s to contract %s", funds, sender, contractAddress))
		}
		preview.ExpectedEvents = []string{contractsEventExecuted}
		preview.ExecutionMessages = []string{fmt.Sprintf("AVM external execution payload_bytes=%d gas_limit=%d", len(payload), gasLimit)}
		preview.Signers = normalizeStrings([]string{sender})
	case "/l1.contracts.v1.MsgExecuteInternal", "/l1.contracts.v1.MsgSendInternalMessage":
		nested := contractFieldStruct(rv, "Message")
		if nested.IsValid() && nested.Kind() == reflect.Struct {
			preview.Summary = fmt.Sprintf("internal AVM message to %s", valueOrUnknown(contractFieldString(nested, "DestinationAccount")))
			preview.StateDiff = []string{fmt.Sprintf("contracts: process internal message %s and update destination contract/account state", valueOrUnknown(contractFieldString(nested, "MessageID")))}
			preview.ExecutionMessages = []string{internalMessageSummaryGeneric(nested)}
		} else {
			preview.Summary = "internal AVM message"
			preview.StateDiff = []string{"contracts: process internal message and update destination contract/account state"}
		}
		if typeURL == "/l1.contracts.v1.MsgExecuteInternal" {
			preview.ExpectedEvents = []string{contractsEventExecuted}
		} else {
			preview.ExpectedEvents = []string{contractsEventInternalSent}
		}
		preview.PayloadFormat = "structured"
	case "/l1.contracts.v1.MsgUpdateContractParams":
		authority := contractFieldString(rv, "Authority")
		preview.Summary = "update AVM contract parameters"
		preview.Parties = parties("authority", authority)
		preview.StateDiff = []string{"contracts: replace module params"}
		preview.ExpectedEvents = []string{"message", contractsEventParamsUpdated}
		preview.Signers = normalizeStrings([]string{authority})
		preview.PayloadFormat = "structured"
	default:
		return MessagePreview{}, false
	}
	if len(preview.StateDiff) == 0 {
		preview.StateDiff = []string{"no persistent state change inferred by static preview"}
	}
	return preview, true
}

func contractFieldString(rv reflect.Value, name string) string {
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return ""
	}
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return ""
		}
		field = field.Elem()
	}
	if field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

func contractFieldUint64(rv reflect.Value, name string) uint64 {
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return 0
		}
		field = field.Elem()
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return field.Uint()
	default:
		return 0
	}
}

func contractFieldBytes(rv reflect.Value, name string) []byte {
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return nil
	}
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return nil
		}
		field = field.Elem()
	}
	if field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.Uint8 {
		return nil
	}
	return append([]byte(nil), field.Bytes()...)
}

func contractFieldStruct(rv reflect.Value, name string) reflect.Value {
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}
	}
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return reflect.Value{}
		}
		field = field.Elem()
	}
	return field
}

func internalMessageSummaryGeneric(rv reflect.Value) string {
	return fmt.Sprintf(
		"internal message id=%s source=%s destination=%s funds=%d opcode=%d query_id=%d gas_limit=%d bounce=%t",
		valueOrUnknown(contractFieldString(rv, "MessageID")),
		valueOrUnknown(contractFieldString(rv, "SourceContractUser")),
		valueOrUnknown(contractFieldString(rv, "DestinationAccount")),
		contractFieldUint64(rv, "Funds"),
		contractFieldUint64(rv, "Opcode"),
		contractFieldUint64(rv, "QueryID"),
		contractFieldUint64(rv, "GasLimit"),
		contractFieldBool(rv, "Bounce"),
	)
}

func contractFieldBool(rv reflect.Value, name string) bool {
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return false
	}
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return false
		}
		field = field.Elem()
	}
	return field.Kind() == reflect.Bool && field.Bool()
}

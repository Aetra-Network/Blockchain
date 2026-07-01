package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/sovereign-l1/l1/app/txpreview"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
)

const (
	flagPreviewBinary             = "binary"
	flagPreviewDApp               = "dapp-address"
	flagPreviewAccount            = "account"
	flagPreviewNoQueries          = "no-query-hints"
	flagPreviewKnownAuthz         = "existing-account-permission"
	flagPreviewKnownFeegrant      = "existing-spending-allowance"
	flagPreviewKnownIBC           = "existing-ibc-approval"
	flagPreviewKnownContractAuthz = "existing-contract-authorization"
	flagPreviewOriginDApp         = "origin-dapp"
	flagPreviewMessageDomain      = "message-domain"
	flagPreviewExpectedSender     = "expected-sender"
	flagPreviewPurpose            = "purpose"
	flagPreviewDangerPermission   = "dangerous-permission"
	flagPreviewSecurityBadgeJSON  = "security-badge-json"
	flagPreviewBlockOnMismatch    = "block-on-mismatch"
)

func NewTxPreviewCmd(txConfig client.TxConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preview [tx-file|-]",
		Short: "Preview transaction effects before signing or broadcasting",
		Long: strings.TrimSpace(`
Preview decodes a generated or signed transaction and prints a human-readable JSON
intent report without writing chain state. Use it before signing to inspect balance
changes, fees, message effects, expected events, execution messages, and approval
surfaces such as authz grants and fee allowances.

For keeper-level gas/result checks, run tx simulate against a node after inspecting
this static pre-sign preview.
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := readPreviewInput(args[0])
			if err != nil {
				return err
			}
			binary, _ := cmd.Flags().GetBool(flagPreviewBinary)
			var tx sdk.Tx
			if binary {
				tx, err = txConfig.TxDecoder()(raw)
			} else {
				tx, err = txConfig.TxJSONDecoder()(raw)
			}
			if err != nil {
				return fmt.Errorf("decode tx for preview: %w", err)
			}
			dapps, _ := cmd.Flags().GetStringArray(flagPreviewDApp)
			account, _ := cmd.Flags().GetString(flagPreviewAccount)
			knownAuthz, _ := cmd.Flags().GetStringArray(flagPreviewKnownAuthz)
			knownFeegrant, _ := cmd.Flags().GetStringArray(flagPreviewKnownFeegrant)
			knownIBC, _ := cmd.Flags().GetStringArray(flagPreviewKnownIBC)
			knownContractAuthz, _ := cmd.Flags().GetStringArray(flagPreviewKnownContractAuthz)
			originDApp, _ := cmd.Flags().GetString(flagPreviewOriginDApp)
			messageDomain, _ := cmd.Flags().GetString(flagPreviewMessageDomain)
			expectedSender, _ := cmd.Flags().GetString(flagPreviewExpectedSender)
			purpose, _ := cmd.Flags().GetString(flagPreviewPurpose)
			dangerousPermissions, _ := cmd.Flags().GetStringArray(flagPreviewDangerPermission)
			blockOnMismatch, _ := cmd.Flags().GetBool(flagPreviewBlockOnMismatch)
			badgeJSON, _ := cmd.Flags().GetString(flagPreviewSecurityBadgeJSON)
			var securityBadge *contracttypes.ContractSecurityBadge
			if strings.TrimSpace(badgeJSON) != "" {
				securityBadge = new(contracttypes.ContractSecurityBadge)
				if err := json.Unmarshal([]byte(badgeJSON), securityBadge); err != nil {
					return fmt.Errorf("decode security badge JSON: %w", err)
				}
			}
			noQueryHints, _ := cmd.Flags().GetBool(flagPreviewNoQueries)
			report, err := txpreview.Build(tx, txpreview.Options{
				Account:                     account,
				DAppAddresses:               dapps,
				KnownAccountPermissions:     knownAuthz,
				KnownSpendingAllowances:     knownFeegrant,
				KnownIBCApprovals:           knownIBC,
				KnownContractAuthorizations: knownContractAuthz,
				IncludeQueryHints:           !noQueryHints,
				OriginDApp:                  originDApp,
				IntendedMessageDomain:       messageDomain,
				ExpectedSender:              expectedSender,
				Purpose:                     purpose,
				DangerousPermissions:        dangerousPermissions,
				BlockOnMismatch:             blockOnMismatch,
				SecurityBadge:               securityBadge,
			})
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, report)
		},
	}
	cmd.Flags().Bool(flagPreviewBinary, false, "decode input as binary tx bytes instead of tx JSON")
	cmd.Flags().StringArray(flagPreviewDApp, nil, "dApp, grantee, contract, or spender address to highlight; may be repeated")
	cmd.Flags().String(flagPreviewAccount, "", "account address whose existing permission surfaces should be highlighted in query hints")
	cmd.Flags().StringArray(flagPreviewKnownAuthz, nil, "known existing account permission/authz for this dApp; may be repeated")
	cmd.Flags().StringArray(flagPreviewKnownFeegrant, nil, "known existing feegrant/spending allowance for this dApp; may be repeated")
	cmd.Flags().StringArray(flagPreviewKnownIBC, nil, "known existing IBC approval for this dApp; may be repeated")
	cmd.Flags().StringArray(flagPreviewKnownContractAuthz, nil, "known existing contract authorization/admin/capability for this dApp; may be repeated")
	cmd.Flags().String(flagPreviewOriginDApp, "", "origin dApp or site that initiated the signing request")
	cmd.Flags().String(flagPreviewMessageDomain, "", "intended message domain or contract binding for the signing request")
	cmd.Flags().String(flagPreviewExpectedSender, "", "expected account or contract sender for the signing request")
	cmd.Flags().String(flagPreviewPurpose, "", "human-readable signing purpose shown to the user")
	cmd.Flags().StringArray(flagPreviewDangerPermission, nil, "dangerous permission or approval to surface in the signing preview; may be repeated")
	cmd.Flags().String(flagPreviewSecurityBadgeJSON, "", "optional contract security badge JSON to include in the preview")
	cmd.Flags().Bool(flagPreviewBlockOnMismatch, true, "block signing when the origin, domain, sender, or security badge mismatches")
	cmd.Flags().Bool(flagPreviewNoQueries, false, "omit live query hints for existing grants/allowances")
	return cmd
}

func readPreviewInput(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("tx file is required")
	}
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

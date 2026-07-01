package txpreview

import (
	"strings"
	"testing"

	gogoany "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	feegranttypes "github.com/cosmos/cosmos-sdk/x/feegrant"

	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
)

type testTx struct {
	msgs       []sdk.Msg
	fee        sdk.Coins
	gas        uint64
	feePayer   []byte
	feeGranter []byte
	memo       string
}

func (tx testTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx testTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func (tx testTx) GetGas() uint64 {
	return tx.gas
}

func (tx testTx) GetFee() sdk.Coins {
	return tx.fee
}

func (tx testTx) FeePayer() []byte {
	return tx.feePayer
}

func (tx testTx) FeeGranter() []byte {
	return tx.feeGranter
}

func (tx testTx) GetMemo() string {
	return tx.memo
}

func TestPreviewBankSendShowsBalanceDiffAndFee(t *testing.T) {
	tx := testTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{
			FromAddress: "AEfrom",
			ToAddress:   "AEto",
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("naet", 42)),
		}},
		fee:      sdk.NewCoins(sdk.NewInt64Coin("naet", 2)),
		gas:      12345,
		feePayer: []byte("AEfrom"),
		memo:     "preview test",
	}

	report, err := Build(tx, Options{IncludeQueryHints: true})
	require.NoError(t, err)
	require.Equal(t, ModeNativeTxPreview, report.Mode)
	require.Equal(t, MutationNone, report.Mutation)
	require.Equal(t, "2naet", report.Fee.Amount)
	require.Equal(t, uint64(12345), report.Fee.GasLimit)
	require.Equal(t, []string{"AEfrom"}, report.Signers)
	require.Len(t, report.Messages, 1)
	require.Contains(t, report.Messages[0].Summary, "send 42naet")
	require.Contains(t, report.Messages[0].StateDiff, "bank: debit 42naet from AEfrom")
	require.Contains(t, report.Messages[0].StateDiff, "bank: credit 42naet to AEto")
	require.Contains(t, report.Messages[0].ExpectedEvents, "transfer")
}

func TestPreviewAuthzAndFeegrantShowDAppAccess(t *testing.T) {
	dapp := "AEdapp"
	tx := testTx{
		msgs: []sdk.Msg{
			&authztypes.MsgGrant{
				Granter: "AEowner",
				Grantee: dapp,
				Grant: authztypes.Grant{
					Authorization: &gogoany.Any{TypeUrl: "/cosmos.bank.v1beta1.MsgSend"},
				},
			},
			&feegranttypes.MsgGrantAllowance{
				Granter:   "AEowner",
				Grantee:   dapp,
				Allowance: &gogoany.Any{TypeUrl: "/cosmos.feegrant.v1beta1.BasicAllowance"},
			},
		},
	}

	report, err := Build(tx, Options{DAppAddresses: []string{dapp}, IncludeQueryHints: true})
	require.NoError(t, err)
	require.Len(t, report.Messages, 2)
	require.Contains(t, report.Messages[0].PotentialApprovals[0], "account permission")
	require.Contains(t, report.Messages[1].PotentialApprovals[0], "spending allowance")
	require.Equal(t, "not_queried_offline", report.DAppAccess.ExistingAccessStatus)
	require.Len(t, report.DAppAccess.AccessChanges, 2)
	require.Contains(t, report.DAppAccess.QueryHints, "aetrad query authz grants-by-grantee <dapp-address>")
}

func TestPreviewContractExecuteShowsExecutionMessage(t *testing.T) {
	msg := &fakeContractExecuteExternal{
		Sender:          "AEsender",
		ContractAddress: "AEcontract",
		Payload:         []byte(`{"op":"ping"}`),
		Funds:           7,
		GasLimit:        100000,
		Height:          10,
	}

	report, ok := previewContractMsg(0, "/l1.contracts.v1.MsgExecuteExternal", msg)
	require.True(t, ok)
	require.Contains(t, report.Summary, "execute AVM contract")
	require.Contains(t, report.StateDiff, "contracts: contract storage may change for AEcontract")
	require.Contains(t, report.ExpectedEvents, contractsEventExecuted)
	require.Len(t, report.ExecutionMessages, 1)
	require.Contains(t, report.ExecutionMessages[0], "payload_bytes=13")
}

func TestPreviewContractExecuteFlagsBinaryPayload(t *testing.T) {
	msg := &fakeContractExecuteExternal{
		Sender:          "AEsender",
		ContractAddress: "AEcontract",
		Payload:         []byte{0xff, 0x00, 0x13},
		Funds:           7,
		GasLimit:        100000,
		Height:          10,
	}

	report, ok := previewContractMsg(0, "/l1.contracts.v1.MsgExecuteExternal", msg)
	require.True(t, ok)
	require.Equal(t, "binary", report.PayloadFormat)
	require.Contains(t, report.RiskLabels, "non-human-readable-payload")
	require.Contains(t, report.RiskNotes, "external payload is not valid JSON")
}

func TestBuildBlocksOnOriginAndSenderMismatch(t *testing.T) {
	tx := testTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{
			FromAddress: "AEsender",
			ToAddress:   "AEto",
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("naet", 1)),
		}},
	}
	badge := &contracttypes.ContractSecurityBadge{
		ContractAddress:  "AEto",
		Badge:            contracttypes.SecurityBadgeCritical,
		Verified:         false,
		RiskScoreBps:     9000,
		Categories:       []string{contracttypes.SecurityAttestationCategoryPhishingLinked},
		Flags:            []string{"linked-to-phishing-graph"},
		RelatedAddresses: []string{"AEphish"},
	}

	report, err := Build(tx, Options{
		OriginDApp:            "https://dapp.example",
		IntendedMessageDomain: "https://other.example",
		ExpectedSender:        "AEexpected",
		Purpose:               "approve transfer",
		BlockOnMismatch:       true,
		SecurityBadge:         badge,
	})
	require.NoError(t, err)
	require.True(t, report.Signing.Blocked)
	require.Contains(t, report.Signing.Warnings, "origin mismatch: origin https://dapp.example does not match intended domain https://other.example")
	require.Contains(t, strings.Join(report.Signing.Warnings, "\n"), "sender mismatch: expected AEexpected")
	require.Contains(t, report.Signing.RiskLabels, "security-badge:critical")
	require.Contains(t, report.Warnings, "signing blocked: origin/domain mismatch; sender mismatch; critical security badge")
}

type fakeContractExecuteExternal struct {
	Sender          string
	ContractAddress string
	Payload         []byte
	Funds           uint64
	GasLimit        uint64
	Height          uint64
}

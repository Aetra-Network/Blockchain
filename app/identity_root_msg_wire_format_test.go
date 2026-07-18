package app_test

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app"
	"github.com/sovereign-l1/l1/app/addressing"
	identityroottypes "github.com/sovereign-l1/l1/x/identity-root/types"
)

// TestIdentityRootMsgSendToNameCollectionDecodesOverTheWire is the wire-format
// guard for ANS Phase A's REGISTER/TOPUP entry point, and it exists because a
// live adversarial test found the module dead on a running node: the .aet
// auction could never open because a signed MsgSendToNameCollection tx could not
// be DECODED.
//
// Root cause: the four hand-rolled identity-root Msg types implemented Reset(),
// String() and ProtoMessage() but NOT the gogoproto Descriptor() method. The
// v0.54.3 tx decoder runs unknownproto.RejectUnknownFields over every TxBody
// message (x/auth/tx/decoder.go), and that walker calls Descriptor() to load the
// message's field set; a type without it is rejected AT DECODE, before routing
// or signature verification, with "does not have a Descriptor() method". So the
// module booted and its get-methods returned correct prices, but no auction
// could ever open.
//
// A keeper test cannot see this: it calls the handler directly in Go and never
// touches the wire. Even a GetMsgV1Signers-only test (app/aez_msg_wire_format_
// test.go's shape) cannot see it, because the x/tx signing context resolves
// signers off protoreflect descriptors, not the gogoproto Descriptor() method.
// Only the real TxConfig encode -> TxDecoder round trip exercises the failing
// path. This test does exactly that, then resolves the decoded message's signer
// through the app's own signing context (which routes CustomGetSigners in
// app/keeperconfig/tx.go).
//
// Against the pre-fix tree (Descriptor() methods absent) the TxDecoder assertion
// fails with the "does not have a Descriptor() method" tx parse error.
func TestIdentityRootMsgSendToNameCollectionDecodesOverTheWire(t *testing.T) {
	testApp := app.Setup(t, false)
	txConfig := testApp.TxConfig()

	senderAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x42}, 20))
	require.NoError(t, err)

	msg := &identityroottypes.MsgSendToNameCollection{
		Sender:     senderAE,
		Opcode:     identityroottypes.OpcodeRegister,
		Comment:    "alice",
		AmountNaet: 5000,
		Height:     1,
	}

	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))

	// Encode as a real transaction would. This already worked pre-fix; the
	// decode below is the regression.
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	require.NotEmpty(t, txBytes, "encoded to empty bytes; struct tags/descriptor are missing")

	// Decode through the app's real TxDecoder -- the RejectUnknownFields walker
	// here calls Descriptor() on the body message. This is the exact path
	// baseapp runs on every incoming tx, and the one that was rejecting the
	// message on the live node.
	decodedTx, err := txConfig.TxDecoder()(txBytes)
	require.NoError(t, err, "MsgSendToNameCollection must decode over the wire; a missing Descriptor() method rejects it here")

	msgs := decodedTx.GetMsgs()
	require.Len(t, msgs, 1)
	decoded, ok := msgs[0].(*identityroottypes.MsgSendToNameCollection)
	require.True(t, ok, "decoded message must route back to the concrete identity-root type")
	require.Equal(t, senderAE, decoded.Sender)
	require.Equal(t, identityroottypes.OpcodeRegister, decoded.Opcode)
	require.Equal(t, "alice", decoded.Comment)
	require.Equal(t, uint64(5000), decoded.AmountNaet)

	// And the decoded message resolves its signer to the sender field, through
	// the same signing context baseapp verifies signatures against.
	signers, _, err := testApp.AppCodec().GetMsgV1Signers(decoded)
	require.NoError(t, err, "signer must resolve for a routable tx")
	require.Len(t, signers, 1)
	expected, err := addressing.Parse(senderAE)
	require.NoError(t, err)
	require.Equal(t, expected, signers[0], "signer must be the parsed bytes of the sender field")
}

// TestIdentityRootAuctionMsgsAllDecodeOverTheWire extends the guard to the other
// two auction-driving messages that shared the identical missing-Descriptor()
// defect, so a future removal of any one Descriptor() method is caught here
// rather than on a validator. (MsgUpdatePriceTable is gov-gated and carries no
// Height, but shares the decode path, so it is covered too.)
func TestIdentityRootAuctionMsgsAllDecodeOverTheWire(t *testing.T) {
	testApp := app.Setup(t, false)
	txConfig := testApp.TxConfig()

	bidderAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x24}, 20))
	require.NoError(t, err)
	ownerAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x25}, 20))
	require.NoError(t, err)

	authorityAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x17}, 20))
	require.NoError(t, err)

	cases := []struct {
		name string
		msg  sdk.Msg
	}{
		{
			name: "MsgPlaceBid",
			msg:  &identityroottypes.MsgPlaceBid{Bidder: bidderAE, Name: "alice.aet", AmountNaet: 6000, Height: 1},
		},
		{
			name: "MsgStartAuction",
			msg:  &identityroottypes.MsgStartAuction{Owner: ownerAE, Name: "alice.aet", StartPriceNaet: 1000, DurationDays: 7, Height: 1},
		},
		{
			name: "MsgUpdatePriceTable",
			msg:  &identityroottypes.MsgUpdatePriceTable{Authority: authorityAE, MinLabelLens: []uint32{3}, PricesNaet: []string{"5000"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := txConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(tc.msg))
			txBytes, err := txConfig.TxEncoder()(builder.GetTx())
			require.NoError(t, err)
			require.NotEmpty(t, txBytes)
			_, err = txConfig.TxDecoder()(txBytes)
			require.NoError(t, err, "%s must decode over the wire (missing Descriptor() rejects it here)", tc.name)
		})
	}
}

// TestIdentityRootPhaseBMsgsDecodeAndResolveSigner is the ANS Phase B wire-format
// guard: the three new hand-rolled messages (MsgAttachDomain, MsgDetachDomain,
// MsgCreateSubdomain) must each survive the real TxConfig encode -> TxDecoder
// round trip (the RejectUnknownFields walker calls Descriptor() here, which a
// keeper test never exercises) AND resolve their signer to the "owner" field
// through the app's signing context (the CustomGetSigners entries in
// app/keeperconfig/tx.go). Against a tree missing any of these messages'
// Descriptor()/CustomGetSigners wiring, this fails exactly as the Phase A defect
// did on a live node.
func TestIdentityRootPhaseBMsgsDecodeAndResolveSigner(t *testing.T) {
	testApp := app.Setup(t, false)
	txConfig := testApp.TxConfig()

	ownerAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x51}, 20))
	require.NoError(t, err)
	targetAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x52}, 20))
	require.NoError(t, err)

	cases := []struct {
		name string
		msg  sdk.Msg
	}{
		{
			name: "MsgAttachDomain",
			msg:  &identityroottypes.MsgAttachDomain{Owner: ownerAE, Fqdn: "alice.aet", Target: targetAE, Height: 1},
		},
		{
			name: "MsgDetachDomain",
			msg:  &identityroottypes.MsgDetachDomain{Owner: ownerAE, Fqdn: "alice.aet", Height: 1},
		},
		{
			name: "MsgCreateSubdomain",
			msg:  &identityroottypes.MsgCreateSubdomain{Owner: ownerAE, ParentName: "alice.aet", Label: "test", Height: 1},
		},
	}
	expectedSigner, err := addressing.Parse(ownerAE)
	require.NoError(t, err)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := txConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(tc.msg))

			txBytes, err := txConfig.TxEncoder()(builder.GetTx())
			require.NoError(t, err)
			require.NotEmpty(t, txBytes, "%s encoded to empty bytes; struct tags/descriptor missing", tc.name)

			decodedTx, err := txConfig.TxDecoder()(txBytes)
			require.NoError(t, err, "%s must decode over the wire; a missing Descriptor() rejects it here", tc.name)

			msgs := decodedTx.GetMsgs()
			require.Len(t, msgs, 1)

			signers, _, err := testApp.AppCodec().GetMsgV1Signers(msgs[0])
			require.NoError(t, err, "%s signer must resolve for a routable tx", tc.name)
			require.Len(t, signers, 1)
			require.Equal(t, expectedSigner, signers[0], "%s signer must be the parsed bytes of the owner field", tc.name)
		})
	}
}

// TestIdentityRootDisownAttachmentDecodesAndResolvesTarget is the wire-format
// guard for FIX A's MsgDisownAttachment. Unlike the owner-signed Phase B
// messages, its signer resolves to the TARGET field (the wallet authorizing the
// self-detach of an attachment aimed at its own account), so this both proves the
// message decodes over the wire (Descriptor() present) and that CustomGetSigners
// routes the signer to "target" rather than an owner field it does not have.
func TestIdentityRootDisownAttachmentDecodesAndResolvesTarget(t *testing.T) {
	testApp := app.Setup(t, false)
	txConfig := testApp.TxConfig()

	targetAE, err := addressing.FormatUserFriendly(bytes.Repeat([]byte{0x63}, 20))
	require.NoError(t, err)

	msg := &identityroottypes.MsgDisownAttachment{Target: targetAE, Height: 1}

	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))

	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	require.NotEmpty(t, txBytes, "MsgDisownAttachment encoded to empty bytes; struct tags/descriptor missing")

	decodedTx, err := txConfig.TxDecoder()(txBytes)
	require.NoError(t, err, "MsgDisownAttachment must decode over the wire; a missing Descriptor() rejects it here")

	msgs := decodedTx.GetMsgs()
	require.Len(t, msgs, 1)
	decoded, ok := msgs[0].(*identityroottypes.MsgDisownAttachment)
	require.True(t, ok, "decoded message must route back to the concrete MsgDisownAttachment type")
	require.Equal(t, targetAE, decoded.Target)

	signers, _, err := testApp.AppCodec().GetMsgV1Signers(decoded)
	require.NoError(t, err, "MsgDisownAttachment signer must resolve for a routable tx")
	require.Len(t, signers, 1)
	expected, err := addressing.Parse(targetAE)
	require.NoError(t, err)
	require.Equal(t, expected, signers[0], "signer must be the parsed bytes of the target field")
}

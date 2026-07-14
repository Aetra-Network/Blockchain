package addressing_test

import (
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
)

func nestedSendMsgs(n int) []sdk.Msg {
	out := make([]sdk.Msg, n)
	for i := range out {
		out[i] = &banktypes.MsgSend{
			FromAddress: addressing.FormatAccAddress(sdk.AccAddress(bytes20(0x01))),
			ToAddress:   addressing.FormatAccAddress(sdk.AccAddress(bytes20(0x02))),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("naet", 1)),
		}
	}
	return out
}

// TestCountMessagesUnwrapsNestedAuthzMsgExec is the direct regression test for
// FINDING-013: a single top-level authz.MsgExec wrapping N inner messages
// must no longer be counted as 1 message. This codebase's WalkMessages visits
// the wrapper message itself in addition to every unwrapped child (matching
// the pre-existing app/txhandlers walkTxMessages convention used by
// RejectDirectUserStakingDecorator/collectTxSigners), so the effective count
// is N+1, not 1 and not N.
func TestCountMessagesUnwrapsNestedAuthzMsgExec(t *testing.T) {
	const n = 100
	grantee := sdk.AccAddress(bytes20(0x03))
	execMsg := authz.NewMsgExec(grantee, nestedSendMsgs(n))

	count, err := addressing.CountMessages([]sdk.Msg{&execMsg})
	require.NoError(t, err)
	require.Equal(t, uint64(n+1), count,
		"must count the MsgExec wrapper plus all %d nested messages, not just 1", n)
}

// TestCountMessagesFlatMessagesUnchanged locks in that ordinary, non-nested
// message counting is unaffected: N top-level messages still count as
// exactly N (parity with pre-fix behavior for the common case).
func TestCountMessagesFlatMessagesUnchanged(t *testing.T) {
	const n = 5
	count, err := addressing.CountMessages(nestedSendMsgs(n))
	require.NoError(t, err)
	require.Equal(t, uint64(n), count)
}

// TestCountMessagesHandlesMultipleTopLevelExecs mirrors a tx carrying several
// top-level MsgExec envelopes, each wrapping its own inner messages -- every
// wrapper and every nested message must be counted.
func TestCountMessagesHandlesMultipleTopLevelExecs(t *testing.T) {
	grantee := sdk.AccAddress(bytes20(0x04))
	exec1 := authz.NewMsgExec(grantee, nestedSendMsgs(3))
	exec2 := authz.NewMsgExec(grantee, nestedSendMsgs(4))

	count, err := addressing.CountMessages([]sdk.Msg{&exec1, &exec2})
	require.NoError(t, err)
	// 2 wrappers + 3 + 4 nested = 9.
	require.Equal(t, uint64(9), count)
}

// TestCountMessagesHandlesDoublyNestedExec covers a MsgExec wrapping another
// MsgExec (a grantee re-delegating), verifying the walk recurses to every
// depth rather than stopping after one level of unwrapping.
func TestCountMessagesHandlesDoublyNestedExec(t *testing.T) {
	innerGrantee := sdk.AccAddress(bytes20(0x05))
	outerGrantee := sdk.AccAddress(bytes20(0x06))

	innerExec := authz.NewMsgExec(innerGrantee, nestedSendMsgs(2))
	outerExec := authz.NewMsgExec(outerGrantee, []sdk.Msg{&innerExec})

	count, err := addressing.CountMessages([]sdk.Msg{&outerExec})
	require.NoError(t, err)
	// outerExec + innerExec + 2 nested sends = 4.
	require.Equal(t, uint64(4), count)
}

// TestWalkMessagesVisitsWrapperBeforeChildren verifies visitation order: the
// wrapper message is observed before its unwrapped children.
func TestWalkMessagesVisitsWrapperBeforeChildren(t *testing.T) {
	grantee := sdk.AccAddress(bytes20(0x07))
	execMsg := authz.NewMsgExec(grantee, nestedSendMsgs(2))

	var order []string
	err := addressing.WalkMessages([]sdk.Msg{&execMsg}, func(msg sdk.Msg) error {
		order = append(order, sdk.MsgTypeURL(msg))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"/cosmos.authz.v1beta1.MsgExec",
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.bank.v1beta1.MsgSend",
	}, order)
}

// TestWalkMessagesStopsAtFirstError verifies traversal short-circuits on the
// first error, without visiting later siblings.
func TestWalkMessagesStopsAtFirstError(t *testing.T) {
	boom := errors.New("boom")
	visited := 0

	err := addressing.WalkMessages(nestedSendMsgs(5), func(sdk.Msg) error {
		visited++
		if visited == 2 {
			return boom
		}
		return nil
	})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 2, visited)
}

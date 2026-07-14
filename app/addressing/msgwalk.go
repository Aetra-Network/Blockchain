package addressing

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NestedMsgProvider is implemented by wrapper messages that carry other
// sdk.Msg values inside them -- most notably authz.MsgExec, whose
// GetMessages() unpacks the messages a grantee is executing on a granter's
// behalf. Ante-level checks that only range over tx.GetMsgs() see the
// wrapper message and never its contents, so any check meant to apply to
// "every message that will execute" (message-count caps, per-message fees,
// zero-address / reserved-recipient guards, address-policy field checks,
// ...) must walk through this interface to see the real, effective message
// set (FINDING-013, FINDING-014).
type NestedMsgProvider interface {
	GetMessages() ([]sdk.Msg, error)
}

// WalkMessages calls visit for every message in msgs, and recursively for
// every message nested inside a wrapper message (e.g. authz.MsgExec), so
// callers observe the full, effective set of messages a tx will execute
// instead of only the top-level envelope. The wrapper message itself is
// visited first, before its children, at every nesting level. Traversal
// stops at the first error returned by visit or by unwrapping a nested
// message.
//
// app/txhandlers has its own unexported equivalent (walkTxMessages, used by
// RejectDirectUserStakingDecorator / collectTxSigners). It is re-declared
// here rather than imported: app/txhandlers already depends on this package
// (storage-rent address formatting), so importing it back here would create
// an import cycle. This copy is what x/fees/keeper and app/addressing's own
// ante_policy.go use.
func WalkMessages(msgs []sdk.Msg, visit func(sdk.Msg) error) error {
	for _, msg := range msgs {
		if err := visit(msg); err != nil {
			return err
		}

		nested, ok := msg.(NestedMsgProvider)
		if !ok {
			continue
		}

		children, err := nested.GetMessages()
		if err != nil {
			return err
		}
		if err := WalkMessages(children, visit); err != nil {
			return err
		}
	}

	return nil
}

// CountMessages returns the total number of messages in msgs, counting the
// wrapper message and every message nested inside it (e.g. authz.MsgExec)
// -- the actual, effective set of messages a tx will execute, which is what
// message-count caps and per-message fees are meant to bound (FINDING-013).
func CountMessages(msgs []sdk.Msg) (uint64, error) {
	var count uint64
	if err := WalkMessages(msgs, func(sdk.Msg) error {
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

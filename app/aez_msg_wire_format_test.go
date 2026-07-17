package app_test

import (
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/sovereign-l1/l1/app"
	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// TestAEZMsgUpdateRoutingTableIsBroadcastable is the wire-format guard for AEZ
// Phase 2's one Msg, and it exists because this tree has been bitten twice by
// exactly this bug in exactly this place.
//
// x/contracts' hand-rolled Msgs lacked protobuf struct tags and field
// descriptors: gogoproto.Marshal silently produced EMPTY BYTES and Unmarshal
// panicked (see app/avm_msg_wire_format_test.go). x/nominator-pool's had the
// same defect and it was found LIVE -- broadcasting one crashed every receiving
// node. Both were invisible to keeper tests, which call the handler directly in
// Go and never touch the wire.
//
// So this test does what a keeper test structurally cannot: it marshals the
// message the way a real transaction would and resolves its signer through the
// app's own codec -- which routes through signing.Options.CustomGetSigners in
// app/keeperconfig/tx.go. If the descriptor loses its fields, its
// cosmos.msg.v1.service option, or its CustomGetSigners registration, this fails
// here instead of on a validator.
func TestAEZMsgUpdateRoutingTableIsBroadcastable(t *testing.T) {
	testApp := app.Setup(t, false)
	appCodec := testApp.AppCodec()

	authority := aeztypes.GovAuthority()
	buckets := make([]uint32, aeztypes.BucketCount)
	// A non-zero bucket so the repeated field carries real content: declared
	// LABEL_OPTIONAL instead of LABEL_REPEATED the vector would decode to a
	// single scalar and silently drop 255 assignments.
	buckets[42] = 3

	msg := &aeztypes.MsgUpdateRoutingTable{
		Authority:        authority,
		Version:          7,
		Epoch:            2,
		ActivationHeight: 20000,
		Buckets:          buckets,
	}

	bz, err := gogoproto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if len(bz) == 0 {
		t.Fatalf("marshaled to empty bytes; struct tags are missing again")
	}

	var decoded aeztypes.MsgUpdateRoutingTable
	if err := gogoproto.Unmarshal(bz, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Authority != authority {
		t.Fatalf("authority round-trip: got %q want %q", decoded.Authority, authority)
	}
	if decoded.Version != 7 || decoded.Epoch != 2 || decoded.ActivationHeight != 20000 {
		t.Fatalf("scalar round-trip failed: %+v", decoded)
	}
	// The whole 256-entry vector must survive, not just its first element.
	if len(decoded.Buckets) != aeztypes.BucketCount {
		t.Fatalf("bucket vector round-trip: got %d entries want %d", len(decoded.Buckets), aeztypes.BucketCount)
	}
	if decoded.Buckets[42] != 3 {
		t.Fatalf("bucket 42 round-trip: got %d want 3", decoded.Buckets[42])
	}

	signers, _, err := appCodec.GetMsgV1Signers(msg)
	if err != nil {
		t.Fatalf("GetSigners error: %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("expected exactly 1 signer, got %d", len(signers))
	}
	gotAddr, err := aetraaddress.Codec{}.BytesToString(signers[0])
	if err != nil {
		t.Fatalf("decode signer bytes: %v", err)
	}
	if gotAddr != authority {
		t.Fatalf("signer mismatch: got %s want %s", gotAddr, authority)
	}
}

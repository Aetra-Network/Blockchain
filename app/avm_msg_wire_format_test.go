package app_test

import (
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/sovereign-l1/l1/app"
	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
)

func avmProbeAddress(t *testing.T, seed byte) string {
	t.Helper()
	raw := make([]byte, 20)
	for i := range raw {
		raw[i] = seed
	}
	addr, err := aetraaddress.FormatUserFriendly(raw)
	if err != nil {
		t.Fatalf("format address: %v", err)
	}
	return addr
}

// TestAVMContractMsgsAreBroadcastable guards against a regression where
// x/contracts' hand-rolled Msg types (MsgStoreCode/MsgDeployContract/
// MsgExecuteExternal) lacked protobuf struct tags and field descriptors:
// gogoproto.Marshal silently produced empty bytes and Unmarshal panicked, so
// no real signed transaction carrying one of these messages could ever be
// built or broadcast even though every existing x/contracts keeper test
// exercised the keeper directly in Go and never caught it.
func TestAVMContractMsgsAreBroadcastable(t *testing.T) {
	testApp := app.Setup(t, false)
	appCodec := testApp.AppCodec()

	authority := avmProbeAddress(t, 0xAA)
	creator := avmProbeAddress(t, 0xBB)
	sender := avmProbeAddress(t, 0xCC)

	cases := []struct {
		name string
		msg  gogoproto.Message
		want string
	}{
		{"MsgStoreCode", &contractstypes.MsgStoreCode{Authority: authority, CodeHash: "hash123", CodeBytes: 3, Bytecode: []byte{1, 2, 3}}, authority},
		{"MsgDeployContract", &contractstypes.MsgDeployContract{Creator: creator, CodeID: "hash123", Height: 5}, creator},
		{"MsgExecuteExternal", &contractstypes.MsgExecuteExternal{Sender: sender, ContractAddress: "AEsomething", GasLimit: 1000, Height: 5}, sender},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bz, err := gogoproto.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			if len(bz) == 0 {
				t.Fatalf("marshaled to empty bytes; struct tags are missing again")
			}

			signers, _, err := appCodec.GetMsgV1Signers(tc.msg)
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
			if gotAddr != tc.want {
				t.Fatalf("signer mismatch: got %s want %s", gotAddr, tc.want)
			}
		})
	}
}

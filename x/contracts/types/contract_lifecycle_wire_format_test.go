package types

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
)

// TestContractLifecycleMsgsAreBroadcastable guards the same regression class as
// app/avm_msg_wire_format_test.go's TestAVMContractMsgsAreBroadcastable:
// UpgradeContractCode, MigrateContractState, SetContractAdmin, and
// DisableContractUpgrades were fully implemented, unit-tested keeper methods
// (x/contracts/keeper/keeper.go) whose Msg types carried no protobuf struct
// tags and no Descriptor(). gogoproto.Marshal would have silently produced
// empty bytes and the SDK v0.54.3 tx decoder would reject any real signed
// transaction carrying one of these messages at decode time, even though a
// direct Go call to the keeper method (as every prior test used) worked fine.
// This builds a bare interface registry + ProtoCodec (mirroring app.go's own
// construction) rather than a full app.Setup, so it stays a x/contracts-only
// test.
func TestContractLifecycleMsgsAreBroadcastable(t *testing.T) {
	registry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: gogoproto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          addressing.Codec{},
			ValidatorAddressCodec: addressing.Codec{},
		},
	})
	require.NoError(t, err)
	RegisterInterfaces(registry)
	appCodec := codec.NewProtoCodec(registry)

	actor := contractAPIAddress(0xDD)
	contractAddr := contractAPIAddress(0xEE)

	cases := []struct {
		name string
		msg  gogoproto.Message
	}{
		{"MsgUpgradeContractCode", &MsgUpgradeContractCode{Actor: actor, ContractAddress: contractAddr, NewCodeID: "code2", MigrationHandler: "schema_only", Height: 5}},
		{"MsgMigrateContractState", &MsgMigrateContractState{Actor: actor, ContractAddress: contractAddr, FromSchemaVersion: 1, ToSchemaVersion: 2, MigrationHandler: "append", Payload: []byte(":v2"), Height: 5}},
		{"MsgSetContractAdmin", &MsgSetContractAdmin{Actor: actor, ContractAddress: contractAddr, NewAdmin: actor, Height: 5}},
		{"MsgDisableContractUpgrades", &MsgDisableContractUpgrades{Actor: actor, ContractAddress: contractAddr, Height: 5}},
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
			gotAddr, err := addressing.Codec{}.BytesToString(signers[0])
			if err != nil {
				t.Fatalf("decode signer bytes: %v", err)
			}
			if gotAddr != actor {
				t.Fatalf("signer mismatch: got %s want %s", gotAddr, actor)
			}
		})
	}
}

package types

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	proto2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

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
		// MsgScheduleContractUpgrade / MsgApplyScheduledUpgrade: the same
		// wire-format regression class guarded above, for the timelocked
		// two-step upgrade flow (see the doc comment on
		// MsgScheduleContractUpgrade in contract_state.go).
		{"MsgScheduleContractUpgrade", &MsgScheduleContractUpgrade{Actor: actor, ContractAddress: contractAddr, NewCodeID: "code2", MigrationHandler: "schema_only", Height: 5}},
		{"MsgApplyScheduledUpgrade", &MsgApplyScheduledUpgrade{Actor: actor, ContractAddress: contractAddr, Height: 5}},
		// MsgDeleteExpiredContract: contracts-storage-rent-cycle's archive/
		// delete path. Signs with "authority" (not "actor", unlike the
		// upgrade-flow messages above) -- reusing the same address value for
		// both roles here just exercises the signer-decode path generically.
		{"MsgDeleteExpiredContract", &MsgDeleteExpiredContract{Authority: actor, ContractAddress: contractAddr, Height: 5}},
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

// TestContractLifecycleMsgsWireRoundTrip is the SA2-N01 regression guard
// proper (see x/native-account/types/zone_note_wire_test.go's identical
// pattern, this plan's template): it proves the actual wire path -- marshal
// via gogoproto (what a real broadcast does), unmarshal via gogoproto (what a
// real receiving node does) -- round-trips every field, not just that
// Marshal produces non-empty bytes and a signer can be extracted (which
// TestContractLifecycleMsgsAreBroadcastable above already covers).
// MsgDeleteExpiredContract is new; MsgTopUpContract, MsgPayContractStorageDebt,
// and MsgUnfreezeContract are backfilled here because the research found they
// were previously exercised only via direct keeper calls in tests, never
// through a raw wire marshal/unmarshal round trip.
func TestContractLifecycleMsgsWireRoundTrip(t *testing.T) {
	sender := contractAPIAddress(0x21)
	contractAddr := contractAPIAddress(0x22)

	t.Run("MsgDeleteExpiredContract", func(t *testing.T) {
		msg := &MsgDeleteExpiredContract{Authority: sender, ContractAddress: contractAddr, Height: 12345}
		bz, err := gogoproto.Marshal(msg)
		require.NoError(t, err)
		require.NotEmpty(t, bz)

		decoded := &MsgDeleteExpiredContract{}
		require.NoError(t, gogoproto.Unmarshal(bz, decoded))
		require.Equal(t, msg.Authority, decoded.Authority)
		require.Equal(t, msg.ContractAddress, decoded.ContractAddress)
		require.Equal(t, msg.Height, decoded.Height)
	})

	t.Run("MsgTopUpContract", func(t *testing.T) {
		msg := &MsgTopUpContract{Sender: sender, ContractAddress: contractAddr, Amount: 777, Height: 12345}
		bz, err := gogoproto.Marshal(msg)
		require.NoError(t, err)
		require.NotEmpty(t, bz)

		decoded := &MsgTopUpContract{}
		require.NoError(t, gogoproto.Unmarshal(bz, decoded))
		require.Equal(t, msg.Sender, decoded.Sender)
		require.Equal(t, msg.ContractAddress, decoded.ContractAddress)
		require.Equal(t, msg.Amount, decoded.Amount)
		require.Equal(t, msg.Height, decoded.Height)
	})

	t.Run("MsgPayContractStorageDebt", func(t *testing.T) {
		msg := &MsgPayContractStorageDebt{Sender: sender, ContractAddress: contractAddr, Amount: 888, Height: 12345}
		bz, err := gogoproto.Marshal(msg)
		require.NoError(t, err)
		require.NotEmpty(t, bz)

		decoded := &MsgPayContractStorageDebt{}
		require.NoError(t, gogoproto.Unmarshal(bz, decoded))
		require.Equal(t, msg.Sender, decoded.Sender)
		require.Equal(t, msg.ContractAddress, decoded.ContractAddress)
		require.Equal(t, msg.Amount, decoded.Amount)
		require.Equal(t, msg.Height, decoded.Height)
	})

	t.Run("MsgUnfreezeContract", func(t *testing.T) {
		msg := &MsgUnfreezeContract{Sender: sender, ContractAddress: contractAddr, Height: 12345}
		bz, err := gogoproto.Marshal(msg)
		require.NoError(t, err)
		require.NotEmpty(t, bz)

		decoded := &MsgUnfreezeContract{}
		require.NoError(t, gogoproto.Unmarshal(bz, decoded))
		require.Equal(t, msg.Sender, decoded.Sender)
		require.Equal(t, msg.ContractAddress, decoded.ContractAddress)
		require.Equal(t, msg.Height, decoded.Height)
	})
}

// TestMsgDeleteExpiredContractDescriptorIndexMatchesMessageType is the
// specific SA2-N01 regression guard: Descriptor()'s hardcoded index must
// point at the message with the matching name inside
// fileDescriptorContractsTx's MessageType slice, decoded independently
// rather than trusted by inspection alone (mirrors
// TestMsgSendZoneNoteDescriptorIndexMatchesMessageType in
// x/native-account/types/zone_note_wire_test.go).
func TestMsgDeleteExpiredContractDescriptorIndexMatchesMessageType(t *testing.T) {
	fd := decodeFileDescriptorContractsTxForTest(t)

	_, msgPath := (&MsgDeleteExpiredContract{}).Descriptor()
	require.Len(t, msgPath, 1)
	require.Equal(t, "MsgDeleteExpiredContract", fd.MessageType[msgPath[0]].GetName())
}

func decodeFileDescriptorContractsTxForTest(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(fileDescriptorContractsTx))
	require.NoError(t, err)
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)
	fd := &descriptorpb.FileDescriptorProto{}
	require.NoError(t, proto2.Unmarshal(raw, fd))
	return fd
}

package contracts

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/sovereign-l1/l1/x/contracts/keeper"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestRealGRPCCodecServesQueryCode is the exact reproduction of the
// originally reported bug: a real out-of-process gRPC client -- mirroring
// what `l1d query avm code <id>` does, which builds its client.Context's
// GRPCClient via a plain grpc.NewClient(grpcURI, dialOpts...) with no custom
// codec option (see cosmos-sdk client/cmd.go, the FlagGRPC branch of
// readQueryCommandFlags) -- calling against a server wired with cosmos-sdk's
// REAL production gRPC codec (codec.ProtoCodec.GRPCCodec(), forced via
// grpc.ForceServerCodec exactly as cosmos-sdk's server/grpc/server.go
// does).
//
// Before query_marshal.go/query_marshal_size.go/query_marshal_unmarshal.go
// added real Marshal()/Unmarshal()/Size() methods to these hand-written
// types, this exact call failed with "rpc error: code = Internal desc =
// grpc: error while marshaling: proto: string does not implement Marshal".
// The failure is not specific to the server's codec: grpc-go's own default
// client codec (google.golang.org/grpc/encoding/proto) wraps any
// Reset/String/ProtoMessage-only type through the identical
// protobuf-go-v2 legacy-compatibility shim (see
// google.golang.org/protobuf/internal/impl/legacy_message.go,
// legacyLoadMessageInfo: it only takes the fast, correct path through a
// type's own Marshal()/Unmarshal() methods when they exist -- interface
// legacyMarshaler/legacyUnmarshaler -- and otherwise falls back to a
// generic reflection-based field walk driven by the raw descriptor bytes,
// which is what breaks on this package's hand-rolled struct layout). That
// is why this test deliberately does NOT force a client-side codec either:
// it must pass using the exact same client wiring the CLI uses.
func TestRealGRPCCodecServesQueryCode(t *testing.T) {
	wallet := gatewayTestAddress("33")
	k := keeper.NewKeeperWithAccountStatus(passthroughAccountStatus{})
	bytecode := minimalValidAVMBytecode("real-grpc-codec-fixture")
	stored, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode})
	require.NoError(t, err)

	// Real production codec: codec.ProtoCodec.GRPCCodec(), forced via
	// grpc.ForceServerCodec exactly like cosmos-sdk's server/grpc/server.go
	// does. A bare grpc.NewServer() would not pin down the exact production
	// wiring this test exists to reproduce.
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	grpcServer := grpc.NewServer(grpc.ForceServerCodec(protoCodec.GRPCCodec()))
	types.RegisterQueryServer(grpcServer, keeper.NewGRPCQueryServer(&k))

	lis := bufconn.Listen(1024 * 1024)
	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(lis) }()
	defer func() {
		grpcServer.Stop()
		<-serveErr
	}()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	// Deliberately no grpc.ForceCodec/WithDefaultCallOptions here: the real
	// CLI dials with only transport credentials (see client/cmd.go), so this
	// exercises grpc-go's stock default client codec end to end.
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := types.NewQueryClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.Code(ctx, &types.QueryCodeRequest{CodeID: stored.CodeID})
	require.NoError(t, err, "real out-of-process gRPC call against the production codec must succeed")

	require.True(t, resp.Found)
	require.Equal(t, stored.CodeID, resp.Code.CodeID)
	require.Equal(t, types.CanonicalCodeHash(bytecode), resp.Code.CodeHash)
	require.Equal(t, bytecode, resp.Code.Bytecode)
	require.Equal(t, wallet, resp.Code.Owner)

	// A second RPC (Params, not Code) confirms the fix isn't a one-message
	// fluke -- Params is a completely separate message with its own
	// Marshal/Unmarshal pair.
	paramsResp, err := client.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, k.Params(), paramsResp.Params)
}

// TestRealGRPCCodecServesQueryContract exercises the same real production
// codec against Contract -- the largest, most field-dense type in the
// closure (24 fields, with fields 16-24 needing 2-byte protobuf tags) -- and
// a request carrying a *StateInit pointer field, to confirm the fix
// generalizes across the whole closure rather than just the simplest
// message.
func TestRealGRPCCodecServesQueryContract(t *testing.T) {
	wallet := gatewayTestAddress("44")
	k := keeper.NewKeeperWithAccountStatus(passthroughAccountStatus{})
	bytecode := minimalValidAVMBytecode("real-grpc-codec-contract-fixture")
	storedCode, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode})
	require.NoError(t, err)

	deployResp, err := k.DeployContractState(context.Background(), types.MsgDeployContract{
		Creator:        wallet,
		CodeID:         storedCode.CodeID,
		InitPayload:    []byte("init"),
		InitialBalance: 0,
		Admin:          wallet,
		Height:         1,
	})
	require.NoError(t, err)

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	grpcServer := grpc.NewServer(grpc.ForceServerCodec(protoCodec.GRPCCodec()))
	types.RegisterQueryServer(grpcServer, keeper.NewGRPCQueryServer(&k))

	lis := bufconn.Listen(1024 * 1024)
	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(lis) }()
	defer func() {
		grpcServer.Stop()
		<-serveErr
	}()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := types.NewQueryClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.Contract(ctx, &types.QueryContractRequest{ContractAddress: deployResp.ContractAddressUser})
	require.NoError(t, err, "real out-of-process gRPC Contract query against the production codec must succeed")
	require.True(t, resp.Found)
	require.Equal(t, deployResp.ContractAddressUser, resp.Contract.AddressUser)
	require.Equal(t, storedCode.CodeID, resp.Contract.CodeID)
	require.Equal(t, wallet, resp.Contract.Owner)
	require.Equal(t, wallet, resp.Contract.Admin)
}

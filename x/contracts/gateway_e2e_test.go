package contracts

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/keeper"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// TestAppModuleRegisterGRPCGatewayRoutesServesRealQueries proves the REST
// gateway wired in RegisterGRPCGatewayRoutes actually reaches the live
// keeper: real REST mux + a real HTTP round trip against real keeper state.
// Before this fix, RegisterGRPCGatewayRoutes was an empty function body, so
// any REST call to /l1/contracts/v1/... returned 404/Unimplemented
// regardless of the keeper's actual state -- see
// RESULTS_V1-live-testnet-exercise.md section 3.
func TestAppModuleRegisterGRPCGatewayRoutesServesRealQueries(t *testing.T) {
	wallet := gatewayTestAddress("11")
	k := keeper.NewKeeperWithAccountStatus(passthroughAccountStatus{})
	bytecode := minimalValidAVMBytecode("gateway-e2e-fixture")
	stored, err := k.StoreCode(types.MsgStoreCode{Authority: wallet, Bytecode: bytecode})
	require.NoError(t, err)

	mux := runtime.NewServeMux()
	NewAppModule(&k).RegisterGRPCGatewayRoutes(client.Context{}, mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/l1/contracts/v1/code/" + stored.CodeID)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode, "gateway response: %s", string(body))
	require.NotContains(t, string(body), "Unimplemented")
	require.NotContains(t, string(body), "not implemented")
	require.Contains(t, string(body), stored.CodeID)
	require.Contains(t, string(body), types.CanonicalCodeHash(bytecode))

	// A second REST route (list, not single-resource) to confirm this isn't
	// a one-route fluke.
	listResp, err := http.Get(server.URL + "/l1/contracts/v1/codes?pagination.limit=10")
	require.NoError(t, err)
	defer listResp.Body.Close()
	listBody, err := io.ReadAll(listResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listResp.StatusCode, "gateway response: %s", string(listBody))
	require.Contains(t, string(listBody), stored.CodeID)
}

type passthroughAccountStatus struct{}

func (passthroughAccountStatus) AccountStatus(context.Context, string) (string, bool, error) {
	return "active", true, nil
}

func gatewayTestAddress(hexByte string) string {
	bz, err := hex.DecodeString(strings.Repeat(hexByte, 20))
	if err != nil {
		panic(err)
	}
	return addressing.FormatAccAddress(sdk.AccAddress(bz))
}

// minimalValidAVMBytecode builds the smallest possible AVM module that
// decodes AND passes avm.Verifier.Verify (a single "return" instruction
// exporting EntryDeploy, importing the one host function OpReturn requires).
// See FINDING-004: StoreCode now actually calls avm.DecodeModule + Verify
// instead of only checking the AVM1 header/size, so fixtures that used to be
// arbitrary "AVM1 <placeholder text>" ASCII strings must now be real,
// decodable modules. seed only varies the MetadataHash so distinct callers
// get distinct CanonicalCodeHash values; it carries no other meaning.
func minimalValidAVMBytecode(seed string) []byte {
	var metadata [32]byte
	copy(metadata[:], seed)
	module := avm.Module{
		Version:      avm.Version,
		Imports:      []avm.HostFunction{avm.HostReturn},
		Exports:      map[avm.Entrypoint]uint32{avm.EntryDeploy: 0},
		MetadataHash: metadata,
		Code:         []avm.Instruction{{Op: avm.OpReturn, Arg: 0}},
	}
	bz, err := avm.EncodeModule(module)
	if err != nil {
		panic(err)
	}
	return bz
}

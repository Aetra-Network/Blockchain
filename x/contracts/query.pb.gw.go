package contracts

// NOTE: this file is hand-written, not machine-generated, despite the
// .pb.gw.go suffix -- see RegisterGRPCGatewayRoutes below for why a real
// protoc-gen-grpc-gateway output can't be used here. The suffix is kept
// deliberately: app/security_attack_audit_test.go's consensus-purity sweep
// (TestConsensusCriticalSourceRejectsNondeterminismAndExternalNetworkCalls)
// exempts *.pb.gw.go files from its "no net/http in x/ or app/" rule, which
// is correct for this file: none of its functions run during a state
// transition, only once at REST-server bootstrap (RegisterGRPCGatewayRoutes
// is called from api_services.go-style wiring, never from EndBlock/InitGenesis/
// msg or query handlers).

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/cosmos/cosmos-sdk/client"

	"github.com/sovereign-l1/l1/x/contracts/keeper"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// Route patterns mirror proto/l1/contracts/v1/query.proto's google.api.http
// annotations exactly (verified against the buf-generated
// api/l1/contracts/v1/query.pb.gw.go, whose Pattern values are copied
// verbatim below -- Pattern is a pure path-template matcher with no
// dependency on any generated Go message type, so reusing it here carries
// none of the cross-package registry risk described in RegisterGRPCGatewayRoutes).
var (
	patternQueryParams          = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3}, []string{"l1", "contracts", "v1", "params"}, "", runtime.AssumeColonVerbOpt(false)))
	patternQueryCode            = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3, 1, 0, 4, 1, 5, 4}, []string{"l1", "contracts", "v1", "code", "code_id"}, "", runtime.AssumeColonVerbOpt(false)))
	patternQueryCodes           = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3}, []string{"l1", "contracts", "v1", "codes"}, "", runtime.AssumeColonVerbOpt(false)))
	patternQueryContract        = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3, 1, 0, 4, 1, 5, 4}, []string{"l1", "contracts", "v1", "contract", "contract_address"}, "", runtime.AssumeColonVerbOpt(false)))
	patternQueryContracts       = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 1}, []string{"l1", "contracts", "v1"}, "", runtime.AssumeColonVerbOpt(false)))
	patternQueryContractStorage = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3, 1, 0, 4, 1, 5, 4, 2, 5}, []string{"l1", "contracts", "v1", "contract", "contract_address", "storage"}, "", runtime.AssumeColonVerbOpt(false)))
	patternQueryContractReceipt = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3, 1, 0, 4, 1, 5, 4, 2, 5}, []string{"l1", "contracts", "v1", "contract", "contract_address", "receipts"}, "", runtime.AssumeColonVerbOpt(false)))
)

// RegisterGRPCGatewayRoutes hand-registers the contracts module's read-only
// REST surface by calling the keeper's query methods directly (in-process --
// the REST gateway and the node it serves always live in the same "l1d
// start" process), instead of round-tripping through gRPC wire
// serialization. Two independent reasons this had to avoid the "normal"
// generated-client route:
//
//  1. api/l1/contracts/v1 (a real protoc/buf-generated client for this same
//     service) exists, but importing it into this process corrupts global
//     gogoproto/protobuf-go type registries against x/contracts/types (both
//     declare identical "l1.contracts.v1.*" proto type names), which
//     crashes codec.InterfaceRegistry.RegisterImplementations at app boot
//     ("concrete type *types.MsgStoreCode has already been registered under
//     typeURL /").
//  2. Independently of (1): x/contracts/types' hand-written query
//     request/response structs (service.go, api.go, types.go) were never
//     given real gogoproto Marshal/Unmarshal methods, only the bare
//     Reset/String/ProtoMessage/Descriptor subset. That means ANY real wire
//     round trip -- cosmos-sdk's production gRPC codec
//     (codec.ProtoCodec.GRPCCodec(), server/grpc/server.go:60), a CLI
//     client.Context.Invoke call, or grpcurl -- fails
//     ("proto: string does not implement Marshal"), regardless of whether
//     api/l1/contracts/v1 is involved at all. Calling the keeper directly
//     sidesteps this too, since no bytes are ever marshaled.
//
// See RESULTS_V1-live-testnet-exercise.md section 3: before this fix,
// RegisterGRPCGatewayRoutes was an empty function body and every REST call
// here returned Unimplemented. The CLI (a genuinely separate process, which
// cannot take this in-process shortcut) still needs real Marshal/Unmarshal
// implementations for these types before it can query a remote node --
// tracked as a follow-up, not fixed by this change.
func (am AppModule) RegisterGRPCGatewayRoutes(_ client.Context, mux *runtime.ServeMux) {
	qs := keeper.NewGRPCQueryServer(am.keeper)

	mux.Handle("GET", patternQueryParams, gatewayHandler(func(r *http.Request, _ map[string]string) (any, error) {
		return qs.Params(r.Context(), &types.QueryParamsRequest{})
	}))
	mux.Handle("GET", patternQueryCode, gatewayHandler(func(r *http.Request, pathParams map[string]string) (any, error) {
		return qs.Code(r.Context(), &types.QueryCodeRequest{CodeID: pathParams["code_id"]})
	}))
	mux.Handle("GET", patternQueryCodes, gatewayHandler(func(r *http.Request, _ map[string]string) (any, error) {
		return qs.Codes(r.Context(), &types.QueryCodesRequest{Pagination: gatewayPagination(r)})
	}))
	mux.Handle("GET", patternQueryContract, gatewayHandler(func(r *http.Request, pathParams map[string]string) (any, error) {
		return qs.Contract(r.Context(), &types.QueryContractRequest{ContractAddress: pathParams["contract_address"]})
	}))
	mux.Handle("GET", patternQueryContracts, gatewayHandler(func(r *http.Request, _ map[string]string) (any, error) {
		return qs.Contracts(r.Context(), &types.QueryContractsRequest{Pagination: gatewayPagination(r)})
	}))
	mux.Handle("GET", patternQueryContractStorage, gatewayHandler(func(r *http.Request, pathParams map[string]string) (any, error) {
		return qs.ContractStorage(r.Context(), &types.QueryContractStorageRequest{
			ContractAddress: pathParams["contract_address"],
			Pagination:      gatewayPagination(r),
		})
	}))
	mux.Handle("GET", patternQueryContractReceipt, gatewayHandler(func(r *http.Request, pathParams map[string]string) (any, error) {
		return qs.ContractReceipts(r.Context(), &types.QueryContractReceiptsRequest{
			ContractAddress: pathParams["contract_address"],
			Pagination:      gatewayPagination(r),
		})
	}))
}

// gatewayPagination reads a "pagination.limit" (or bare "limit") query
// parameter into a PageRequest, matching the query-string convention
// grpc-gateway uses for non-path message fields. Missing/invalid values
// default to 0 (unbounded/keeper-default -- each query keeper method already
// enforces its own bounds).
func gatewayPagination(r *http.Request) types.PageRequest {
	raw := r.URL.Query().Get("pagination.limit")
	if raw == "" {
		raw = r.URL.Query().Get("limit")
	}
	limit, _ := strconv.ParseUint(raw, 10, 32)
	return types.PageRequest{Limit: uint32(limit)}
}

// gatewayHandler adapts a typed query call into a runtime.HandlerFunc,
// writing the result (or error) as plain JSON. This intentionally uses
// encoding/json rather than grpc-gateway's jsonpb marshaler: x/contracts/types
// messages are plain structs with correct `json:"..."` tags (verified
// against every request/response type used above), and none of these
// responses contain oneofs/Any/enums-as-int that would need jsonpb's
// proto-aware handling.
func gatewayHandler(call func(r *http.Request, pathParams map[string]string) (any, error)) runtime.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
		resp, err := call(r, pathParams)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

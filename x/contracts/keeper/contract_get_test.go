package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// counterGetSource is a minimal contract with getters exercising the full
// argument story: a no-arg currentCounter, a one-arg plusCounter(by), and a
// multi-arg, multi-typed clamp(lo, hi, useHi) proving a getter is no longer
// limited to a single numeric argument.
const counterGetSource = `
@storage
struct Storage {
    counter: uint64
}

@message(0x7001)
struct Bump {}

type InternalMsg = Bump
type ExternalMsg = Bump

contract CounterBox {
    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func Storage.load() {
        return Storage.fromChunk(contract.getData())
    }

    @store
    func Storage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        var st = lazy Storage.load()
        st.counter += 1
        st.save()
    }

    @external
    func onExternalMessage(inMsg: Segment) {
    }

    @get
    func currentCounter(): uint64 {
        const st = lazy Storage.load()
        return st.counter
    }

    @get
    func plusCounter(by: uint64): uint64 {
        const st = lazy Storage.load()
        return st.counter + by
    }

    @get
    func clamp(lo: uint64, hi: uint64, useHi: bool): uint64 {
        const st = lazy Storage.load()
        if (useHi) {
            return hi
        }
        return (st.counter < lo) ? lo : st.counter
    }
}
`

// TestContractGetByExactName pins the getter-by-name rule: the EXACT
// source-level function name invokes the getter (via the compiler-emitted
// name-alias selector); any other spelling — including a snake_case variant —
// fails closed.
func TestContractGetByExactName(t *testing.T) {
	owner := aeAddress("21")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	c := mustFamilyCompiler(t)
	res, err := c.Compile([]byte(counterGetSource))
	require.NoError(t, err)
	codeID := storeCompiledCode(t, &k, owner, res)

	initData, err := res.StorageCodec.Encode(map[string]any{"counter": uint64(41)})
	require.NoError(t, err)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: codeID, InitMsg: initData, Funds: 1_000_000,
		Admin: owner, Salt: "getbox", Height: 7,
	})
	require.NoError(t, err)

	// Exact name works and returns the stored value.
	got, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "currentCounter",
	})
	require.NoError(t, err)
	require.True(t, got.Success, "exact getter name must dispatch: %s", got.Error)
	require.Equal(t, "41", got.Result)
	require.NotZero(t, got.GasUsed)

	// One argument, explicit type.
	plus, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "plusCounter",
		Args:            []types.GetMethodArg{{Type: "uint64", Value: "9"}},
	})
	require.NoError(t, err)
	require.True(t, plus.Success, "one-arg getter must dispatch: %s", plus.Error)
	require.Equal(t, "50", plus.Result)

	// Same call using the "number" umbrella type (no width to pick).
	plusNumber, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "plusCounter",
		Args:            []types.GetMethodArg{{Type: "number", Value: "9"}},
	})
	require.NoError(t, err)
	require.True(t, plusNumber.Success, "number-typed arg must dispatch: %s", plusNumber.Error)
	require.Equal(t, "50", plusNumber.Result)

	// Three arguments, three different types: the previous one-argument
	// ceiling was a compiler limitation, not a real AVM constraint.
	clampLo, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "clamp",
		Args: []types.GetMethodArg{
			{Type: "uint64", Value: "1000"},
			{Type: "uint64", Value: "9999"},
			{Type: "bool", Value: "false"},
		},
	})
	require.NoError(t, err)
	require.True(t, clampLo.Success, "3-arg getter must dispatch: %s", clampLo.Error)
	require.Equal(t, "1000", clampLo.Result, "counter(41) < lo(1000), so lo wins")

	clampHi, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "clamp",
		Args: []types.GetMethodArg{
			{Type: "uint64", Value: "1000"},
			{Type: "uint64", Value: "9999"},
			{Type: "bool", Value: "true"},
		},
	})
	require.NoError(t, err)
	require.True(t, clampHi.Success)
	require.Equal(t, "9999", clampHi.Result, "useHi=true picks hi regardless of lo/counter")

	// A different spelling is a different method: snake_case fails closed.
	wrong, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "current_counter",
	})
	require.NoError(t, err)
	require.False(t, wrong.Success, "snake_case spelling must not dispatch a camelCase getter")

	// Unknown method fails closed too.
	missing, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "noSuchGetter",
	})
	require.NoError(t, err)
	require.False(t, missing.Success)

	// An unreasonable number of arguments is still rejected up front.
	tooMany := make([]types.GetMethodArg, types.MaxGetMethodArgs+1)
	for i := range tooMany {
		tooMany[i] = types.GetMethodArg{Type: "uint64", Value: "1"}
	}
	_, err = k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: created.ContractAddressUser,
		Method:          "plusCounter",
		Args:            tooMany,
	})
	require.ErrorContains(t, err, "exceeds limit")
}

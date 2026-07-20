package keeper

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// This file is the reference-contract proof for the read-only
// cross-contract call mechanism (design doc §6, §6.8): unlike
// external_get_test.go (inline hand-written source, exercising the keeper
// wiring in isolation) and x/aetravm/avm/external_get_test.go /
// x/aetravm/compiler/external_get_test.go (VM-level and single-package
// compiler-level opcode tests), this file compiles the REAL reference
// contracts under examples/avm/oracle/ from disk, deploys BOTH of them
// through the real x/contracts keeper (StoreCode -> InstantiateContract,
// the actual on-chain contract registry), and drives them purely through
// the public keeper RPCs (ContractGet, ExecuteContract) -- proving the
// cross-contract read genuinely works end-to-end through deployed, on-chain
// contracts, not just synthetic VM-level opcode tests.

func compileOracleExampleFile(t *testing.T, rel string) *compiler.Result {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	path := filepath.Clean(filepath.Join("..", "..", "..", "examples", "avm", "oracle", rel))
	res, err := c.CompileFile(path)
	require.NoError(t, err)
	return res
}

// deployPriceOracle compiles and deploys examples/avm/oracle/PriceOracle.atlx
// with the given initial price, returning its user-facing address.
func deployPriceOracle(t *testing.T, k *Keeper, owner string, initialPrice uint64, salt string, height uint64) string {
	t.Helper()
	res := compileOracleExampleFile(t, "PriceOracle.atlx")
	codeID := storeCompiledCode(t, k, owner, res)
	initData, err := res.StorageCodec.Encode(map[string]any{"price": initialPrice})
	require.NoError(t, err)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: codeID, InitMsg: initData, Funds: 1_000_000,
		Admin: owner, Salt: salt, Height: height,
	})
	require.NoError(t, err)
	return created.ContractAddressUser
}

// deployOracleConsumer compiles and deploys
// examples/avm/oracle/OracleConsumer.atlx pointed at the given oracle
// address, returning its user-facing address.
func deployOracleConsumer(t *testing.T, k *Keeper, owner, oracle, salt string, height uint64) string {
	t.Helper()
	res := compileOracleExampleFile(t, "OracleConsumer.atlx")
	codeID := storeCompiledCode(t, k, owner, res)
	initData, err := res.StorageCodec.Encode(map[string]any{"oracle": oracle, "lastObservedPrice": uint64(0)})
	require.NoError(t, err)
	created, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: codeID, InitMsg: initData, Funds: 1_000_000,
		Admin: owner, Salt: salt, Height: height,
	})
	require.NoError(t, err)
	return created.ContractAddressUser
}

// TestExampleOracleConsumerReadsRealOraclePrice is the end-to-end reference
// proof: two INDEPENDENTLY compiled, genuinely deployed on-chain contracts
// (from separate .atlx files, no shared source), where the consumer's own
// @get function reads the oracle's CURRENT committed price across the
// contract boundary, purely through the real ContractGet query RPC -- the
// same call site production RPC traffic uses.
func TestExampleOracleConsumerReadsRealOraclePrice(t *testing.T) {
	owner := aeAddress("41")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	oracleAddr := deployPriceOracle(t, &k, owner, 314159, "oracle-a", 10)
	consumerAddr := deployOracleConsumer(t, &k, owner, oracleAddr, "consumer-a", 11)

	got, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: consumerAddr,
		Method:          "currentPrice",
	})
	require.NoError(t, err)
	require.True(t, got.Success, "currentPrice() through the real deployed consumer must succeed: %s", got.Error)
	require.Equal(t, "314159", got.Result, "consumer must return the oracle's real, currently-committed price, not a hardcoded or cached one")
	require.NotZero(t, got.GasUsed)
}

// TestExampleOracleConsumerPriceOfDistinguishesTwoRealOracles deploys TWO
// separate, independently-priced PriceOracle instances and queries the SAME
// deployed consumer contract's priceOf(target) getter against each -- proof
// that the identical compiled bytecode genuinely performs a live read of
// whichever real on-chain contract it is pointed at, rather than returning
// a value baked in at compile time or cached from the first deployment.
func TestExampleOracleConsumerPriceOfDistinguishesTwoRealOracles(t *testing.T) {
	owner := aeAddress("42")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	oracleA := deployPriceOracle(t, &k, owner, 111, "oracle-b1", 10)
	oracleB := deployPriceOracle(t, &k, owner, 222, "oracle-b2", 11)
	// consumer is deployed pointed at oracleA, but priceOf() below queries
	// it against oracleB's address instead -- exercising the parameterized
	// path, independent of the consumer's own stored default target.
	consumerAddr := deployOracleConsumer(t, &k, owner, oracleA, "consumer-b", 12)

	gotA, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: consumerAddr,
		Method:          "priceOf",
		Args:            []types.GetMethodArg{{Type: "address", Value: oracleA}},
	})
	require.NoError(t, err)
	require.True(t, gotA.Success, "priceOf(oracleA) must succeed: %s", gotA.Error)
	require.Equal(t, "111", gotA.Result)

	gotB, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: consumerAddr,
		Method:          "priceOf",
		Args:            []types.GetMethodArg{{Type: "address", Value: oracleB}},
	})
	require.NoError(t, err)
	require.True(t, gotB.Success, "priceOf(oracleB) must succeed: %s", gotB.Error)
	require.Equal(t, "222", gotB.Result, "the same deployed consumer contract must return oracleB's own distinct real price, not oracleA's")
}

// TestExampleOracleConsumerObservesLiveOraclePriceUpdate proves temporal
// liveness end-to-end: the oracle's price is pushed AFTER both contracts
// are already deployed, and the consumer's query reflects the NEW value --
// not whatever was true at deploy time. It also exercises BOTH real keeper
// call sites the design doc's resolver wiring covers: the mutating
// executeContract path (the consumer's own onExternalMessage performs an
// externalGet() read mid-execution and persists it) and the read-only
// ContractGet query path (both re-queried afterward).
func TestExampleOracleConsumerObservesLiveOraclePriceUpdate(t *testing.T) {
	owner := aeAddress("43")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	require.NoError(t, k.InitGenesis(k.ExportGenesis()))

	oracleAddr := deployPriceOracle(t, &k, owner, 500, "oracle-c", 10)
	consumerAddr := deployOracleConsumer(t, &k, owner, oracleAddr, "consumer-c", 11)

	before, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: consumerAddr,
		Method:          "currentPrice",
	})
	require.NoError(t, err)
	require.True(t, before.Success, "currentPrice() before update must succeed: %s", before.Error)
	require.Equal(t, "500", before.Result)

	// Push a real price update to the oracle through its own public
	// ExecuteContract entrypoint (SetPrice), then read it back to confirm
	// the on-chain oracle itself now holds the new value.
	setPriceRes, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	oracleRes, err := setPriceRes.CompileFile(filepath.Clean(filepath.Join("..", "..", "..", "examples", "avm", "oracle", "PriceOracle.atlx")))
	require.NoError(t, err)
	setPriceBody, err := oracleRes.MessageBodies["SetPrice"].Encode(map[string]any{"price": uint64(9001)})
	require.NoError(t, err)
	_, err = k.ExecuteContract(types.MsgExecuteContract{
		Sender:          owner,
		ContractAddress: oracleAddr,
		Msg:             setPriceBody,
		Height:          12,
	})
	require.NoError(t, err)

	oracleAfter, err := k.ContractGet(types.QueryContractGetRequest{ContractAddress: oracleAddr, Method: "price"})
	require.NoError(t, err)
	require.True(t, oracleAfter.Success)
	require.Equal(t, "9001", oracleAfter.Result, "the oracle's own price must reflect the update")

	// Now exercise the MUTATING call site: the consumer's own external
	// handler performs a fresh externalGet() read of the oracle mid-
	// execution and persists it.
	_, err = k.ExecuteContract(types.MsgExecuteContract{
		Sender:          owner,
		ContractAddress: consumerAddr,
		Msg:             []byte{},
		Height:          13,
	})
	require.NoError(t, err)

	observed, err := k.ContractGet(types.QueryContractGetRequest{ContractAddress: consumerAddr, Method: "lastObservedPrice"})
	require.NoError(t, err)
	require.True(t, observed.Success, "lastObservedPrice() must succeed: %s", observed.Error)
	require.Equal(t, "9001", observed.Result, "the mutating call site's externalGet() must have observed and persisted the oracle's NEW, live price")

	// And the read-only ContractGet query path must independently agree,
	// proving both real keeper call sites see the identical live value.
	after, err := k.ContractGet(types.QueryContractGetRequest{
		ContractAddress: consumerAddr,
		Method:          "currentPrice",
	})
	require.NoError(t, err)
	require.True(t, after.Success, "currentPrice() after update must succeed: %s", after.Error)
	require.Equal(t, "9001", after.Result, "the query path's externalGet() must reflect the oracle's real, current (post-update) price, not the stale value observed at deploy time")
}

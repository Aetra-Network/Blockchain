package conformance

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// TestMapStorageFieldDefaultsToEmptyMapOnFreshContract is the regression test
// for the "Map-typed @storage field has no type-aware zero default" gap: a
// Map<K,V> @storage field with no explicit initializer used to decode as
// TagUint64(0) (the generic scalar default) rather than an empty TagMap the
// instant storage was completely fresh/absent, so the very first map method
// call on a genesis-fresh contract (dex_amm.atlx's AddLiquidity reading
// st.lpBalances.get(provider) on its first-ever call) tripped
// "AVM type error: expected map, got uint64". See runtimeValueFromStorageHinted
// / avm.StateReadHintMap in x/aetravm/avm/avm.go and stateReadTypeHint in
// x/aetravm/compiler/compile.go for the fix.
//
// This drives the contract's REAL AddLiquidity message through the compiled
// module from a genuinely fresh avm.Storage{} (no seeding, no pre-existing
// key of any kind) -- the exact genesis-deploy shape a brand-new on-chain
// contract instance starts from.
func TestMapStorageFieldDefaultsToEmptyMapOnFreshContract(t *testing.T) {
	deployer := testAddress(0x71)
	res := compileExampleFile(t, filepath.Join("dex", "dex_amm.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	provider := testAddress(0xA1)
	body := mustCodecBody(t, res.MessageBodies["AddLiquidity"], map[string]any{
		"amountA": 100,
		"amountB": 100,
	})
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: deployer,
		GasLimit:        20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), provider...),
			Opcode:   res.MessageBodyOpcodes["AddLiquidity"],
			QueryID:  uint64(res.MessageBodyOpcodes["AddLiquidity"]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, exec.ResultCode, "AddLiquidity on a genesis-fresh contract must not trap (exec=%+v)", exec)

	// The first deposit seeds the pool 1:1, so the provider's minted LP
	// balance equals the smaller contributed leg (100), and the dictionary
	// must actually hold that entry afterward -- not just "didn't trap".
	rawLP, ok := exec.State["lpBalances"]
	require.True(t, ok, "AddLiquidity must have written the lpBalances field")
	decoded, _, err := avm.CanonicalDecode(rawLP)
	require.NoError(t, err)
	entries, err := decoded.AsMap()
	require.NoError(t, err)
	require.Len(t, entries, 1, "exactly one LP after the first deposit")
	got, err := entries[0].Value.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100), got)
}

// TestNestedMapStorageFieldDefaultsToEmptyMapOnFreshContract is the nested-map
// counterpart of TestMapStorageFieldDefaultsToEmptyMapOnFreshContract: proves
// multi_id_ledger.atlx's Map<address, Map<uint64, uint256>> outer @storage
// field also defaults correctly from a genuinely fresh avm.Storage{} (no
// seeding), driving a real Credit message end to end.
func TestNestedMapStorageFieldDefaultsToEmptyMapOnFreshContract(t *testing.T) {
	deployer := testAddress(0x02)
	res := compileExampleFile(t, filepath.Join("token", "multi_id_ledger.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	alice := testAddress(0xA1)
	body := mustCodecBody(t, res.MessageBodies["Credit"], map[string]any{
		"tokenId": uint64(7),
		"amount":  big.NewInt(1000),
	})
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: deployer,
		GasLimit:        20_000_000,
		Message: async.MessageEnvelope{
			Source:   append([]byte(nil), alice...),
			Opcode:   res.MessageBodyOpcodes["Credit"],
			QueryID:  uint64(res.MessageBodyOpcodes["Credit"]),
			Body:     body,
			GasLimit: 20_000_000,
		},
	})
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, exec.ResultCode, "Credit on a genesis-fresh contract must not trap (exec=%+v)", exec)

	rawBalances, ok := exec.State["balances"]
	require.True(t, ok, "Credit must have written the balances field")
	outerValue, _, err := avm.CanonicalDecode(rawBalances)
	require.NoError(t, err)
	outerEntries, err := outerValue.AsMap()
	require.NoError(t, err)
	require.Len(t, outerEntries, 1, "exactly one owner (alice) after the first credit")
	innerEntries, err := outerEntries[0].Value.AsMap()
	require.NoError(t, err, "the outer map's value must itself decode as a map")
	require.Len(t, innerEntries, 1)
	got, err := innerEntries[0].Value.AsBigInt()
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1000), got)
}

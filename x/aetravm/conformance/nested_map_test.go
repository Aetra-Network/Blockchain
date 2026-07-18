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

// TestNestedMapCompilesAndExecutes proves that a genuinely nested dictionary
// type -- Map<address, Map<uint64, uint256>> -- compiles and EXECUTES through
// the real compiler + AVM, not just parses. Prior read-only analysis found
// every layer involved (parser's generic type-argument grammar, the compiler's
// map-method type inference, and CanonicalEncode/CanonicalDecode + OpMapGet/
// OpMapSet in the VM) already fully generic/recursive over TagMap values with
// no Map-specific special-casing, so this was expected to already work; this
// test is the first one that actually drives it end to end instead of only
// asserting the claim by reading the implementation.
//
// It exercises, through examples/avm/token/multi_id_ledger.atlx (a per-owner,
// per-token-id balance registry):
//   - get/set/delete at the OUTER map level (per-owner sub-ledger).
//   - get/set/delete at the INNER map level (per-token-id balance), including
//     the "load the inner map, mutate a value inside it, write the whole
//     inner map back into the outer map" pattern (the mutate-and-writeback
//     idiom; see the .atlx file's header comment for why this is expressed
//     with a named local rather than literal `x.get(a).set(b, c)` chaining --
//     that syntax does not parse in ATLX today, independent of map nesting).
//   - a storage round-trip: the nested map field is written with toChunk()
//     and re-read with fromChunk() on every message (via the contract's
//     standard @store load()/save() pair), so every one of the sequential
//     runner.Run calls below is itself a toChunk/fromChunk round-trip proof.
//     The test additionally decodes the raw "balances" storage field
//     directly with avm.CanonicalDecode after several mutations and asserts
//     the decoded value is a TagMap whose entries' VALUES are themselves
//     TagMap values with the expected inner entries -- an explicit,
//     independent check that the nested Map survives the wire round trip
//     (not just that a subsequent getter happens to read back the right
//     number).
func TestNestedMapCompilesAndExecutes(t *testing.T) {
	deployer := testAddress(0x01)
	res := compileExampleFile(t, filepath.Join("token", "multi_id_ledger.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	alice := testAddress(0xA1)
	bob := testAddress(0xB2)
	aliceStr := addressing.FormatAccAddress(alice)
	bobStr := addressing.FormatAccAddress(bob)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	// send drives one internal message (Credit/Burn/Transfer) through the
	// compiled contract from the given sender and returns the committed
	// state, asserting the call did NOT trap.
	send := func(state avm.Storage, sender []byte, msgName string, fields map[string]any) avm.Storage {
		t.Helper()
		body := mustCodecBody(t, res.MessageBodies[msgName], fields)
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: deployer,
			GasLimit:        20_000_000,
			Message: async.MessageEnvelope{
				Source:   append([]byte(nil), sender...),
				Opcode:   res.MessageBodyOpcodes[msgName],
				QueryID:  uint64(res.MessageBodyOpcodes[msgName]),
				Body:     body,
				GasLimit: 20_000_000,
			},
		})
		require.NoErrorf(t, err, "send %s", msgName)
		require.Equalf(t, async.ResultOK, exec.ResultCode, "send %s result", msgName)
		return exec.State
	}

	// sendExpectTrap is like send but asserts the message TRAPS (a
	// deterministic rollback), and that storage is left untouched.
	sendExpectTrap := func(state avm.Storage, sender []byte, msgName string, fields map[string]any) {
		t.Helper()
		body := mustCodecBody(t, res.MessageBodies[msgName], fields)
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: deployer,
			GasLimit:        20_000_000,
			Message: async.MessageEnvelope{
				Source:   append([]byte(nil), sender...),
				Opcode:   res.MessageBodyOpcodes[msgName],
				QueryID:  uint64(res.MessageBodyOpcodes[msgName]),
				Body:     body,
				GasLimit: 20_000_000,
			},
		})
		require.NotEqualf(t, async.ResultOK, exec.ResultCode, "send %s should have trapped (err=%v)", msgName, err)
		require.Equal(t, state, exec.State, "a trapped message must leave storage untouched")
	}

	// addressUint64ArgCodec builds a positional getter-argument codec for an
	// (address, uint64) getter, matching the "arg0/arg1" wire convention the
	// compiler uses for scalar getter parameters.
	addressUint64ArgCodec := func(name string) compiler.Codec {
		return compiler.Codec{
			Name: name,
			Fields: []compiler.CodecField{
				{Name: "arg0", Type: compiler.TypeRef{Name: "address"}},
				{Name: "arg1", Type: compiler.TypeRef{Name: "uint64"}},
			},
		}
	}
	addressArgCodec := func(name string) compiler.Codec {
		return compiler.Codec{
			Name:   name,
			Fields: []compiler.CodecField{{Name: "arg0", Type: compiler.TypeRef{Name: "address"}}},
		}
	}

	balanceOf := func(state avm.Storage, owner string, tokenID uint64) *big.Int {
		t.Helper()
		body := mustCodecBody(t, addressUint64ArgCodec("balanceOf"), map[string]any{"arg0": owner, "arg1": tokenID})
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "balanceOf"), Body: body, GasLimit: 5_000_000},
			GasLimit: 5_000_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsBigInt()
		require.NoError(t, err)
		return v
	}
	hasToken := func(state avm.Storage, owner string, tokenID uint64) uint64 {
		t.Helper()
		body := mustCodecBody(t, addressUint64ArgCodec("hasToken"), map[string]any{"arg0": owner, "arg1": tokenID})
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "hasToken"), Body: body, GasLimit: 5_000_000},
			GasLimit: 5_000_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}
	hasOwner := func(state avm.Storage, owner string) uint64 {
		t.Helper()
		body := mustCodecBody(t, addressArgCodec("hasOwner"), map[string]any{"arg0": owner})
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "hasOwner"), Body: body, GasLimit: 5_000_000},
			GasLimit: 5_000_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}
	tokenCountForOwner := func(state avm.Storage, owner string) uint64 {
		t.Helper()
		body := mustCodecBody(t, addressArgCodec("tokenCountForOwner"), map[string]any{"arg0": owner})
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "tokenCountForOwner"), Body: body, GasLimit: 5_000_000},
			GasLimit: 5_000_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}
	ownerCount := func(state avm.Storage) uint64 {
		t.Helper()
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "ownerCount"), GasLimit: 5_000_000},
			GasLimit: 5_000_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}

	// --- fresh contract: nothing set yet ---
	//
	// NOTE: the initial "balances" field is seeded EXPLICITLY with a
	// canonical-encoded empty map, rather than left absent from Storage (the
	// way every other example/acceptance test in this suite starts a fresh
	// contract, e.g. avm.Storage{} in multisig_acceptance_test.go /
	// pow_acceptance_test.go). A truly absent Map-typed storage field (no
	// explicit `= ...` initializer, and no prior stored value) decodes as a
	// generic zero-valued uint64 instead of an empty map, tripping
	// "AVM type error: expected map, got uint64 → EXIT_TYPE_ERROR" the moment
	// any map method (even a bare .keys()/.get()) touches it. This reproduces
	// identically for dex_amm.atlx's pre-existing SINGLE-LEVEL
	// Map<address,uint256> lpBalances field driven from a fresh
	// avm.Storage{} through a real AddLiquidity message -- so it is a
	// general "no declared-type-aware zero default for a Map storage field"
	// gap in the storage-default path, not something specific to nesting.
	// See the followup note in the test package doc / final report: fixing
	// it requires touching the storage-default generation in
	// x/aetravm/compiler/compile.go and/or the fromChunk/getData runtime
	// default construction in x/aetravm/avm/avm.go, both explicitly out of
	// scope for this task. Seeding the field here with the exported
	// avm.ValueMapEmpty()/avm.CanonicalEncode APIs works around it without
	// touching either off-limits file, and still lets this test drive every
	// nested get/set/delete/round-trip path for real.
	state := avm.Storage{"balances": mustEncodeEmptyMap(t)}
	require.Equal(t, uint64(0), ownerCount(state))
	require.Equal(t, uint64(0), hasOwner(state, aliceStr))
	require.Equal(t, big.NewInt(0), balanceOf(state, aliceStr, 7))

	// --- Credit: outer get-or-empty, inner set, outer writeback (first entry
	// for a brand-new owner: constructs a fresh inner map with
	// InnerLedger.empty()). ---
	state = send(state, alice, "Credit", map[string]any{"tokenId": uint64(7), "amount": big.NewInt(1000)})
	require.Equal(t, big.NewInt(1000), balanceOf(state, aliceStr, 7))
	require.Equal(t, uint64(1), hasToken(state, aliceStr, 7))
	require.Equal(t, uint64(1), hasOwner(state, aliceStr))
	require.Equal(t, uint64(1), ownerCount(state))
	require.Equal(t, uint64(1), tokenCountForOwner(state, aliceStr))

	// --- Credit again on the SAME (owner, tokenId): outer get finds an
	// existing inner map (no InnerLedger.empty() this time), inner get finds
	// an existing balance and adds to it -- the accumulate branch of the
	// mutate-and-writeback pattern. ---
	state = send(state, alice, "Credit", map[string]any{"tokenId": uint64(7), "amount": big.NewInt(500)})
	require.Equal(t, big.NewInt(1500), balanceOf(state, aliceStr, 7))

	// --- Credit a SECOND token id for the same owner: proves the inner map
	// independently tracks multiple keys under one outer entry. ---
	state = send(state, alice, "Credit", map[string]any{"tokenId": uint64(9), "amount": big.NewInt(42)})
	require.Equal(t, big.NewInt(42), balanceOf(state, aliceStr, 9))
	require.Equal(t, big.NewInt(1500), balanceOf(state, aliceStr, 7), "crediting tokenId 9 must not disturb tokenId 7")
	require.Equal(t, uint64(2), tokenCountForOwner(state, aliceStr))
	require.Equal(t, uint64(1), ownerCount(state), "still only one owner touched so far")

	// Explicit storage round-trip check: decode the raw "balances" field
	// straight off the wire and assert it is a TagMap whose value for alice
	// is ITSELF a TagMap with the two expected inner entries. This is
	// independent of the getters above -- it inspects the actual encoded
	// bytes produced by toChunk()/setData(), proving the nested Map survived
	// the round trip at the encoding level, not just that some later getter
	// happened to read the right numbers back.
	rawBalances, ok := state["balances"]
	require.True(t, ok, "storage must have a \"balances\" field after a Credit")
	outerValue, _, err := avm.CanonicalDecode(rawBalances)
	require.NoError(t, err)
	outerEntries, err := outerValue.AsMap()
	require.NoError(t, err)
	require.Len(t, outerEntries, 1, "exactly one owner (alice) so far")
	require.Equal(t, aliceStr, mustAsAddress(t, outerEntries[0].Key))
	innerEntries, err := outerEntries[0].Value.AsMap()
	require.NoError(t, err, "the outer map's VALUE must itself decode as a map (Map<address, Map<uint64,uint256>>)")
	require.Len(t, innerEntries, 2, "alice's inner ledger must have both tokenId 7 and tokenId 9")
	innerByTokenID := map[uint64]*big.Int{}
	for _, e := range innerEntries {
		k, err := e.Key.AsUint64()
		require.NoError(t, err)
		v, err := e.Value.AsBigInt()
		require.NoError(t, err)
		innerByTokenID[k] = v
	}
	require.Equal(t, big.NewInt(1500), innerByTokenID[7])
	require.Equal(t, big.NewInt(42), innerByTokenID[9])

	// --- Transfer: sender-side inner get -> inner set/delete -> outer
	// writeback, AND receiver-side outer get-or-empty -> inner set -> outer
	// writeback, in the SAME message. Transfers alice's full tokenId-7
	// balance to bob, which must trigger a nested delete (alice's tokenId 7
	// key disappears entirely, not just zero) while alice's tokenId 9 entry
	// is untouched, and bob gets a brand-new inner map. ---
	state = send(state, alice, "Transfer", map[string]any{"to": bobStr, "tokenId": uint64(7), "amount": big.NewInt(1500)})
	require.Equal(t, big.NewInt(0), balanceOf(state, aliceStr, 7))
	require.Equal(t, uint64(0), hasToken(state, aliceStr, 7), "a fully-transferred token id must be DELETED from the inner map, not merely zeroed")
	require.Equal(t, big.NewInt(42), balanceOf(state, aliceStr, 9), "the untouched token id must survive the sibling's nested delete")
	require.Equal(t, uint64(1), tokenCountForOwner(state, aliceStr))
	require.Equal(t, big.NewInt(1500), balanceOf(state, bobStr, 7))
	require.Equal(t, uint64(1), hasToken(state, bobStr, 7))
	require.Equal(t, uint64(1), hasOwner(state, aliceStr), "alice still holds tokenId 9, so her outer entry must remain")
	require.Equal(t, uint64(1), hasOwner(state, bobStr))
	require.Equal(t, uint64(2), ownerCount(state))

	// --- Burn: fully burning bob's only token id must delete his inner
	// entry AND, because his inner ledger is now empty, delete his OUTER
	// entry too (nested delete cascading to an outer delete). ---
	state = send(state, bob, "Burn", map[string]any{"tokenId": uint64(7), "amount": big.NewInt(1500)})
	require.Equal(t, uint64(0), hasOwner(state, bobStr), "burning an owner's only balance must drop the owner's outer entry entirely")
	require.Equal(t, big.NewInt(0), balanceOf(state, bobStr, 7))
	require.Equal(t, uint64(1), ownerCount(state), "bob's outer entry is gone; only alice remains")
	require.Equal(t, big.NewInt(42), balanceOf(state, aliceStr, 9), "bob's burn must not disturb alice's balances")

	// --- negative cases: nested get/set/delete guards actually gate. ---
	sendExpectTrap(state, bob, "Burn", map[string]any{"tokenId": uint64(7), "amount": big.NewInt(1)}) // bob has no sub-ledger anymore
	sendExpectTrap(state, alice, "Burn", map[string]any{"tokenId": uint64(999), "amount": big.NewInt(1)}) // alice has no such token id
	sendExpectTrap(state, alice, "Burn", map[string]any{"tokenId": uint64(9), "amount": big.NewInt(1000)}) // insufficient balance
}

func mustAsAddress(t *testing.T, v avm.RuntimeValue) string {
	t.Helper()
	s, err := v.AsAddress()
	require.NoError(t, err)
	return s
}

// mustEncodeEmptyMap canonical-encodes an empty map value, for seeding a
// fresh contract's Map-typed storage field explicitly (see the note above
// TestNestedMapCompilesAndExecutes).
func mustEncodeEmptyMap(t *testing.T) []byte {
	t.Helper()
	bz, err := avm.CanonicalEncode(avm.ValueMapEmpty())
	require.NoError(t, err)
	return bz
}

package compiler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file exercises the compiler-side surface of read-only cross-contract
// calls (design doc §6, §6.8): the bare free-function call syntax
// `externalGet(target, method, expectedType, args...)`, chosen specifically
// because it is NOT a dotted receiver.method(...) call, so it cannot collide
// with the existing closed dotted-call vocabulary
// (inferBuiltinMethodCallType's get/set/has/delete/keys/entries/len/...)
// and does not depend on Expr.Text ever meaning "the method name" for a
// dotted call (it never is -- see parser.go:1794-1799).

const externalGetCallerSource = `
struct DemoState {
  count: uint64 = 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @get
  func readOtherBalance(target: address): uint64 {
    return externalGet(target, "getBalance", "uint64")
  }
}
`

const externalGetCalleeSource = `
struct WalletState {
  balance: uint64 = 0
}

contract Wallet {
  storage: WalletState
  incomingMessages: WalletState

  @get
  func getBalance(): uint64 {
    return 4242
  }
}
`

// TestExternalGetCompilesToExpectedOpcodes confirms the source-level syntax
// lowers to the exact instruction sequence design doc §6.8 point 2
// specifies: OpMakeTuple (bundling the call's own arguments, here zero),
// then the target address, then OpCallExternalGet, whose Arg packs the
// compile-time getter-name selector (low 32 bits) and the expected-return
// ValueTag (high 32 bits) -- never a runtime string for either.
func TestExternalGetCompilesToExpectedOpcodes(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(externalGetCallerSource))
	require.NoError(t, err)

	require.True(t, hasOpcode(res.Module.Code, avm.OpCallExternalGet), "externalGet() must lower to OpCallExternalGet")
	require.True(t, hasOpcode(res.Module.Code, avm.OpMakeTuple), "externalGet()'s call arguments must be bundled via OpMakeTuple")

	var found bool
	for _, ins := range res.Module.Code {
		if ins.Op != avm.OpCallExternalGet {
			continue
		}
		found = true
		wantSelector := avm.GetterNameSelector("getBalance")
		gotSelector := uint32(ins.Arg & 0xFFFFFFFF)
		require.Equal(t, wantSelector, gotSelector, "selector must be avm.GetterNameSelector(method), matching the same hash a real getter's own dispatch uses")
		wantTag, ok := avm.ExternalGetExpectedTag("uint64")
		require.True(t, ok)
		gotTag := avm.ValueTag(ins.Arg >> 32)
		require.Equal(t, wantTag, gotTag, "expected-return tag must be packed from the declared expectedType literal")
	}
	require.True(t, found, "compiled module must contain an OpCallExternalGet instruction")
}

// TestExternalGetEndToEndAcrossTwoCompiledContracts is the genuine
// successful-cross-contract-read case, run through the REAL interpreter
// across two INDEPENDENTLY compiled modules (no shared source, no shared
// type declarations -- exactly the "separately compiled module with no
// shared interface" scenario design doc §6.8 point 4 describes), wired
// through a hand-built resolver standing in for the keeper's
// newExternalGetResolver.
func TestExternalGetEndToEndAcrossTwoCompiledContracts(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	callerRes, err := c.Compile([]byte(externalGetCallerSource))
	require.NoError(t, err)
	calleeRes, err := c.Compile([]byte(externalGetCalleeSource))
	require.NoError(t, err)

	// NOTE: the compiler's SelectorRegistry entry for a @get function is the
	// full-signature selector, a DIFFERENT hash than
	// avm.GetterNameSelector's name-only alias (getmethod.go:82-91) --
	// EntryQuery dispatch recognizes both, the alias being exactly what lets
	// a caller invoke a getter knowing only its exact source-level name
	// (the same mechanism ContractGet's existing production RPC already
	// relies on, contract_get.go:59). The real proof this end-to-end test
	// exists for is that dispatching via the ALIAS selector -- what
	// OpCallExternalGet's Arg actually carries -- reaches the callee's
	// compiled getBalance() body and returns its value, asserted below.

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	const targetAddr = "wallet-under-test"
	resolver := func(addr string, gasBudget uint64) (avm.Module, avm.Storage, bool, error) {
		require.Equal(t, targetAddr, addr)
		return calleeRes.Module, avm.Storage{}, true, nil
	}

	callerSelector := getterSelector(t, callerRes, "readOtherBalance")
	body, err := encodeAddressArgBody(targetAddr)
	require.NoError(t, err)

	exec, err := runner.Run(callerRes.Module, avm.Storage{}, avm.RuntimeContext{
		Entry: avm.EntryQuery,
		Message: async.MessageEnvelope{
			Opcode:   callerSelector,
			Body:     body,
			GasLimit: 200_000,
		},
		GasLimit:             200_000,
		ExternalGetResolver:  resolver,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	got, err := exec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(4242), got)
}

// encodeAddressArgBody encodes a single positional "arg0" address-typed
// field the same way the message-field decoder (IRExprMsgField) expects,
// matching the wire shape query args already use
// (types/contract_get.go's EncodeMessageBody / runtimeMessageFieldValue) --
// avoiding a dependency on x/contracts/types from this package by inlining
// the tiny JSON shape here.
func encodeAddressArgBody(addr string) ([]byte, error) {
	type fieldEntry struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	return json.Marshal([]fieldEntry{{Name: "arg0", Type: "address", Value: addr}})
}

// TestExternalGetRejectsNonAddressTarget, TestExternalGetRequiresLiteralMethodName,
// TestExternalGetRequiresLiteralExpectedType, and
// TestExternalGetRejectsUnsupportedExpectedType pin down inferExprType's
// compile-time validation (design doc §6.8 point 4) so a malformed call is
// rejected at compile time, not silently miscompiled.

func TestExternalGetRejectsNonAddressTarget(t *testing.T) {
	src := `
struct DemoState { count: uint64 = 0 }
contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  @get
  func bad(): uint64 {
    return externalGet(42, "getBalance", "uint64")
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err, "externalGet() target must be type-checked as an address, not an arbitrary integer")
}

func TestExternalGetRequiresLiteralMethodName(t *testing.T) {
	src := `
struct DemoState { count: uint64 = 0 }
contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  @get
  func bad(target: address, method: string): uint64 {
    return externalGet(target, method, "uint64")
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err, "externalGet()'s method name must be a compile-time string literal, not a runtime value")
}

func TestExternalGetRequiresLiteralExpectedType(t *testing.T) {
	src := `
struct DemoState { count: uint64 = 0 }
contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  @get
  func bad(target: address, typ: string): uint64 {
    return externalGet(target, "getBalance", typ)
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err, "externalGet()'s expected type must be a compile-time string literal")
}

func TestExternalGetRejectsUnsupportedExpectedType(t *testing.T) {
	src := `
struct DemoState { count: uint64 = 0 }
contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  @get
  func bad(target: address): uint64 {
    return externalGet(target, "getBalance", "Map<uint64,uint64>")
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err, "externalGet()'s expected type must be one of the supported scalar spellings, not a compound type")
}

func TestExternalGetRequiresAtLeastThreeArguments(t *testing.T) {
	src := `
struct DemoState { count: uint64 = 0 }
contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  @get
  func bad(target: address): uint64 {
    return externalGet(target, "getBalance")
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err, "externalGet() requires at least three arguments")
}

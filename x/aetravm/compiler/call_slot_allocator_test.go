package compiler

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// This file is a regression test for a real, previously-unfixed bug found
// while building reference-contract evidence for the intra-contract
// CALL/RET mechanism (design doc §1.1's module-wide local-slot allocator):
// a called function's slot range was carved out of the shared
// callTargetRegistry.slotCounter only at the moment it was FIRST discovered
// (compileCalledFunction's `base := *reg.slotCounter`), while an
// entrypoint/match-arm's own ordinary locals were claimed purely from that
// region's own env.nextLocalSlot -- two counters only ever reconciled at
// the very END of a whole top-level region's lowering (baseEnv's
// advanceSlots closure), never continuously as each individual local was
// claimed.
//
// Concretely: if a function F is called from match arm A (compiling F for
// the first time, claiming slots [0, k)), and is called AGAIN (dedup-reused,
// same fixed [0, k) range) from a DIFFERENT match arm B that ALSO calls a
// second function G earlier in arm B's own body (binding G's tuple return
// into locals that occupy slots [0, 1) -- arm B's own env.nextLocalSlot
// independently starts back at 0, since sibling arms are designed to reuse
// slot ranges) -- then calling F from arm B silently overwrites arm B's
// still-live locals with whatever F's own body last left in slots [0, k),
// with NO trap: a wrong value silently committed to storage. Reproduced
// live in examples/avm/bridge/bridge_verify.atlx's LightClientVerify
// handler (calls both merkleWalk, called from 3 sites total, and
// verifyQuorum) while refactoring it to use the new call mechanism instead
// of its old duplicated-loop shape; fixed by claimLocalSlot (compile.go),
// which keeps env.nextLocalSlot and the module-wide slotCounter
// continuously synchronized in both directions instead of only at region
// boundaries.

const twoRealCallsSharedCallerLocalsSource = `
struct DemoState {
  merkleShaResult: uint64 = 0
  sigCount: uint64 = 0
  thresholdResult: uint64 = 0
  accepted: uint64 = 0
}

const HASH_LEN = 32
const PUBKEY_LEN = 33
const SIG_LEN = 64
const LEAF_TAG = 0x00
const NODE_TAG = 0x01

@message(0xB101)
struct VerifyMerkleSha {
  leaf: bytes
  proof: bytes
  directions: bytes
  root: bytes
}

@message(0xB104)
struct LightClientVerify {
  headerHash: bytes
  pubkeys: bytes
  sigs: bytes
  leaf: bytes
  proof: bytes
  directions: bytes
  stateRoot: bytes
}

type DemoMsg = VerifyMerkleSha | LightClientVerify

contract Demo {
  storage: DemoState
  incomingMessages: DemoMsg

  @store
  func DemoState.load() {
    return DemoState.fromChunk(contract.getData())
  }

  @store
  func DemoState.save(self) {
    contract.setData(self.toChunk())
  }

  @pure
  func leafHashSha(leaf: bytes): hash32 {
    return sha256(concat(toBytesBE(LEAF_TAG, 1), leaf))
  }

  @pure
  func hashNodeSha(left: bytes, right: bytes): hash32 {
    return sha256(concat(toBytesBE(NODE_TAG, 1), concat(left, right)))
  }

  @pure
  func pubkeyEq(a: bytes, b: bytes): bool {
    return (fromBytesBE(subBytes(a, 0, HASH_LEN)) == fromBytesBE(subBytes(b, 0, HASH_LEN)))
        && (byteAt(a, PUBKEY_LEN - 1) == byteAt(b, PUBKEY_LEN - 1))
  }

  // Real (multi-statement, early-returning) function called from TWO
  // different match arms below: VerifyMerkleSha (compiling it for the
  // first time) and LightClientVerify (a dedup-reuse of that same
  // compiled block).
  @impure
  func merkleWalk(leaf: bytes, proof: bytes, directions: bytes, root: bytes): uint64 {
    const n = len(proof) / HASH_LEN
    const lenOk = (len(proof) == n * HASH_LEN)
        && (len(directions) == n)
        && (len(root) == HASH_LEN)
    if (!lenOk) {
      return 0
    }
    var cur = leafHashSha(leaf)
    var i = 0
    while (i < n) {
      const sib = subBytes(proof, i * HASH_LEN, HASH_LEN)
      const goLeft = byteAt(directions, i) == 0
      cur = goLeft ? hashNodeSha(cur, sib) : hashNodeSha(sib, cur)
      i += 1
    }
    return (fromBytesBE(cur) == fromBytesBE(root)) ? 1 : 0
  }

  // Real, tuple-returning function called ONLY from LightClientVerify,
  // BEFORE that arm's call to the already-elsewhere-compiled merkleWalk --
  // exactly the ordering that exposed the bug (this call's destructured
  // results must survive the SUBSEQUENT merkleWalk call unchanged).
  @impure
  func verifyQuorum(headerHash: bytes, pubkeys: bytes, sigs: bytes): (uint64, uint64) {
    var valid = 0
    var i = 0
    const k = len(sigs) / SIG_LEN
    while (i < k) {
      const pk = subBytes(pubkeys, i * PUBKEY_LEN, PUBKEY_LEN)
      const sig = subBytes(sigs, i * SIG_LEN, SIG_LEN)
      var seen = 0
      var j = 0
      while (j < i) {
        const pkj = subBytes(pubkeys, j * PUBKEY_LEN, PUBKEY_LEN)
        if (pubkeyEq(pk, pkj)) {
          seen = 1
        }
        j += 1
      }
      if (seen == 0 && verifySecp256k1(headerHash, sig, pk)) {
        valid += 1
      }
      i += 1
    }
    const total = len(pubkeys) / PUBKEY_LEN
    const quorum = (valid * 3 > total * 2) ? 1 : 0
    return (valid, quorum)
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy DemoMsg.fromSegment(in.body)
    match (msg) {
      // Compiles merkleWalk for the first time, claiming its fixed slot
      // range relative to whatever the module-wide counter is at THIS
      // point.
      VerifyMerkleSha => {
        var st = lazy DemoState.load()
        const result = merkleWalk(msg.leaf, msg.proof, msg.directions, msg.root)
        st.merkleShaResult = result
        st.save()
      }
      // A SIBLING arm: its own env.nextLocalSlot independently restarts
      // from the pre-match value (siblings are designed to reuse slot
      // ranges), so verifyQuorum's destructured (valid, quorum) locals can
      // land on the SAME physical slots merkleWalk's own body uses
      // internally -- this arm's call to merkleWalk below is a dedup-reuse
      // of the ALREADY-compiled block from the sibling arm above, at its
      // ALREADY-fixed slot range.
      LightClientVerify => {
        var st = lazy DemoState.load()
        const (valid, quorum) = verifyQuorum(msg.headerHash, msg.pubkeys, msg.sigs)
        const merkleOk = merkleWalk(msg.leaf, msg.proof, msg.directions, msg.stateRoot)
        st.sigCount = valid
        st.thresholdResult = quorum
        st.merkleShaResult = merkleOk
        st.accepted = (quorum == 1 && merkleOk == 1) ? 1 : 0
        st.save()
      }
      else => {}
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func sigCount(): uint64 {
    const st = lazy DemoState.load()
    return st.sigCount
  }

  @get
  func thresholdResult(): uint64 {
    const st = lazy DemoState.load()
    return st.thresholdResult
  }

  @get
  func merkleShaResult(): uint64 {
    const st = lazy DemoState.load()
    return st.merkleShaResult
  }

  @get
  func accepted(): uint64 {
    const st = lazy DemoState.load()
    return st.accepted
  }
}
`

// TestCompileTwoRealCallsShareNoSlotWithCallerLocals reproduces and pins the
// fix for the slot-collision bug documented above: a dedup-reused real call
// (merkleWalk, called from a sibling arm first) must never overwrite a
// STILL-LIVE local (valid/quorum) that a DIFFERENT real call (verifyQuorum)
// bound earlier in the SAME arm.
func TestCompileTwoRealCallsShareNoSlotWithCallerLocals(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(twoRealCallsSharedCallerLocalsSource))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	// Prime merkleWalk's compiled slot range via the sibling arm first,
	// exactly as the bug required.
	shaBody, err := res.MessageBodies["VerifyMerkleSha"].Encode(map[string]any{
		"leaf":       []byte{0xAB},
		"proof":      make([]byte, 32),
		"directions": []byte{0},
		"root":       make([]byte, 32),
	})
	require.NoError(t, err)
	shaExec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: sdk.AccAddress(bytes.Repeat([]byte{0xaa}, 20)),
		GasLimit:        1_000_000,
		Message: async.MessageEnvelope{
			Opcode:   res.MessageBodyOpcodes["VerifyMerkleSha"],
			QueryID:  1,
			Body:     shaBody,
			GasLimit: 1_000_000,
		},
	})
	require.NoError(t, err, "submit VerifyMerkleSha")
	require.Equal(t, async.ResultOK, shaExec.ResultCode)

	// Now submit LightClientVerify (calls verifyQuorum THEN the
	// already-compiled merkleWalk) against a FRESH storage state, with a
	// zero-validator batch (valid=0, quorum=0 -- deterministic, no real
	// signatures needed to hit the bug) and an empty (n=0) Merkle proof
	// against an all-zero stateRoot that a real sha256 leaf hash can never
	// equal, so the exact numeric verdict is unambiguous and independent of
	// hash output.
	lcBody, err := res.MessageBodies["LightClientVerify"].Encode(map[string]any{
		"headerHash": make([]byte, 32),
		"pubkeys":    []byte{},
		"sigs":       []byte{},
		"leaf":       []byte{0xCD},
		"proof":      []byte{}, // n=0, length-discipline check passes trivially
		"directions": []byte{},
		"stateRoot":  make([]byte, 32),
	})
	require.NoError(t, err)
	lcExec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: sdk.AccAddress(bytes.Repeat([]byte{0xaa}, 20)),
		GasLimit:        1_000_000,
		Message: async.MessageEnvelope{
			Opcode:   res.MessageBodyOpcodes["LightClientVerify"],
			QueryID:  2,
			Body:     lcBody,
			GasLimit: 1_000_000,
		},
	})
	require.NoError(t, err, "submit LightClientVerify")
	require.Equal(t, async.ResultOK, lcExec.ResultCode)

	getU64 := func(state avm.Storage, getter string) uint64 {
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, getter), GasLimit: 100_000},
			GasLimit: 100_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}

	// The bug's failure mode was either a hard AVM type-error trap (a
	// bytes/hash value landing where a uint64 was expected) OR silent
	// corruption of valid/quorum's numeric value -- either way, these must
	// come back as the deterministically-correct verdict: an empty
	// validator batch never meets quorum (0), and an empty Merkle proof
	// against a non-matching root never verifies (0).
	require.Equal(t, uint64(0), getU64(lcExec.State, "sigCount"), "valid must survive the subsequent merkleWalk call uncorrupted")
	require.Equal(t, uint64(0), getU64(lcExec.State, "thresholdResult"), "quorum must survive the subsequent merkleWalk call uncorrupted")
	require.Equal(t, uint64(0), getU64(lcExec.State, "merkleShaResult"), "merkleWalk's own dedup-reused-call result must be correct")
	require.Equal(t, uint64(0), getU64(lcExec.State, "accepted"), "combined verdict must reflect the uncorrupted valid/quorum/merkleOk values")
}

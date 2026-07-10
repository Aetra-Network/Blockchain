package compiler

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
	"github.com/stretchr/testify/require"
)

// counterSource is the canonical ATLX counter fixture. It deliberately sticks
// to the formatter-representable subset of the surface (no lazy bindings, no
// bare expression statements) so format round-trip tests can compile the
// formatted output; storage writes go through `set state.field`, which maps to
// the same avm.Storage keys as before.
const counterSource = `
struct CounterState {
  count: u64 = 0
  owner: Address = "AEowner"
}

@message(11)
struct Increment {
  amount: u64
}

@message(0x2001)
struct Touch {
  note: u64
}

type ExternalMsg = Increment
type InternalMsg = Touch

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg
  incomingExternal: ExternalMsg
  namespace "counter"
  chain "avm-local"

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = InternalMsg.fromSegment(in.body)
    match (msg) {
      Touch => {
        set state.count = in.queryId + msg.note
      }
      else => {
        assert (in.body.isEmpty()) throw 0xFFFF
      }
    }
  }

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = ExternalMsg.fromSegment(inMsg)
    match (msg) {
      Increment => {
        set state.count = msg.amount
      }
      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = CounterState.load()
    return st.count
  }
}
`

// counterIncrementBody encodes an Increment message body for the compiled
// counterSource fixture so runtime executions can decode msg.amount.
func counterIncrementBody(t *testing.T, res *Result, amount uint64) []byte {
	t.Helper()
	bodyCodec, ok := res.MessageBodies["Increment"]
	if !ok {
		t.Fatal("counter fixture is missing the Increment message body codec")
	}
	body, err := bodyCodec.Encode(map[string]any{"amount": amount})
	if err != nil {
		t.Fatalf("encode Increment body: %v", err)
	}
	return body
}

// getterSelector resolves the auto-derived selector of a @get func from the
// selector registry (getter selectors are no longer pinned in source).
func getterSelector(t *testing.T, res *Result, name string) uint32 {
	t.Helper()
	for _, entry := range res.SelectorRegistry.Entries {
		if entry.Kind == "getter" && entry.Name == name {
			return entry.Selector
		}
	}
	t.Fatalf("getter %q not found in selector registry", name)
	return 0
}

// richSemanticsSource exercises enums, helper calls, loops, and bounced
// handling through the canonical surface. The legacy migrate message is gone:
// the migrate surface no longer exists in the language (EntryMigrate remains
// runtime-only).
const richSemanticsSource = `
enum Mode {
  Off;
  On;
}

struct CounterState {
  count: u64 = 0
}

@message(11)
struct Increment {}

type ExternalMsg = Increment

func plusOne(x: u64) -> u64 {
  if 1 == 1 {
    return x + 1
  } else {
    return x
  }
}

func choose(mode: Mode) -> u64 {
  match mode {
    Mode.Off {
      return 0
    }
    Mode.On {
      return 1
    }
  }
}

contract Counter {
  storage: CounterState
  incomingExternal: ExternalMsg
  namespace "counter"
  chain "avm-local"

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = ExternalMsg.fromSegment(inMsg)
    match (msg) {
      Increment => {
        const sanity = plusOne(1)
        const mode_check = choose(Mode.On)
        var step = 2
        const rounds = 3
        var st = lazy CounterState.load()
        for i in 0 to rounds {
          st.count += step
        }
        st.save()
      }
      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
    var st = lazy CounterState.load()
    st.count += 3
    st.save()
  }

  @get
  func getCount(): u64 {
    const st = CounterState.load()
    return st.count
  }
}
`

const signatureAuthSource = `
const ERR_NOT_OWNER = 1001
const ERR_BAD_NONCE = 1002
const ERR_BAD_MSG = 1003

@storage
struct AuthState {
  owner: address
  publicKey: bytes
  nonce: uint32
  lastNow: int64
  lastLogicalTime: uint64
  lastBlockLogicalTime: uint64
  lastValue: coins
}

@message(0x2001)
struct SignedCall {
  nonce: uint32
  payload: bytes
  signature: bytes
}

type InternalMsg = SignedCall

contract AuthGate {
  storage: AuthState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy SignedCall.fromSegment(in.body)
    const st = lazy AuthState.fromChunk(contract.getData())

    assert (in.senderAddress == st.owner) throw ERR_NOT_OWNER
    assert (msg.nonce == st.nonce + 1) throw ERR_BAD_NONCE
    assert (isSignatureValid(hash(msg.payload), msg.signature, st.publicKey)) throw ERR_BAD_MSG
  }
}
`

const strictErrorModelSource = `
const ERR_BAD_SENDER = 2001
const ERR_BAD_NONCE = 2002
const ERR_ASSERT = 2003
const ERR_THROW = 2004

@storage
struct ErrorState {
  counter: uint64 = 0
  owner: address = "AEowner"
  nonce: uint32 = 0
}

@message(0x2301)
struct Control {
  nonce: uint32
  step: uint64
  mode: uint32
}

type InternalMsg = Control

contract ErrorGate {
  storage: ErrorState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy Control.fromSegment(in.body)
    var st = lazy ErrorState.load()
    st.counter += msg.step
    assert (in.senderAddress == st.owner) throw ERR_BAD_SENDER
    assert (msg.nonce == st.nonce + 1) throw ERR_BAD_NONCE
    st.nonce = msg.nonce

    if msg.mode == 0 {
      assert (false) throw ERR_ASSERT
    } else if msg.mode == 1 {
      throw ERR_THROW
    }

    st.save()
  }
}
`

func TestCompileCounterShouldBeExampleSource(t *testing.T) {
	examplePath := filepath.Join("..", "..", "..", "examples", "avm", "counter_should_be.atlx")
	src, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}

	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	res, err := c.Compile(src)
	if err != nil {
		t.Fatalf("compile example: %v", err)
	}
	if err := res.Manifest.Validate(); err != nil {
		t.Fatalf("manifest validate: %v", err)
	}
	if len(res.ModuleBytes) == 0 || res.ModuleHash == [32]byte{} {
		t.Fatal("example compile did not produce module artifacts")
	}
	if len(res.SelectorRegistry.Entries) == 0 {
		t.Fatal("example compile did not produce selector registry entries")
	}

	formatted, err := FormatSourceNamed(examplePath, string(src))
	if err != nil {
		t.Fatalf("format example: %v", err)
	}
	if !strings.Contains(formatted, "const msg =") {
		t.Fatal("formatted example did not use const bindings")
	}
	if !strings.Contains(formatted, "var st =") {
		t.Fatal("formatted example did not use var bindings")
	}
	if !strings.Contains(formatted, "func onInternalMessage(in: InMessage)") {
		t.Fatal("formatted example did not preserve reserved internal handler name")
	}
	if !strings.Contains(formatted, "func onBouncedMessage(in: InMessageBounced)") {
		t.Fatal("formatted example did not preserve reserved bounced handler name")
	}
	if !strings.Contains(formatted, "@external func onExternalMessage(inMsg: Segment)") {
		t.Fatal("formatted example did not preserve external handler shape")
	}
	for _, forbidden := range []string{"let ", "val ", "mut "} {
		if strings.Contains(formatted, forbidden) {
			t.Fatalf("formatted example still contains forbidden keyword %q", forbidden)
		}
	}
}

func TestCompileCounterContract(t *testing.T) {
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	res1, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res1 == nil {
		t.Fatal("nil result")
	}
	if len(res1.ModuleBytes) == 0 {
		t.Fatal("empty module bytes")
	}
	if err := res1.Manifest.Validate(); err != nil {
		t.Fatalf("manifest validate: %v", err)
	}
	// event/wallet action declarations are no longer part of ATLX; manifests
	// compiled from source must not carry either surface.
	if len(res1.Manifest.WalletActions) != 0 {
		t.Fatalf("expected no wallet actions, got %d", len(res1.Manifest.WalletActions))
	}
	if len(res1.Manifest.Events) != 0 {
		t.Fatalf("expected no events, got %d", len(res1.Manifest.Events))
	}
	if err := avm.VerifyInterface(res1.Module, res1.Manifest); err != nil {
		t.Fatalf("verify interface: %v", err)
	}
	if _, err := avm.DecodeModule(res1.ModuleBytes); err != nil {
		t.Fatalf("decode module: %v", err)
	}
	if _, err := avm.HashStateInit(res1.StateInit); err != nil {
		t.Fatalf("hash state init: %v", err)
	}
	if len(res1.SelectorRegistry.Entries) == 0 {
		t.Fatal("empty selector registry")
	}
	if res1.IR == nil || len(res1.IR.Entries) == 0 {
		t.Fatal("missing compiler IR")
	}
	if res1.CodeChunk == nil {
		t.Fatal("missing code chunk artifact")
	}
	if res1.CodeChunkHash == [32]byte{} {
		t.Fatal("missing code chunk hash")
	}
	if !hasOpcode(res1.Module.Code, avm.OpWriteStorage) {
		t.Fatal("compiled module does not write storage")
	}
	if !hasOpcode(res1.Module.Code, avm.OpReadStorage) {
		t.Fatal("compiled module does not read storage")
	}
	if !hasOpcode(res1.Module.Code, avm.OpReadMsgQueryID) {
		t.Fatal("compiled module does not lower message parameter reads")
	}

	res2, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("recompile: %v", err)
	}
	if string(res1.ModuleBytes) != string(res2.ModuleBytes) {
		t.Fatal("module bytes are not deterministic")
	}
	if res1.ModuleHash != res2.ModuleHash {
		t.Fatal("module hash changed across identical builds")
	}
	if res1.ManifestHash != res2.ManifestHash {
		t.Fatal("manifest hash changed across identical builds")
	}
	if res1.StateInitHash != res2.StateInitHash {
		t.Fatal("state init hash changed across identical builds")
	}

	dir := t.TempDir()
	if err := writeCompileArtifactsForTest(dir, res1); err != nil {
		t.Fatalf("write artifacts: %v", err)
	}
	for _, name := range []string{"module.bin", "module.chunk", "interface.json", "stateinit.json", "storage-layout.json", "selector-registry.json", "codecs.json", "diagnostics.json", "ir.json", "dependency-lock.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
}

func TestCompileHashCommitmentsChangeOnSchemaAndSelectorMutation(t *testing.T) {
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	base, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("compile base: %v", err)
	}

	reorderedSource := strings.Replace(counterSource,
		"struct CounterState {\n  count: u64 = 0\n  owner: Address = \"AEowner\"\n}",
		"struct CounterState {\n  owner: Address = \"AEowner\"\n  count: u64 = 0\n}", 1)
	reordered, err := c.Compile([]byte(reorderedSource))
	if err != nil {
		t.Fatalf("compile reordered storage: %v", err)
	}
	if base.StorageLayout.LayoutHash == reordered.StorageLayout.LayoutHash {
		t.Fatal("storage layout hash did not change when field order changed")
	}
	if base.StorageCodec.Hash == reordered.StorageCodec.Hash {
		t.Fatal("storage codec hash did not change when field order changed")
	}

	// message opcodes now live on @message annotations; they are committed via
	// the module bytecode (dispatch constants) and the state init, so mutating
	// the opcode must change those commitments.
	opcodeMutatedSource := strings.Replace(counterSource, "@message(11)", "@message(21)", 1)
	opcodeMutated, err := c.Compile([]byte(opcodeMutatedSource))
	if err != nil {
		t.Fatalf("compile opcode mutated: %v", err)
	}
	if base.ModuleHash == opcodeMutated.ModuleHash {
		t.Fatal("module hash did not change when message opcode changed")
	}
	if base.StateInitHash == opcodeMutated.StateInitHash {
		t.Fatal("state init hash did not change when message opcode changed")
	}

	// getter selectors are derived from the getter name; renaming the getter
	// must change the selector registry and ABI commitments.
	getterMutatedSource := strings.ReplaceAll(counterSource, "getCount", "getTotal")
	getterMutated, err := c.Compile([]byte(getterMutatedSource))
	if err != nil {
		t.Fatalf("compile getter mutated: %v", err)
	}
	if base.SelectorRegistry.RegistryHash == getterMutated.SelectorRegistry.RegistryHash {
		t.Fatal("selector registry hash did not change when getter selector changed")
	}
	if base.ManifestHash == getterMutated.ManifestHash {
		t.Fatal("manifest hash did not change when getter selector changed")
	}

	namespaceMutatedSource := strings.Replace(counterSource, "namespace \"counter\"", "namespace \"counter-alt\"", 1)
	namespaceMutated, err := c.Compile([]byte(namespaceMutatedSource))
	if err != nil {
		t.Fatalf("compile namespace mutated: %v", err)
	}
	if base.StateInitHash == namespaceMutated.StateInitHash {
		t.Fatal("state init hash did not change when namespace changed")
	}
}

func TestCompileFilesPackageImportLock(t *testing.T) {
	src := strings.Replace(counterSource, "struct CounterState", "package examples.counter\nimport stdlib \"avm/stdlib@avm-stdlib/v1\"\nimport \"aetra/math@1.2.3\"\n\nstruct CounterState", 1)
	c, _ := New(DefaultOptions())
	res, err := c.CompileFiles([]NamedSource{{Name: "counter.atlx", Data: []byte(src)}})
	if err != nil {
		t.Fatalf("compile files: %v", err)
	}
	if res.Source.Package != "examples.counter" {
		t.Fatalf("package not parsed: %q", res.Source.Package)
	}
	if len(res.DependencyLock.Entries) < 2 {
		t.Fatalf("dependency lock missing entries: %d", len(res.DependencyLock.Entries))
	}
	if len(res.StateInit.DependencyHashes) != len(res.DependencyLock.Entries)+1 {
		t.Fatalf("state init dependency hashes not committed")
	}
}

func TestCompileCommitmentsStableUnderImportAndHelperReorder(t *testing.T) {
	base := `
package app.counter
import "lib/math@1.0.0"
import "lib/meta@1.0.0"

struct CounterState {
  count: u64 = 0
}

@message(11)
struct Increment {}

type InternalMsg = Increment

func bump(x: u64) -> u64 {
  return addOne(x)
}

func label(x: u64) -> u64 {
  return decorate(x)
}

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = InternalMsg.fromSegment(in.body)
    match (msg) {
      Increment => {
        set state.count = bump(1)
      }
      else => {
        assert (in.body.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = CounterState.load()
    return st.count
  }
}
`
	reordered := `
package app.counter
import "lib/meta@1.0.0"
import "lib/math@1.0.0"

struct CounterState {
  count: u64 = 0
}

@message(11)
struct Increment {}

type InternalMsg = Increment

func label(x: u64) -> u64 {
  return decorate(x)
}

func bump(x: u64) -> u64 {
  return addOne(x)
}

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg

  // reorder-only comment noise should not affect canonical commitments
  @get
  func getCount(): u64 {
    const st = CounterState.load()
    return st.count
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = InternalMsg.fromSegment(in.body)
    match (msg) {
      Increment => {
        set state.count = bump(1)
      }
      else => {
        assert (in.body.isEmpty()) throw 0xFFFF
      }
    }
  }
}
`
	libMath := `
package lib.math

func addOne(x: u64) -> u64 {
  return x + 1
}
`
	libMeta := `
package lib.meta

func decorate(x: u64) -> u64 {
  return x
}
`
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	resolver := testResolver{
		files: map[string]NamedSource{
			"lib/math@1.0.0": {Name: "math.atlx", Data: []byte(libMath)},
			"lib/meta@1.0.0": {Name: "meta.atlx", Data: []byte(libMeta)},
		},
	}
	c.opts.Resolver = resolver
	left, err := c.CompileFiles([]NamedSource{{Name: "base.atlx", Data: []byte(base)}})
	if err != nil {
		t.Fatalf("compile base: %v", err)
	}
	right, err := c.CompileFiles([]NamedSource{{Name: "reordered.atlx", Data: []byte(reordered)}})
	if err != nil {
		t.Fatalf("compile reordered: %v", err)
	}
	if left.ModuleHash != right.ModuleHash {
		t.Fatalf("module hash changed under reorder/no-op source edits")
	}
	if left.ManifestHash != right.ManifestHash {
		t.Fatalf("ABI hash changed under reorder/no-op source edits")
	}
	if left.StateInitHash != right.StateInitHash {
		t.Fatalf("state init hash changed under reorder/no-op source edits")
	}
	if left.SelectorRegistry.RegistryHash != right.SelectorRegistry.RegistryHash {
		t.Fatalf("selector registry hash changed under reorder/no-op source edits")
	}
	if left.DependencyLock.LockHash != right.DependencyLock.LockHash {
		t.Fatalf("dependency lock hash changed under reorder/no-op source edits")
	}
	if left.DependencyLock.Package != right.DependencyLock.Package {
		t.Fatalf("dependency lock package changed under reorder/no-op source edits")
	}
	if len(left.DependencyLock.Entries) == 0 {
		t.Fatal("dependency lock missing entries")
	}
	if !reflect.DeepEqual(left.DependencyLock.Entries, right.DependencyLock.Entries) {
		t.Fatalf("dependency lock entries changed under reorder/no-op source edits:\nleft=%+v\nright=%+v", left.DependencyLock.Entries, right.DependencyLock.Entries)
	}
	if left.DependencyLock.Entries[0].ABIHash == [32]byte{} {
		t.Fatal("dependency lock missing ABI hash commitment")
	}
}

func TestCompileRuntimeDifferentialStabilityAcrossFormattingAndRepeatedRuns(t *testing.T) {
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}

	parsed, err := ParseSourceNamed("counter.atlx", counterSource)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	formatted := FormatSource(parsed)
	if formatted == "" {
		t.Fatal("formatted source must not be empty")
	}

	original, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("compile original: %v", err)
	}
	recompiled, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("compile repeated: %v", err)
	}
	formattedResult, err := c.Compile([]byte(formatted))
	if err != nil {
		t.Fatalf("compile formatted: %v", err)
	}

	if original.ModuleHash != recompiled.ModuleHash || original.ManifestHash != recompiled.ManifestHash || original.StateInitHash != recompiled.StateInitHash {
		t.Fatal("repeat compile changed canonical commitments")
	}
	if original.ModuleHash != formattedResult.ModuleHash || original.ManifestHash != formattedResult.ManifestHash || original.StateInitHash != formattedResult.StateInitHash {
		t.Fatal("format round-trip changed canonical commitments")
	}
	if original.DependencyLock.LockHash != recompiled.DependencyLock.LockHash || original.DependencyLock.LockHash != formattedResult.DependencyLock.LockHash {
		t.Fatal("dependency lock changed across equivalent compiler inputs")
	}
	if original.SelectorRegistry.RegistryHash != recompiled.SelectorRegistry.RegistryHash || original.SelectorRegistry.RegistryHash != formattedResult.SelectorRegistry.RegistryHash {
		t.Fatal("selector registry changed across equivalent compiler inputs")
	}
	if !reflect.DeepEqual(original.Manifest.WalletActions, recompiled.Manifest.WalletActions) || !reflect.DeepEqual(original.Manifest.WalletActions, formattedResult.Manifest.WalletActions) {
		t.Fatal("wallet actions changed across equivalent compiler inputs")
	}

	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	ctx := avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 9, Body: counterIncrementBody(t, original, 42), GasLimit: 100_000},
		GasLimit: 100_000,
	}
	storage := avm.Storage{"count": avm.EncodeU64(41)}

	first, err := runner.Run(original.Module, storage, ctx)
	if err != nil {
		t.Fatalf("run original: %v", err)
	}
	second, err := runner.Run(recompiled.Module, storage, ctx)
	if err != nil {
		t.Fatalf("run repeated: %v", err)
	}
	third, err := runner.Run(formattedResult.Module, storage, ctx)
	if err != nil {
		t.Fatalf("run formatted: %v", err)
	}

	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, third) {
		t.Fatalf("runtime execution drifted across equivalent compiler inputs:\noriginal=%#v\nrecompiled=%#v\nformatted=%#v", first, second, third)
	}

	proof1, err := avm.BuildExecutionProof(original.Module, storage, ctx, first)
	if err != nil {
		t.Fatalf("build proof original: %v", err)
	}
	proof2, err := avm.BuildExecutionProof(recompiled.Module, storage, ctx, second)
	if err != nil {
		t.Fatalf("build proof repeated: %v", err)
	}
	proof3, err := avm.BuildExecutionProof(formattedResult.Module, storage, ctx, third)
	if err != nil {
		t.Fatalf("build proof formatted: %v", err)
	}
	if avm.ExecutionProofHash(proof1) != avm.ExecutionProofHash(proof2) || avm.ExecutionProofHash(proof1) != avm.ExecutionProofHash(proof3) {
		t.Fatal("execution proof hash drifted across equivalent compiler/runtime inputs")
	}
}

// TestCompileIsByteIdenticalAcrossManyRepeatedRuns guards against
// nondeterministic codegen (e.g. an unsorted map range feeding IR/bytecode
// emission). A single repeat compile can pass by chance since Go only
// randomizes map iteration start position, not every possible ordering;
// compiling many times in a loop drives the odds of masking a real ordering
// bug down to effectively zero.
func TestCompileIsByteIdenticalAcrossManyRepeatedRuns(t *testing.T) {
	const iterations = 300

	assertStable := func(t *testing.T, name string, sources []NamedSource, opts Options) {
		t.Helper()
		var firstModuleBytes []byte
		var firstModuleHash, firstManifestHash, firstStateInitHash, firstLockHash, firstRegistryHash [32]byte
		for i := 0; i < iterations; i++ {
			c, err := New(opts)
			if err != nil {
				t.Fatalf("%s: new compiler on iteration %d: %v", name, i, err)
			}
			res, err := c.CompileFiles(append([]NamedSource(nil), sources...))
			if err != nil {
				t.Fatalf("%s: compile on iteration %d: %v", name, i, err)
			}
			if i == 0 {
				firstModuleBytes = res.ModuleBytes
				firstModuleHash = res.ModuleHash
				firstManifestHash = res.ManifestHash
				firstStateInitHash = res.StateInitHash
				firstLockHash = res.DependencyLock.LockHash
				firstRegistryHash = res.SelectorRegistry.RegistryHash
				continue
			}
			if !bytes.Equal(res.ModuleBytes, firstModuleBytes) {
				t.Fatalf("%s: module bytes differ on iteration %d (nondeterministic codegen)", name, i)
			}
			if res.ModuleHash != firstModuleHash {
				t.Fatalf("%s: module hash differs on iteration %d", name, i)
			}
			if res.ManifestHash != firstManifestHash {
				t.Fatalf("%s: manifest hash differs on iteration %d", name, i)
			}
			if res.StateInitHash != firstStateInitHash {
				t.Fatalf("%s: state init hash differs on iteration %d", name, i)
			}
			if res.DependencyLock.LockHash != firstLockHash {
				t.Fatalf("%s: dependency lock hash differs on iteration %d", name, i)
			}
			if res.SelectorRegistry.RegistryHash != firstRegistryHash {
				t.Fatalf("%s: selector registry hash differs on iteration %d", name, i)
			}
		}
	}

	t.Run("embedded counter source", func(t *testing.T) {
		assertStable(t, "counter", []NamedSource{{Name: "counter.atlx", Data: []byte(counterSource)}}, DefaultOptions())
	})

	t.Run("token wallet and master example", func(t *testing.T) {
		root := filepath.Join("..", "..", "..", "examples", "avm", "token")
		walletData, err := os.ReadFile(filepath.Join(root, "token_wallet.atlx"))
		if err != nil {
			t.Fatalf("read token_wallet.atlx: %v", err)
		}
		sharedData, err := os.ReadFile(filepath.Join(root, "token_shared.atlx"))
		if err != nil {
			t.Fatalf("read token_shared.atlx: %v", err)
		}
		masterData, err := os.ReadFile(filepath.Join(root, "token_master.atlx"))
		if err != nil {
			t.Fatalf("read token_master.atlx: %v", err)
		}
		opts := DefaultOptions()

		assertStable(t, "token_wallet", []NamedSource{
			{Name: "token_wallet.atlx", Data: walletData},
			{Name: "token_shared.atlx", Data: sharedData},
		}, opts)
		assertStable(t, "token_master", []NamedSource{
			{Name: "token_master.atlx", Data: masterData},
			{Name: "token_shared.atlx", Data: sharedData},
		}, opts)
	})
}

func TestCompileRejectsRecursiveAndImpureFunctions(t *testing.T) {
	t.Run("recursive functions", func(t *testing.T) {
		src := `
struct CounterState {
  count: u64 = 0
}

func first() -> u64 {
  return second()
}

func second() -> u64 {
  return first()
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @internal
  func onInternalMessage(in: InMessage) {
    const sanity = first()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
		c, err := New(DefaultOptions())
		require.NoError(t, err)
		_, err = c.Compile([]byte(src))
		require.ErrorContains(t, err, "recursive function call cycle detected")
	})

	t.Run("pure state writes", func(t *testing.T) {
		src := `
struct CounterState {
  count: u64 = 0
}

@pure func bump() -> u64 {
  set state.count = 1
  return 0
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @internal
  func onInternalMessage(in: InMessage) {
    const sanity = bump()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
		c, err := New(DefaultOptions())
		require.NoError(t, err)
		_, err = c.Compile([]byte(src))
		require.ErrorContains(t, err, "function \"bump\" is annotated @pure but has side effects")
	})

	t.Run("pure effects", func(t *testing.T) {
		src := `
struct CounterState {
  count: u64 = 0
}

@pure func bump() -> u64 {
  send 0 to "AEreceiver" opcode = 77;
  return 0
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @internal
  func onInternalMessage(in: InMessage) {
    const sanity = bump()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
		c, err := New(DefaultOptions())
		require.NoError(t, err)
		_, err = c.Compile([]byte(src))
		require.ErrorContains(t, err, "function \"bump\" is annotated @pure but has side effects")
	})
}

func TestCompileSeparatesTopLevelFunctionsFromContractMembers(t *testing.T) {
	src := `
@pure func helper(x: u64) -> u64 {
  return x + 1
}

struct DemoState {
  count: u64 = 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
    set state.count = helper(0)
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func count(): u64 {
    const st = DemoState.load()
    return st.count
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Manifest.Methods)
	require.Len(t, res.Manifest.AsyncHandlers, 2)
	require.Len(t, res.Manifest.GetMethods, 1)
}

// TestCompileInlinesHelperWithComparisonAndLogic guards inlining of helper
// functions that use their parameters inside comparison (<, >, ==) and logic
// (&&, ||) expressions — the min/clamp/bounds-check helpers every framework or
// DEX author writes. Before the inliner substituted into compare/logic nodes,
// such a parameter lowered to an unbound identifier and the call failed.
func TestCompileInlinesHelperWithComparisonAndLogic(t *testing.T) {
	src := `
@pure func pickMin(a: u64, b: u64) -> u64 {
  return a < b ? a : b
}

@pure func inRange(x: u64, lo: u64, hi: u64) -> bool {
  return x >= lo && x <= hi
}

struct DemoState {
  count: u64 = 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal
  func onInternalMessage(in: InMessage) {
    set state.count = pickMin(0, 7)
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func clamped(): u64 {
    const st = DemoState.load()
    return pickMin(st.count, 100)
  }

  @get
  func bounded(): bool {
    const st = DemoState.load()
    return inRange(st.count, 1, 50)
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Manifest.GetMethods, 2)
}

func TestCompileCapturesAnnotatedStorageAndMessageSchemas(t *testing.T) {
	src := `
@storage struct DemoState {
  root: Chunk
  cache: lazy Chunk<Chunk>?
}

@message(0x1001) struct Ping {
  ticket: u64
}

contract Demo {
  storage: DemoState
  incomingMessages: Ping

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Contains(t, res.MessageBodies, "Ping")
	require.Equal(t, uint32(0x1001), res.MessageBodyOpcodes["Ping"])
	require.Equal(t, "DemoState", res.StorageLayout.Name)
	require.True(t, res.StorageLayout.Fields[1].Lazy)
	require.True(t, res.StorageCodec.Fields[1].Lazy)
}

func TestCompileBuildsMessageUnionsAndExhaustiveMatch(t *testing.T) {
	src := `
@message(0x1001) struct Inc {
  by: u32
}

@message(0x1002) struct Dec {
  by: u32
}

type InternalMsg = Inc | Dec

struct CounterState {
  count: u64 = 0
}

func handle(msg: InternalMsg) -> u64 {
  match msg {
    Inc(by) {
      return by
    }
    Dec(by) {
      return by + 1
    }
  }
}

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	union, ok := res.MessageUnions["InternalMsg"]
	require.True(t, ok)
	require.Len(t, union.Variants, 2)
	require.Equal(t, "Inc", union.Variants[0].Name)
	require.Equal(t, uint32(0x1001), union.Variants[0].Opcode)
	require.Equal(t, "Dec", union.Variants[1].Name)
	require.Equal(t, uint32(0x1002), union.Variants[1].Opcode)
}

func TestCompileRejectsIncompleteUnionMatch(t *testing.T) {
	src := `
@message(0x1001) struct Inc {
  by: u32
}

@message(0x1002) struct Dec {
  by: u32
}

type InternalMsg = Inc | Dec

struct CounterState {
  count: u64 = 0
}

func handle(msg: InternalMsg) -> u64 {
  match msg {
    Inc(by) {
      return by
    }
  }
}

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err)
	require.ErrorContains(t, err, "missing variant Dec")
}

func TestCompileDiagnosticsAreStableAcrossRepeatedRuns(t *testing.T) {
	bad := `
struct CounterState {
  count: u64 = "bad"
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	_, err = c.Compile([]byte(bad))
	if err == nil {
		t.Fatal("expected compile failure")
	}
	firstErr := err.Error()
	_, err = c.Compile([]byte(bad))
	if err == nil {
		t.Fatal("expected compile failure")
	}
	secondErr := err.Error()
	if firstErr != secondErr {
		t.Fatalf("compile diagnostics are not stable: %q vs %q", firstErr, secondErr)
	}
}

func TestCompileFilesResolvesImportedSources(t *testing.T) {
	root := `
package app.counter
import "lib/math@1.0.0"

struct CounterState {
  count: u64 = 0
}

func bump(x: u64) -> u64 {
  return addOne(x)
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @internal
  func onInternalMessage(in: InMessage) {
    set state.count = addOne(1)
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    return addOne(1)
  }
}
`
	libMath := `
package lib.math
import "lib/core@1.0.0"

func addOne(x: u64) -> u64 {
  return bumpCore(x)
}
`
	libCore := `
package lib.core

func bumpCore(x: u64) -> u64 {
  return x + 1
}
`
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	resolver := testResolver{
		files: map[string]NamedSource{
			"lib/math@1.0.0": {Name: "math.atlx", Data: []byte(libMath)},
			"lib/core@1.0.0": {Name: "core.atlx", Data: []byte(libCore)},
		},
	}
	merged, err := parsePackageSources([]NamedSource{{Name: "root.atlx", Data: []byte(root)}}, resolver)
	if err != nil {
		t.Fatalf("parse package sources: %v", err)
	}
	foundAddOne := false
	for _, fn := range merged.Functions {
		if fn != nil && fn.Name == "addOne" {
			foundAddOne = true
			break
		}
	}
	if !foundAddOne {
		t.Fatalf("imported function not merged into package: imports=%+v functions=%+v", merged.Imports, merged.Functions)
	}
	c.opts.Resolver = resolver
	res, err := c.CompileFiles([]NamedSource{{Name: "root.atlx", Data: []byte(root)}})
	if err != nil {
		t.Fatalf("compile with imports: %v", err)
	}
	if len(res.DependencyLock.Entries) < 3 {
		t.Fatalf("expected transitive dependency lock entries, got %d", len(res.DependencyLock.Entries))
	}
	if res.DependencyLock.Entries[0].Path != "avm/stdlib" {
		t.Fatalf("stdlib dependency missing first: %+v", res.DependencyLock.Entries[0])
	}
}

func TestCompileFileResolvesProjectLocalImports(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.atlx")
	constantsPath := filepath.Join(dir, "constants.atlx")
	helpersPath := filepath.Join(dir, "helpers.atlx")
	structsDir := filepath.Join(dir, "structs")
	messagesPath := filepath.Join(structsDir, "messages.atlx")

	require.NoError(t, os.MkdirAll(structsDir, 0o755))

	require.NoError(t, os.WriteFile(constantsPath, []byte(`
package app.counter

const ERR_BAD_NONCE = 1002
const COUNTER_STEP = 3

`), 0o644))

	require.NoError(t, os.WriteFile(helpersPath, []byte(`
package app.counter

func bumpOne(x: uint64) -> uint64 {
  return x + 1
}
`), 0o644))

	require.NoError(t, os.WriteFile(messagesPath, []byte(`
package app.counter
import "constants.atlx"
import "helpers.atlx"

@message(0x2001)
struct Touch {
  nonce: uint32
}

func stepCount(value: uint64) -> uint64 {
  return bumpOne(value) + COUNTER_STEP - 1
}

type InternalMsg = Touch
`), 0o644))

	require.NoError(t, os.WriteFile(mainPath, []byte(`
package app.counter
import "structs/messages.atlx"

struct CounterState {
  count: uint64 = 0
}

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy Touch.fromSegment(in.body)
    assert (msg.nonce != 0) throw ERR_BAD_NONCE

    var st = lazy CounterState.fromChunk(contract.getData())
    st.count = stepCount(st.count)
    contract.setData(st.toChunk())
  }
}
`), 0o644))

	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.CompileFile(mainPath)
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())
	require.NotEmpty(t, res.MessageBodies)
	require.NotEmpty(t, res.DependencyLock.Entries)

	foundLocalImport := false
	for _, dep := range res.DependencyLock.Entries {
		if dep.Path == "constants.atlx" || dep.Path == "helpers.atlx" || dep.Path == filepath.ToSlash(filepath.Join("structs", "messages.atlx")) {
			foundLocalImport = true
		}
	}
	require.True(t, foundLocalImport, "expected local file imports in dependency lock: %+v", res.DependencyLock.Entries)

	res2, err := c.CompileFile(mainPath)
	require.NoError(t, err)
	require.Equal(t, res.DependencyLock.LockHash, res2.DependencyLock.LockHash)

	require.NoError(t, os.WriteFile(helpersPath, []byte(`
package app.counter

func bumpOne(x: uint64) -> uint64 {
  return x + 2
}
`), 0o644))

	res3, err := c.CompileFile(mainPath)
	require.NoError(t, err)
	require.NotEqual(t, res.DependencyLock.LockHash, res3.DependencyLock.LockHash)
}

func TestCompileReportsFileSpans(t *testing.T) {
	src := `
package demo

struct CounterState {
  count: u64 = "bad"
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, _ := New(DefaultOptions())
	_, err := c.CompileFiles([]NamedSource{{Name: "bad.atlx", Data: []byte(src)}})
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), "bad.atlx") {
		t.Fatalf("expected file span in error, got %v", err)
	}
}

func TestGetterRejectsStateWrites(t *testing.T) {
	src := `
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage: CounterState
  incomingMessages: CounterState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    set state.count = 1
    return state.count
  }
}
`
	c, _ := New(DefaultOptions())
	_, err := c.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected getter mutation error")
	}
	if !strings.Contains(err.Error(), "write state") {
		t.Fatalf("expected getter mutation error, got %v", err)
	}
}

func TestCompileRichSemantics(t *testing.T) {
	c, _ := New(DefaultOptions())
	res, err := c.Compile([]byte(richSemanticsSource))
	if err != nil {
		t.Fatalf("compile rich semantics: %v", err)
	}
	// The source-level migrate surface was removed from ATLX; EntryMigrate is
	// runtime-only and no longer exported by source-compiled modules.
	if _, ok := res.Module.Exports[avm.EntryMigrate]; ok {
		t.Fatal("source-compiled module must not export migrate entrypoint")
	}
	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	storage := avm.Storage{"count": avm.EncodeU64(0)}
	ctx := avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	}
	exec, err := runner.Run(res.Module, storage, ctx)
	if err != nil {
		t.Fatalf("run rich semantics: %v", err)
	}
	if got := avm.DecodeU64(exec.State["count"]); got != 6 {
		t.Fatalf("expected count 6, got %d", got)
	}
	if len(exec.Outgoing) != 0 {
		t.Fatalf("expected no outgoing messages, got %d", len(exec.Outgoing))
	}

	bouncedCtx := avm.RuntimeContext{
		Entry:    avm.EntryReceiveBounced,
		Message:  async.MessageEnvelope{Opcode: 12, QueryID: 2, GasLimit: 100_000, Bounced: true},
		GasLimit: 100_000,
	}
	bouncedExec, err := runner.Run(res.Module, exec.State, bouncedCtx)
	if err != nil {
		t.Fatalf("run bounced handler: %v", err)
	}
	if got := avm.DecodeU64(bouncedExec.State["count"]); got != 9 {
		t.Fatalf("expected bounced count 9, got %d", got)
	}
	if len(bouncedExec.Outgoing) != 0 {
		t.Fatalf("bounced handler should not emit outgoing messages, got %d", len(bouncedExec.Outgoing))
	}

	getExec, err := runner.Run(res.Module, bouncedExec.State, avm.RuntimeContext{Entry: avm.EntryQuery, Message: async.MessageEnvelope{Opcode: getterSelector(t, res, "getCount"), QueryID: 2, GasLimit: 100_000}, GasLimit: 100_000})
	if err != nil {
		t.Fatalf("run getter: %v", err)
	}
	if got, err := getExec.ReturnValue.AsUint64(); err != nil || got != 9 {
		t.Fatalf("expected getter return 9, got %v (err=%v)", got, err)
	}
}

func TestCompileSendLowering(t *testing.T) {
	src := `
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage: CounterState
  incomingExternal: CounterState

  @external
  func onExternalMessage(inMsg: Segment) {
    send 0 to "AEreceiver" opcode = 77;
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, _ := New(DefaultOptions())
	res, err := c.Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile send lowering: %v", err)
	}
	if !hasOpcode(res.Module.Code, avm.OpEmitInternal) {
		t.Fatal("send lowering did not emit OpEmitInternal")
	}
	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	exec, err := runner.Run(res.Module, avm.Storage{"count": avm.EncodeU64(0)}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveExternal,
		Message:         async.MessageEnvelope{Opcode: 11, QueryID: 9, GasLimit: 100_000},
		GasLimit:        100_000,
		EmitDestination: sdk.AccAddress([]byte("AEreceiver")),
	})
	if err != nil {
		t.Fatalf("run send lowering: %v", err)
	}
	if len(exec.Outgoing) != 1 {
		t.Fatalf("expected one outgoing message, got %d", len(exec.Outgoing))
	}
	if exec.Outgoing[0].Opcode != 77 {
		t.Fatalf("expected outgoing opcode 77, got %d", exec.Outgoing[0].Opcode)
	}
}

func TestMessageEnvelopeAliasesAndBuildMessageFields(t *testing.T) {
	src := `
const ERR_BAD_MSG = 0xFFFF

@storage
struct EnvelopeState {
  sender: address
  value: coins
  opcode: uint32
  queryId: uint64
  logicalTime: uint64
  attachedValue: coins
  bodyHash: hash32
}

@store
func EnvelopeState.load() {
  return EnvelopeState.fromChunk(contract.getData())
}

@store
func EnvelopeState.save(self) {
  contract.setData(self.toChunk())
}

@message(0x7101)
struct Ping {
  amount: uint64
}

contract EnvelopeDemo {
  storage: EnvelopeState
  incomingMessages: Ping

  @internal
  func onInternalMessage(in: InMessage) {
    var st = lazy EnvelopeState.load()
    st.sender = in.sender
    st.value = in.value
    st.opcode = in.opcode
    st.queryId = in.queryId
    st.logicalTime = in.logicalTime
    st.attachedValue = in.attachedValue
    st.bodyHash = hash(in.body)
    st.save()
  }

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = buildMessage({
      bounce: false,
      amount: 0,
      receiver: getAddress(),
      opcode: 77,
      queryId: 123,
      body: Ping {
        amount: 1,
      },
    })
    msg.send()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	internalBody := []byte("envelope body")
	internalSender := sdk.AccAddress(bytes.Repeat([]byte{0x41}, 20))
	internalState := avm.Storage{}
	internalExec, err := runner.Run(res.Module, internalState, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20)),
		Message: async.MessageEnvelope{
			Source:      internalSender,
			Destination: sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20)),
			Value:       sdk.NewCoin("naet", sdkmath.NewInt(1234)),
			Opcode:      91,
			QueryID:     92,
			Body:        internalBody,
			GasLimit:    100_000,
			ForwardFee:  sdk.NewCoin("naet", sdkmath.ZeroInt()),
		},
		LogicalTime:   777,
		AttachedValue: sdkmath.NewInt(3456),
		GasLimit:      100_000,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), internalExec.ResultCode)
	require.NoError(t, err)

	require.Equal(t, mustCanonicalEncode(t, avm.ValueAddress(addressing.FormatAccAddress(internalSender))), internalExec.State["sender"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueCoins(big.NewInt(1234))), internalExec.State["value"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueUint64(91)), internalExec.State["opcode"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueUint64(92)), internalExec.State["queryId"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueTimestamp(777)), internalExec.State["logicalTime"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueCoins(big.NewInt(3456))), internalExec.State["attachedValue"])
	require.Len(t, internalExec.State["bodyHash"], 33)
	require.NotEqual(t, make([]byte, len(internalExec.State["bodyHash"])), internalExec.State["bodyHash"])

	externalExec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveExternal,
		Message:         async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 100_000},
		GasLimit:        100_000,
		ContractAddress: sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20)),
	})
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), externalExec.ResultCode)
	require.Len(t, externalExec.Outgoing, 1)
	require.Equal(t, uint32(77), externalExec.Outgoing[0].Opcode)
	require.Equal(t, uint64(123), externalExec.Outgoing[0].QueryID)
	require.False(t, externalExec.Outgoing[0].Bounce)
}

func TestMethodSendRejectsArguments(t *testing.T) {
	src := `
@storage
struct Storage {}

@message(0x7201)
struct Ping {
  amount: uint64
}

type InternalMsg = Ping
type ExternalMsg = Ping

contract EnvelopeDemo {
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
  }

  @external
  func onExternalMessage(inMsg: Segment) {
    const out = buildMessage({
      receiver: getAddress(),
      amount: 0,
      body: Ping {
        amount: 1,
      },
    })
    out.send(SEND_BOUNCE_ON_FAIL)
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err)
	require.ErrorContains(t, err, ".send() takes no arguments")
}

func mustCanonicalEncode(t *testing.T, value avm.RuntimeValue) []byte {
	t.Helper()
	bz, err := avm.CanonicalEncode(value)
	require.NoError(t, err)
	return bz
}

func TestCompileAndRuntimeReceiptsAreStableAcrossRepeatedRuns(t *testing.T) {
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	first, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("compile first: %v", err)
	}
	second, err := c.Compile([]byte(counterSource))
	if err != nil {
		t.Fatalf("compile second: %v", err)
	}
	if first.ModuleHash != second.ModuleHash {
		t.Fatal("module hash changed across identical builds")
	}
	if first.ManifestHash != second.ManifestHash {
		t.Fatal("manifest hash changed across identical builds")
	}
	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	ctx := avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 9, Body: counterIncrementBody(t, first, 42), GasLimit: 100_000},
		GasLimit: 100_000,
	}
	storage := avm.Storage{"count": avm.EncodeU64(41)}
	exec1, err := runner.Run(first.Module, storage, ctx)
	if err != nil {
		t.Fatalf("run first: %v", err)
	}
	exec2, err := runner.Run(second.Module, storage, ctx)
	if err != nil {
		t.Fatalf("run second: %v", err)
	}
	if !reflect.DeepEqual(exec1, exec2) {
		t.Fatalf("runtime receipts changed across identical inputs: %#v vs %#v", exec1, exec2)
	}
}

func TestCompileExtendedExpressionsAndUint64Alias(t *testing.T) {
	src := `
struct DemoState {
  value: uint64 = 0
}

contract Demo {
  storage: DemoState
  incomingExternal: DemoState

  @external
  func onExternalMessage(inMsg: Segment) {
    var x = 6
    var y = 3
    set state.value = (!false ? x * y : x / y) + (x % y) + (x << 1) + (x >> 1) + (x & y) + (x | y) + (x ^ y) + (null ?? 7)
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getValue(): uint64 {
    const st = DemoState.load()
    return st.value
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, hasOpcode(res.Module.Code, avm.OpMul))
	require.True(t, hasOpcode(res.Module.Code, avm.OpDiv))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMod))
	require.True(t, hasOpcode(res.Module.Code, avm.OpShl))
	require.True(t, hasOpcode(res.Module.Code, avm.OpShr))
	require.True(t, hasOpcode(res.Module.Code, avm.OpBitAnd))
	require.True(t, hasOpcode(res.Module.Code, avm.OpBitOr))
	require.True(t, hasOpcode(res.Module.Code, avm.OpBitXor))
	require.True(t, hasOpcode(res.Module.Code, avm.OpNot))
	require.True(t, hasOpcode(res.Module.Code, avm.OpPushNull))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(54), avm.DecodeU64(exec.State["value"]))

	getter, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "getValue"), QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	got, err := getter.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(54), got)
}

func TestCompileLoopSurfaceAndCompoundAssignments(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
  repeatCount: uint64 = 3
}

contract Demo {
  storage: DemoState
  incomingExternal: DemoState

  @external
  func onExternalMessage(inMsg: Segment) {
    var st = lazy DemoState.load()
    while st.count <= 2 {
      st.count += 1
    }

    do {
      st.count += 1
    } while st.count < 6

    repeat st.repeatCount {
      st.count += 2
    }

    st.save()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): uint64 {
    const st = DemoState.load()
    return st.count
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpJump))
	require.True(t, hasOpcode(res.Module.Code, avm.OpJumpIfZero))
	require.True(t, hasOpcode(res.Module.Code, avm.OpDup))
	require.True(t, hasOpcode(res.Module.Code, avm.OpDrop))
	require.True(t, hasOpcode(res.Module.Code, avm.OpAdd))
	require.True(t, hasOpcode(res.Module.Code, avm.OpSub))
	require.True(t, hasOpcode(res.Module.Code, avm.OpLe))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{
		"count":       avm.EncodeU64(0),
		"repeatCount": avm.EncodeU64(3),
	}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(12), avm.DecodeU64(exec.State["count"]))
}

func TestCompileMapSurfaceAndRuntime(t *testing.T) {
	src := `
struct DemoState {
  total: uint64 = 0
}

contract Demo {
  storage: DemoState
  incomingExternal: DemoState

  @external
  func onExternalMessage(inMsg: Segment) {
    var m = Map.empty()
    m = m.set(getAddress(), getAddress())
    assert (m.has(getAddress())) throw 700
    const owner = m.get(getAddress())
    assert (owner != null) throw 701
    const keys = m.keys(10)
    const entries = m.entries(10)
    m = m.delete(getAddress())
    assert (!m.has(getAddress())) throw 702
    set state.total = keys.len() + entries.len() + m.len()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func total(): uint64 {
    const st = DemoState.load()
    return st.total
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapEmpty))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapSet))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapGet))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapHas))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapDelete))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapKeys))
	require.True(t, hasOpcode(res.Module.Code, avm.OpMapEntries))
	require.True(t, hasOpcode(res.Module.Code, avm.OpLen))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(2), avm.DecodeU64(exec.State["total"]))

	getter, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "total"), QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	got, err := getter.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(2), got)
}

func TestCompileMutableLocalsAndLoopControl(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
}

contract Demo {
  storage: DemoState
  incomingExternal: DemoState

  @external
  func onExternalMessage(inMsg: Segment) {
    var i = 0
    var total = 0
    while i < 8 {
      i += 1
      if i == 2 {
        continue
      }
      if i == 6 {
        break
      }
      if true {
        const shadow = i
        total += shadow
      }
      const shadow = 10
      total += shadow
    }
    set state.count = total
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): uint64 {
    const st = DemoState.load()
    return st.count
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpJump))
	require.True(t, hasOpcode(res.Module.Code, avm.OpJumpIfZero))
	require.True(t, hasOpcode(res.Module.Code, avm.OpLoadLocal))
	require.True(t, hasOpcode(res.Module.Code, avm.OpStoreLocal))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(53), avm.DecodeU64(exec.State["count"]))

	getter, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "getCount"), QueryID: 2, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	got, err := getter.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(53), got)
}

func TestCompileSpaceshipComparisonRuns(t *testing.T) {
	src := `
struct DemoState {
  marker: uint64 = 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func compare(): i64 {
    return 1 <=> 2
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpCmp))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	getter, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "compare"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	got, err := getter.ReturnValue.AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(-1), got)
}

func TestRuntimeCoreLanguageAcceptance(t *testing.T) {
	src := `
const ERR_BAD_MSG = 0xFFFF

@message(0x4101)
struct FlowRun {
  outer: uint32
  inner: uint32
  repeatCount: uint32
  mode: uint32
}

@message(0x4102)
struct Other {
}

type ExternalMsg = FlowRun | Other

@storage
struct FlowState {
  total: uint64 = 0
  branch: uint32 = 0
  matched: uint64 = 0
}

contract FlowDemo {
  storage: FlowState
  incomingExternal: ExternalMsg
  namespace "flow"
  chain "avm-local"

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy ExternalMsg.fromSegment(inMsg)

    match (msg) {
      FlowRun(outer, inner, repeatCount, mode) => {
        var st = lazy FlowState.load()
        var total = st.total
        var i = 0

        while i < outer {
          i += 1
          if i == 2 {
            continue
          } else if i == 5 {
            break
          } else {
            var hits = 0
            repeat inner {
              hits += 1
              total += i
            }
            assert (hits == inner) throw ERR_BAD_MSG
          }
        }

        do {
          total += 2
        } while false

        repeat repeatCount {
          total += 1
        }

        if mode == 0 {
          st.branch = 10
        } else if mode == 1 {
          st.branch = 20
        } else {
          st.branch = 30
        }

        st.total = total
        st.matched = outer + inner + repeatCount
      }
      else => {
        var st = lazy FlowState.load()
        st.branch = 99
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())
	require.Contains(t, res.Module.Exports, avm.EntryReceiveExternal)
	require.True(t, hasOpcode(res.Module.Code, avm.OpLoadLocal))
	require.True(t, hasOpcode(res.Module.Code, avm.OpStoreLocal))
	require.True(t, hasOpcode(res.Module.Code, avm.OpJump))
	require.True(t, hasOpcode(res.Module.Code, avm.OpJumpIfZero))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	runFlow := func(mode uint32) avm.Storage {
		t.Helper()
		bodyCodec, ok := res.MessageBodies["FlowRun"]
		require.True(t, ok)
		body, err := bodyCodec.Encode(map[string]any{
			"outer":       uint32(6),
			"inner":       uint32(3),
			"repeatCount": uint32(4),
			"mode":        mode,
		})
		require.NoError(t, err)

		exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
			Entry: avm.EntryReceiveExternal,
			Message: async.MessageEnvelope{
				Source:   sdk.AccAddress(bytes.Repeat([]byte{0x21}, 20)),
				Body:     body,
				Opcode:   0x4101,
				QueryID:  1,
				GasLimit: 100_000,
			},
			GasLimit: 100_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		require.Equal(t, uint64(30), avm.DecodeU64(exec.State["total"]))
		require.Equal(t, uint64(13), avm.DecodeU64(exec.State["matched"]))
		return exec.State
	}

	state0 := runFlow(0)
	require.Equal(t, uint64(10), avm.DecodeU64(state0["branch"]))

	state1 := runFlow(1)
	require.Equal(t, uint64(20), avm.DecodeU64(state1["branch"]))

	state2 := runFlow(2)
	require.Equal(t, uint64(30), avm.DecodeU64(state2["branch"]))

	otherCodec, ok := res.MessageBodies["Other"]
	require.True(t, ok)
	otherBody, err := otherCodec.Encode(nil)
	require.NoError(t, err)

	otherExec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry: avm.EntryReceiveExternal,
		Message: async.MessageEnvelope{
			Source:   sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20)),
			Body:     otherBody,
			Opcode:   0x4102,
			QueryID:  2,
			GasLimit: 100_000,
		},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, otherExec.ResultCode)
	require.Equal(t, uint64(99), avm.DecodeU64(otherExec.State["branch"]))
	require.Equal(t, uint64(0), avm.DecodeU64(otherExec.State["total"]))
}

func TestStorageSnapshotDeleteAndRoundTripAcceptance(t *testing.T) {
	src := `
const ERR_BAD_MSG = 0xFFFF

@message(0x5201)
struct SnapMsg {
  mode: uint32
  value: uint64
  note: uint64
}

type ExternalMsg = SnapMsg

@storage
struct SnapState {
  counter: uint64 = 0
  note: uint64 = 0
}

@store
func SnapState.load() {
  return SnapState.fromChunk(contract.getData())
}

@store
func SnapState.save(self) {
  contract.setData(self.toChunk())
}

contract SnapshotDemo {
  storage: SnapState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy ExternalMsg.fromSegment(inMsg)
    var st = lazy SnapState.load()

    if msg.mode == 0 {
      st.counter = msg.value
      st.note = msg.note
      st.save()
    } else if msg.mode == 1 {
      const snap = contract.getData()
      contract.deleteData()
      contract.setData(snap)
    } else if msg.mode == 2 {
      contract.deleteData()
    } else {
      throw ERR_BAD_MSG
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpReadStorage))
	require.True(t, hasOpcode(res.Module.Code, avm.OpWriteStorage))
	require.True(t, hasOpcode(res.Module.Code, avm.OpDeleteStorage))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	bodyCodec, ok := res.MessageBodies["SnapMsg"]
	require.True(t, ok)

	encodeMsg := func(mode uint32, value uint64, note uint64) []byte {
		t.Helper()
		body, err := bodyCodec.Encode(map[string]any{
			"mode":  mode,
			"value": value,
			"note":  note,
		})
		require.NoError(t, err)
		return body
	}

	run := func(storage avm.Storage, mode uint32, value uint64, note uint64) avm.Execution {
		t.Helper()
		exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
			Entry: avm.EntryReceiveExternal,
			Message: async.MessageEnvelope{
				Source:   sdk.AccAddress(bytes.Repeat([]byte{0x31}, 20)),
				Body:     encodeMsg(mode, value, note),
				Opcode:   0x5201,
				QueryID:  1,
				GasLimit: 100_000,
			},
			GasLimit: 100_000,
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		return exec
	}

	written := run(avm.Storage{}, 0, 7, 11)
	require.Equal(t, uint64(7), avm.DecodeU64(written.State["counter"]))
	require.Equal(t, uint64(11), avm.DecodeU64(written.State["note"]))

	roundTripped := run(written.State, 1, 0, 0)
	require.Equal(t, written.State, roundTripped.State)

	deleted := run(written.State, 2, 0, 0)
	require.Empty(t, deleted.State)
}

func TestStorageHelpersAreTypeNameAgnostic(t *testing.T) {
	src := `
const ERR_BAD_MSG = 0xFFFF

@message(0x5301)
struct SetBalance {
  value: uint64
}

type ExternalMsg = SetBalance

@storage
struct WalletState {
  balance: uint64 = 0
}

@store
func WalletState.load() {
  return WalletState.fromChunk(contract.getData())
}

@store
func WalletState.save(self) {
  contract.setData(self.toChunk())
}

contract WalletDemo {
  storage: WalletState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy ExternalMsg.fromSegment(inMsg)
    var st = lazy WalletState.load()
    st.balance = msg.value
    st.save()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.Equal(t, "WalletState", res.StorageLayout.Name)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	bodyCodec, ok := res.MessageBodies["SetBalance"]
	require.True(t, ok)

	body, err := bodyCodec.Encode(map[string]any{
		"value": uint64(9),
	})
	require.NoError(t, err)

	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry: avm.EntryReceiveExternal,
		Message: async.MessageEnvelope{
			Source:   sdk.AccAddress(bytes.Repeat([]byte{0x31}, 20)),
			Body:     body,
			Opcode:   0x5301,
			QueryID:  1,
			GasLimit: 100_000,
		},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(9), avm.DecodeU64(exec.State["balance"]))
}

func TestCompileRejectsLoopControlOutsideLoop(t *testing.T) {
	src := `
struct DemoState {
  count: uint64 = 0
}

contract Demo {
  storage: DemoState
  incomingExternal: DemoState

  @external
  func onExternalMessage(inMsg: Segment) {
    break
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(src))
	require.Error(t, err)
	require.Contains(t, err.Error(), "only allowed inside a loop")
}

func TestCompileCanonicalExamplesCoverAllEntrypointKinds(t *testing.T) {
	examples, err := filepath.Glob("../../../examples/avm/*.atlx")
	if err != nil {
		t.Fatalf("glob examples: %v", err)
	}
	if len(examples) == 0 {
		t.Fatal("expected canonical AVM examples")
	}
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	covered := map[avm.Entrypoint]bool{}
	for _, path := range examples {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read example %s: %v", path, err)
		}
		res, err := c.Compile(src)
		if err != nil {
			t.Fatalf("compile example %s: %v", path, err)
		}
		if err := res.Manifest.Validate(); err != nil {
			t.Fatalf("validate example %s: %v", path, err)
		}
		for entry := range res.Module.Exports {
			covered[entry] = true
		}
	}
	required := []avm.Entrypoint{
		avm.EntryReceiveExternal,
		avm.EntryReceiveInternal,
		avm.EntryReceiveBounced,
		avm.EntryQuery,
	}
	for _, entry := range required {
		if !covered[entry] {
			t.Fatalf("canonical examples do not cover entrypoint %v", entry)
		}
	}
}

func TestTokenExamplesCompileAndMasterEmbedsWalletBytecode(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	walletPath := filepath.Clean("../../../examples/avm/token/token_wallet.atlx")
	walletResult, err := c.CompileFile(walletPath)
	require.NoError(t, err)
	require.NoError(t, walletResult.Manifest.Validate())

	walletHex := hex.EncodeToString(walletResult.ModuleBytes)
	require.NotEmpty(t, walletHex)

	masterPath := filepath.Clean("../../../examples/avm/token/token_master.atlx")
	masterResult, err := c.CompileFile(masterPath)
	require.NoError(t, err)
	require.NoError(t, masterResult.Manifest.Validate())

	initData, err := masterResult.StorageCodec.Encode(map[string]any{
		"owner":           masterResult.StateInit.DeployerAddress,
		"pendingOwner":    nil,
		"totalSupply":     uint64(0),
		"tokenWalletCode": walletResult.CodeChunk,
		"metadata":        nil,
	})
	require.NoError(t, err)
	require.Contains(t, string(initData), walletHex)
	require.Contains(t, string(initData), "\"chunks\"")
	require.Contains(t, string(initData), "\"base64\"")
	require.Contains(t, string(initData), "\"hash\"")

	var decoded map[string]any
	require.NoError(t, masterResult.StorageCodec.Decode(initData, &decoded))
	codeSnapshot, ok := decoded["tokenWalletCode"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, walletHex, codeSnapshot["hex"])
}

func TestCodeConstructorsCompileAndEncodeDefaults(t *testing.T) {
	src := `
@storage
struct DemoState {
  codeA: Code = Code.fromHex("4142564d")
  codeB: Code = Code.fromBase64("QUJWTQ==")
  codeC: Code = Code.fromChunk(Chunk.fromHex("4142564d"))
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @get
  func sample(value: uint64): uint64 {
    return value
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	defaults, err := res.StorageCodec.EncodeDefaults()
	require.NoError(t, err)
	require.Contains(t, string(defaults), "\"hex\":\"4142564d\"")
	require.Contains(t, string(defaults), "\"base64\":\"QUJWTQ==\"")
	require.Contains(t, string(defaults), "\"chunks\"")
	require.Contains(t, string(defaults), "\"hash\"")
}

func TestCodeHashAndToChunkConstFold(t *testing.T) {
	src := `
const walletCode = Code.fromHex("4142564d")
const walletHash = walletCode.hash()
const walletChunk = walletCode.toChunk()

@storage
struct DemoState {
  code: Code
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @get
  func sample(): uint64 {
    return 0
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(src))
	require.NoError(t, err)
}

func TestAddressLiteralAndSegmentBitsHashCompile(t *testing.T) {
	addrText := addressing.FormatAccAddress(sdk.AccAddress(bytes.Repeat([]byte{0x51}, 20)))
	src := fmt.Sprintf(`
struct DemoState {}

const OWNER = address("%s")

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @get
  func owner(): Address {
    return OWNER
  }

  @external
  func onExternalMessage(inMsg: Segment) {
    var digest = inMsg.bitsHash()
    assert (digest != hash(inMsg)) throw 1
  }
}
`, addrText)
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpPushAddress))
	require.True(t, hasOpcode(res.Module.Code, avm.OpHash))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	getter, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "owner"), QueryID: 1, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	gotAddr, err := getter.ReturnValue.AsAddress()
	require.NoError(t, err)
	require.Equal(t, addrText, gotAddr)
}

func TestExternalRuntimePathAndChildDeploymentAcceptance(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	sourceSource := `
const ERR_BAD_NONCE = 1001

@message(0x4001)
struct DeployChild {
  nonce: uint32
}

@storage
struct SourceState {
}

contract Source {
  storage: SourceState
  incomingMessages: DeployChild

  @external
  func onExternalMessage(inMsg: Segment) {
    assert (!inMsg.isEmpty()) throw ERR_BAD_NONCE
  }
}
`

	sourceResult, err := c.Compile([]byte(sourceSource))
	require.NoError(t, err)
	require.NoError(t, avm.VerifyInterface(sourceResult.Module, sourceResult.Manifest))
	sourceState := avm.EncodeSnapshot(nil)

	params := async.DefaultParams()
	params.MaxMessagesPerBlock = 1
	executor, err := async.NewExecutor(params)
	require.NoError(t, err)
	deployer := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	sourceAddr, err := executor.DeployContract(deployer, sourceResult.ModuleHash[:], sourceState, sourceState, sdkmath.NewInt(10_000))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(sourceAddr, runner.AsyncHandler(sourceResult.Module, nil, avm.RuntimeContext{Entry: avm.EntryReceiveExternal})))

	bodyCodec, ok := sourceResult.MessageBodies["DeployChild"]
	require.True(t, ok)
	body, err := bodyCodec.Encode(map[string]any{"nonce": uint32(1)})
	require.NoError(t, err)

	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      sdk.AccAddress(bytes.Repeat([]byte{9}, 20)),
		Destination: sourceAddr,
		Value:       sdk.NewCoin("naet", sdkmath.ZeroInt()),
		Opcode:      0x4001,
		QueryID:     99,
		Body:        body,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin("naet", sdkmath.ZeroInt()),
	}}))

	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equal(t, uint32(async.ResultOK), receipts[0].ResultCode)
}

func TestCodeChunkStateInitAddressDerivationAndChildDeploymentAcceptance(t *testing.T) {
	childSource := `
@storage
struct ChildState {
  count: uint64 = 0
}

contract Child {
  storage: ChildState
  incomingMessages: ChildState

  @internal
  func onInternalMessage(in: InMessage) {
    var st = lazy ChildState.load()
    st.count += 1
    st.save()
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	childResult, err := c.Compile([]byte(childSource))
	require.NoError(t, err)
	require.NoError(t, avm.VerifyInterface(childResult.Module, childResult.Manifest))

	syntheticCode := []byte{0x41, 0x56, 0x4d, 0x31, 0x00, 0x01, 0xf5, 0xf5}
	syntheticCodeHex := hex.EncodeToString(syntheticCode)
	syntheticCodeBase64 := base64.StdEncoding.EncodeToString(syntheticCode)
	syntheticSnapshot := avm.EncodeSnapshot(nil)
	syntheticSnapshotHex := hex.EncodeToString(syntheticSnapshot)

	sourceSource := fmt.Sprintf(`
const ERR_BAD_NONCE = 1001
const ERR_BAD_MSG = 1002

@message(0x4001)
struct DeployChild {
  nonce: uint32
}

@storage
struct SourceState {
}

struct ChildBoot {
}

contract Source {
  storage: SourceState
  incomingMessages: DeployChild

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy DeployChild.fromSegment(inMsg)
    assert (msg.nonce == 1) throw ERR_BAD_NONCE

    var codeHex = Code.fromHex("%s")
    var codeBase64 = Code.fromBase64("%s")
    var codeChunk = Code.fromChunk(Chunk.fromHex("%s"))

    assert (codeHex.hash() == codeBase64.hash()) throw ERR_BAD_MSG
    assert (codeHex.hash() == codeChunk.hash()) throw ERR_BAD_MSG
    assert (codeHex.hash() == hash(codeHex.toChunk())) throw ERR_BAD_MSG

    var expected = counterfactualAddress({
      code: codeHex,
      data: Bytes.fromHex("%s"),
      salt: "child",
      deployer: getAddress(),
      chainId: "avm-local",
      namespace: "demo",
      balance: 0,
    })
    var autod = autoDeployAddress({
      code: codeHex,
      data: Bytes.fromHex("%s"),
      salt: "child",
      deployer: getAddress(),
      chainId: "avm-local",
      namespace: "demo",
      balance: 0,
    })
    assert (expected == autod) throw ERR_BAD_MSG

    var childInit = {
      code: codeHex,
      data: Bytes.fromHex("%s"),
      salt: "child",
      deployer: getAddress(),
      chainId: "avm-local",
      namespace: "demo",
      balance: 0,
    }

    var deploy = buildMessage({
      bounce: false,
      amount: 0,
      receiver: expected,
      stateInit: childInit,
      body: ChildBoot {},
    })
    deploy.send()
  }
}
`, syntheticCodeHex, syntheticCodeBase64, syntheticCodeHex, syntheticSnapshotHex, syntheticSnapshotHex, syntheticSnapshotHex)

	sourceResult, err := c.Compile([]byte(sourceSource))
	require.NoError(t, err)
	require.NoError(t, avm.VerifyInterface(sourceResult.Module, sourceResult.Manifest))
	sourceState := avm.EncodeSnapshot(nil)

	params := async.DefaultParams()
	params.MaxMessagesPerBlock = 1
	executor, err := async.NewExecutor(params)
	require.NoError(t, err)
	deployer := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	sourceAddr, err := executor.DeployContract(deployer, sourceResult.ModuleHash[:], sourceState, sourceState, sdkmath.NewInt(10_000))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(sourceAddr, runner.AsyncHandler(sourceResult.Module, nil, avm.RuntimeContext{Entry: avm.EntryReceiveExternal})))

	bodyCodec, ok := sourceResult.MessageBodies["DeployChild"]
	require.True(t, ok)
	body, err := bodyCodec.Encode(map[string]any{"nonce": uint32(1)})
	require.NoError(t, err)

	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      sdk.AccAddress(bytes.Repeat([]byte{9}, 20)),
		Destination: sourceAddr,
		Value:       sdk.NewCoin("naet", sdkmath.ZeroInt()),
		Opcode:      0x4001,
		QueryID:     99,
		Body:        body,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin("naet", sdkmath.ZeroInt()),
	}}))

	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equal(t, uint32(async.ResultOK), receipts[0].ResultCode)
	require.Len(t, executor.Queue(), 1)

	childQueued := executor.Queue()[0]
	require.NotNil(t, childQueued.Envelope.StateInit)
	require.NotEmpty(t, childQueued.Envelope.Destination)
	derivedChildAddr, _, err := contracttypes.DeriveContractAddressFromStateInit(contracttypes.DefaultContractChainID, contracttypes.DefaultContractNamespace, addressing.FormatAccAddress(sourceAddr), *childQueued.Envelope.StateInit, contracttypes.DefaultParams())
	require.NoError(t, err)
	require.Equal(t, derivedChildAddr, addressing.FormatAccAddress(childQueued.Envelope.Destination))
}

func TestSignatureVerifyAuthScenarioCompilesAndRollsBackOnFailure(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(signatureAuthSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())
	require.NotEmpty(t, res.MessageBodies)

	seed := sha256.Sum256([]byte("aetralis-auth-seed"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)

	payload := []byte("authorize transfer")
	payloadChunk, err := avm.ToChunkPayload(payload, chunk.TypeNormal)
	require.NoError(t, err)
	digest := payloadChunk.Hash()
	signature := ed25519.Sign(privateKey, digest)

	ownerAddr := sdk.AccAddress([]byte("owner-address-123456"))
	contractAddr := sdk.AccAddress([]byte("contract-address-123"))
	ownerText := addressing.FormatAccAddress(ownerAddr)

	encode := func(v avm.RuntimeValue) []byte {
		t.Helper()
		bz, err := avm.CanonicalEncode(v)
		require.NoError(t, err)
		return bz
	}
	storage := avm.Storage{
		"owner":                encode(avm.ValueAddress(ownerText)),
		"publicKey":            encode(avm.ValueBytes([]byte(publicKey))),
		"nonce":                encode(avm.ValueUint32(7)),
		"lastNow":              encode(avm.ValueInt64(0)),
		"lastLogicalTime":      encode(avm.ValueUint64(0)),
		"lastBlockLogicalTime": encode(avm.ValueUint64(0)),
		"lastValue":            encode(avm.ValueCoins(big.NewInt(0))),
	}

	bodyCodec, ok := res.MessageBodies["SignedCall"]
	require.True(t, ok)

	body, err := bodyCodec.Encode(map[string]any{
		"nonce":     uint32(8),
		"payload":   payload,
		"signature": signature,
	})
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	ctx := avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: contractAddr,
		Message: async.MessageEnvelope{
			Source:      ownerAddr,
			Destination: contractAddr,
			Value:       sdk.NewCoin("naet", sdkmath.NewInt(1234)),
			Opcode:      0x2001,
			QueryID:     77,
			Body:        body,
			GasLimit:    100_000,
		},
		BlockTimestamp:          1_700_000_000,
		LogicalTime:             777,
		CurrentBlockLogicalTime: 888,
		GasLimit:                100_000,
	}

	exec, err := runner.Run(res.Module, storage, ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), exec.ResultCode)
	require.Equal(t, storage, exec.State)

	ownerValue, err := avm.CanonicalDecodeExact(exec.State["owner"])
	require.NoError(t, err)
	decodedOwner, err := ownerValue.AsAddress()
	require.NoError(t, err)
	require.Equal(t, ownerText, decodedOwner)

	publicKeyValue, err := avm.CanonicalDecodeExact(exec.State["publicKey"])
	require.NoError(t, err)
	decodedPublicKey, err := publicKeyValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, []byte(publicKey), decodedPublicKey)

	nonceValue, err := avm.CanonicalDecodeExact(exec.State["nonce"])
	require.NoError(t, err)
	nonce, err := nonceValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(7), nonce)

	lastNowValue, err := avm.CanonicalDecodeExact(exec.State["lastNow"])
	require.NoError(t, err)
	lastNow, err := lastNowValue.AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(0), lastNow)

	lastLogicalTimeValue, err := avm.CanonicalDecodeExact(exec.State["lastLogicalTime"])
	require.NoError(t, err)
	lastLogicalTime, err := lastLogicalTimeValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastLogicalTime)

	lastBlockLogicalTimeValue, err := avm.CanonicalDecodeExact(exec.State["lastBlockLogicalTime"])
	require.NoError(t, err)
	lastBlockLogicalTime, err := lastBlockLogicalTimeValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastBlockLogicalTime)

	lastValueValue, err := avm.CanonicalDecodeExact(exec.State["lastValue"])
	require.NoError(t, err)
	lastValue, err := lastValueValue.AsBigInt()
	require.NoError(t, err)
	require.Zero(t, lastValue.Sign())

	badSignature := append([]byte(nil), signature...)
	badSignature[0] ^= 0x80
	badBody, err := bodyCodec.Encode(map[string]any{
		"nonce":     uint32(8),
		"payload":   payload,
		"signature": badSignature,
	})
	require.NoError(t, err)

	badCtx := ctx
	badCtx.Message.Body = badBody
	badExec, err := runner.Run(res.Module, storage, badCtx)
	require.ErrorContains(t, err, "AVM abort with exit code 1003")
	require.Equal(t, uint32(1003), badExec.ResultCode)
	require.Equal(t, storage, badExec.State)
}

func TestMessageContextSurfaceSupportsAuthAndRuntimeFields(t *testing.T) {
	src := `
const ERR_BAD_MSG = 0xF101
const ERR_BAD_NONCE = 0xF102
const ERR_EXPIRED = 0xF103

@message(0x9201)
struct AuthRequest {
  nonce: uint32
  expiry: int64
  payload: bytes
  signature: bytes
}

@storage
struct ContextState {
  owner: address
  publicKey: bytes
  nonce: uint32 = 0
  lastSender: address
  lastValue: coins
  lastBodyHash: hash32
  lastNow: int64
  lastLogicalTime: uint64
  lastBlockLogicalTime: uint64
  lastOriginalBalance: coins
  lastAttachedValue: coins
}

@store
func ContextState.load() {
  return ContextState.fromChunk(contract.getData())
}

@store
func ContextState.save(self) {
  contract.setData(self.toChunk())
}

contract ContextDemo {
  storage: ContextState
  incomingMessages: AuthRequest

  @internal
  func onInternalMessage(in: InMessage) {
    var st = lazy ContextState.load()
    st.lastSender = in.sender
    st.lastValue = in.value
    st.lastBodyHash = hash(in.body)
    st.lastNow = now()
    st.lastLogicalTime = logicalTime()
    st.lastBlockLogicalTime = currentBlockLogicalTime()
    st.lastOriginalBalance = getOriginalBalance()
    st.lastAttachedValue = getAttachedValue()
    st.save()
  }

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy AuthRequest.fromSegment(inMsg)
    var st = lazy ContextState.load()

    assert (msg.nonce == st.nonce + 1) throw ERR_BAD_NONCE
    assert (msg.expiry > 0) throw ERR_EXPIRED
    assert (isSignatureValid(hash(msg.payload), msg.signature, st.publicKey)) throw ERR_BAD_MSG

    st.nonce = msg.nonce
    st.save()
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	seed := sha256.Sum256([]byte("aetralis-context-seed"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)
	payload := []byte("pay 1")
	payloadChunk, err := avm.ToChunkPayload(payload, chunk.TypeNormal)
	require.NoError(t, err)
	signature := ed25519.Sign(privateKey, payloadChunk.Hash())

	encode := func(v avm.RuntimeValue) []byte {
		t.Helper()
		bz, err := avm.CanonicalEncode(v)
		require.NoError(t, err)
		return bz
	}

	contractAddr := sdk.AccAddress(bytes.Repeat([]byte{0x62}, 20))
	ownerAddr := sdk.AccAddress(bytes.Repeat([]byte{0x63}, 20))
	initialState := avm.Storage{
		"owner":                encode(avm.ValueAddress(addressing.FormatAccAddress(ownerAddr))),
		"publicKey":            encode(avm.ValueBytes([]byte(publicKey))),
		"nonce":                encode(avm.ValueUint32(0)),
		"lastSender":           encode(avm.ValueAddress("")),
		"lastValue":            encode(avm.ValueCoins(big.NewInt(0))),
		"lastBodyHash":         encode(avm.ValueHash([32]byte{})),
		"lastNow":              encode(avm.ValueInt64(0)),
		"lastLogicalTime":      encode(avm.ValueUint64(0)),
		"lastBlockLogicalTime": encode(avm.ValueUint64(0)),
		"lastOriginalBalance":  encode(avm.ValueCoins(big.NewInt(0))),
		"lastAttachedValue":    encode(avm.ValueCoins(big.NewInt(0))),
	}

	externalBodyCodec, ok := res.MessageBodies["AuthRequest"]
	require.True(t, ok)
	externalBody, err := externalBodyCodec.Encode(map[string]any{
		"nonce":     uint32(1),
		"expiry":    int64(999),
		"payload":   payload,
		"signature": signature,
	})
	require.NoError(t, err)

	exec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
		Entry:           avm.EntryReceiveExternal,
		ContractAddress: contractAddr,
		Message: async.MessageEnvelope{
			Source:      ownerAddr,
			Destination: contractAddr,
			Value:       sdk.NewCoin("naet", sdkmath.NewInt(0)),
			Opcode:      0x9201,
			QueryID:     17,
			Body:        externalBody,
			GasLimit:    100_000,
		},
		BlockTimestamp:          1_700_000_111,
		LogicalTime:             777,
		CurrentBlockLogicalTime: 888,
		OriginalBalance:         sdkmath.NewInt(42),
		AttachedValue:           sdkmath.NewInt(0),
		GasLimit:                100_000,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), exec.ResultCode)

	badBody, err := externalBodyCodec.Encode(map[string]any{
		"nonce":     uint32(1),
		"expiry":    int64(1),
		"payload":   payload,
		"signature": signature,
	})
	require.NoError(t, err)
	badExec, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:           avm.EntryReceiveExternal,
		ContractAddress: contractAddr,
		Message: async.MessageEnvelope{
			Source:      ownerAddr,
			Destination: contractAddr,
			Value:       sdk.NewCoin("naet", sdkmath.NewInt(0)),
			Opcode:      0x9201,
			QueryID:     18,
			Body:        badBody,
			GasLimit:    100_000,
		},
		BlockTimestamp:          1_700_000_111,
		LogicalTime:             777,
		CurrentBlockLogicalTime: 888,
		OriginalBalance:         sdkmath.NewInt(42),
		AttachedValue:           sdkmath.NewInt(0),
		GasLimit:                100_000,
	})
	require.Error(t, err)
	require.Equal(t, uint32(0xF102), badExec.ResultCode)

	internalBody := []byte("internal body")
	internalExec, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: contractAddr,
		Message: async.MessageEnvelope{
			Source:      ownerAddr,
			Destination: contractAddr,
			Value:       sdk.NewCoin("naet", sdkmath.NewInt(77)),
			Opcode:      0x9301,
			QueryID:     19,
			Body:        internalBody,
			GasLimit:    100_000,
		},
		BlockTimestamp:          1_700_000_111,
		LogicalTime:             889,
		CurrentBlockLogicalTime: 990,
		OriginalBalance:         sdkmath.NewInt(1000),
		AttachedValue:           sdkmath.NewInt(77),
		GasLimit:                100_000,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), internalExec.ResultCode)
	require.Equal(t, mustCanonicalEncode(t, avm.ValueAddress(addressing.FormatAccAddress(ownerAddr))), internalExec.State["lastSender"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueCoins(big.NewInt(77))), internalExec.State["lastValue"])
	internalBodyChunk, err := avm.ToChunkPayload(internalBody, chunk.TypeNormal)
	require.NoError(t, err)
	var internalBodyHash [32]byte
	copy(internalBodyHash[:], internalBodyChunk.Hash())
	require.Equal(t, mustCanonicalEncode(t, avm.ValueHash(internalBodyHash)), internalExec.State["lastBodyHash"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueTimestamp(1_700_000_111)), internalExec.State["lastNow"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueTimestamp(889)), internalExec.State["lastLogicalTime"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueTimestamp(990)), internalExec.State["lastBlockLogicalTime"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueCoins(big.NewInt(1000))), internalExec.State["lastOriginalBalance"])
	require.Equal(t, mustCanonicalEncode(t, avm.ValueCoins(big.NewInt(77))), internalExec.State["lastAttachedValue"])
}

func TestStrictErrorModelRollsBackAndReturnsStructuredExitCodes(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(strictErrorModelSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())

	ownerAddr := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	ownerText := addressing.FormatAccAddress(ownerAddr)

	encode := func(v avm.RuntimeValue) []byte {
		t.Helper()
		bz, err := avm.CanonicalEncode(v)
		require.NoError(t, err)
		return bz
	}
	storage := avm.Storage{
		"counter": encode(avm.ValueUint64(7)),
		"owner":   encode(avm.ValueAddress(ownerText)),
		"nonce":   encode(avm.ValueUint32(4)),
	}

	bodyCodec, ok := res.MessageBodies["Control"]
	require.True(t, ok)

	run := func(source sdk.AccAddress, nonce uint32, step uint64, mode uint32) (avm.Execution, error) {
		t.Helper()
		body, err := bodyCodec.Encode(map[string]any{
			"nonce": nonce,
			"step":  step,
			"mode":  mode,
		})
		require.NoError(t, err)

		runner, err := avm.NewRunner(avm.DefaultParams())
		require.NoError(t, err)
		exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
			Entry: avm.EntryReceiveInternal,
			Message: async.MessageEnvelope{
				Source:      source,
				Destination: sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20)),
				Value:       sdk.NewCoin("naet", sdkmath.NewInt(123)),
				Opcode:      0x2301,
				QueryID:     7,
				Body:        body,
				GasLimit:    100_000,
			},
			ContractAddress: sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20)),
			GasLimit:        100_000,
		})
		return exec, err
	}

	badSenderExec, err := run(sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20)), 5, 9, 2)
	require.ErrorContains(t, err, "AVM abort with exit code 2001")
	require.Equal(t, uint32(2001), badSenderExec.ResultCode)
	require.Equal(t, storage, badSenderExec.State)
	require.Empty(t, badSenderExec.Outgoing)

	badNonceExec, err := run(ownerAddr, 99, 9, 2)
	require.ErrorContains(t, err, "AVM abort with exit code 2002")
	require.Equal(t, uint32(2002), badNonceExec.ResultCode)
	require.Equal(t, storage, badNonceExec.State)
	require.Empty(t, badNonceExec.Outgoing)

	assertExec, err := run(ownerAddr, 5, 9, 0)
	require.ErrorContains(t, err, "AVM abort with exit code 2003")
	require.Equal(t, uint32(2003), assertExec.ResultCode)
	require.Equal(t, storage, assertExec.State)
	require.Empty(t, assertExec.Outgoing)

	throwExec, err := run(ownerAddr, 5, 9, 1)
	require.ErrorContains(t, err, "AVM abort with exit code 2004")
	require.Equal(t, uint32(2004), throwExec.ResultCode)
	require.Equal(t, storage, throwExec.State)
	require.Empty(t, throwExec.Outgoing)
}

func hasOpcode(code []avm.Instruction, op avm.Opcode) bool {
	for _, ins := range code {
		if ins.Op == op {
			return true
		}
	}
	return false
}

func writeCompileArtifactsForTest(dir string, result *Result) error {
	if result == nil {
		return fmt.Errorf("nil compile result")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "module.bin"), result.ModuleBytes, 0o644); err != nil {
		return err
	}
	if result.CodeChunk != nil {
		bz, err := result.CodeChunk.Serialize()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "module.chunk"), bz, 0o644); err != nil {
			return err
		}
	}
	writeJSON := func(name string, value any) error {
		bz, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name), bz, 0o644)
	}
	if err := writeJSON("interface.json", result.Manifest); err != nil {
		return err
	}
	if err := writeJSON("stateinit.json", map[string]any{
		"abi_version":      result.StateInit.ABIVersion,
		"code_hash":        fmt.Sprintf("%x", result.StateInit.CodeHash),
		"init_data":        fmt.Sprintf("%x", result.StateInit.InitData),
		"salt":             fmt.Sprintf("%x", result.StateInit.Salt),
		"deployer_address": result.StateInit.DeployerAddress,
		"chain_id":         result.StateInit.ChainID,
		"namespace":        result.StateInit.Namespace,
		"initial_balance":  result.StateInit.InitialBalance,
		"capabilities":     result.StateInit.Capabilities.Flags,
		"state_init_hash":  fmt.Sprintf("%x", result.StateInitHash[:]),
		"module_hash":      fmt.Sprintf("%x", result.ModuleHash[:]),
	}); err != nil {
		return err
	}
	if err := writeJSON("storage-layout.json", result.StorageLayout); err != nil {
		return err
	}
	if err := writeJSON("selector-registry.json", result.SelectorRegistry); err != nil {
		return err
	}
	if err := writeJSON("codecs.json", map[string]any{
		"storage":              result.StorageCodec,
		"messages":             result.MessageCodecs,
		"message_bodies":       result.MessageBodies,
		"message_body_opcodes": result.MessageBodyOpcodes,
		"message_unions":       result.MessageUnions,
		"getters":              result.GetterCodecs,
		"events":               result.EventCodecs,
	}); err != nil {
		return err
	}
	if err := writeJSON("diagnostics.json", result.Diagnostics); err != nil {
		return err
	}
	if err := writeJSON("ir.json", result.IR); err != nil {
		return err
	}
	if err := writeJSON("dependency-lock.json", result.DependencyLock); err != nil {
		return err
	}
	return nil
}

// TestCompileAllowsMissingBouncedHandler replaces the old
// TestCompileRejectsMissingBouncedHandler: a @bounced handler is optional in
// the canonical surface (see strictErrorModelSource, which declares none), so
// removing it from the counter fixture must still compile.
func TestCompileAllowsMissingBouncedHandler(t *testing.T) {
	src := strings.ReplaceAll(counterSource, "  @bounced\n  func onBouncedMessage(in: InMessageBounced) {\n  }\n", "")
	if strings.Contains(src, "onBouncedMessage") {
		t.Fatal("fixture still contains the bounced handler; replacement pattern is stale")
	}
	c, _ := New(DefaultOptions())
	if _, err := c.Compile([]byte(src)); err != nil {
		t.Fatalf("expected contract without bounced handler to compile, got %v", err)
	}
}

func TestCompileEnforcesReservedMessageHandlerNames(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "handler annotation requires fixed name",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal func handleMessage(in: InMessage) -> u64 {
    return 0
  }
}
`,
			want: "Expected function name `onInternalMessage` for handler annotated with `@internal`.",
		},
		{
			name: "reserved name cannot be used without matching annotation",
			src: `
func onInternalMessage() -> u64 {
  return 0
}

struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "`onInternalMessage` is a reserved message handler name and can only be used with `@internal`.",
		},
		{
			name: "handler annotations are only allowed inside contract blocks",
			src: `
@internal func onInternalMessage(in: InMessage) {
}

struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "@internal handlers are only allowed inside contract blocks",
		},
		{
			name: "reserved name cannot be used with another handler annotation",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @external func onInternalMessage(in: InMessage) -> u64 {
    return 0
  }
}
`,
			want: "Function name `onInternalMessage` is reserved for `@internal` handlers. Expected `onExternalMessage` for `@external`.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(DefaultOptions())
			require.NoError(t, err)
			_, err = c.Compile([]byte(tt.src))
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCompileEnforcesCanonicalHandlerSignatures(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "internal handler signature is fixed",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal func onInternalMessage(inMsg: Segment) {
  }
}
`,
			want: "internal handler must be `func onInternalMessage(in: InMessage)`",
		},
		{
			name: "external handler signature is fixed",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @external func onExternalMessage(in: InMessage) {
  }
}
`,
			want: "external handler must be `func onExternalMessage(inMsg: Segment)`",
		},
		{
			name: "bounced handler signature is fixed",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced func onBouncedMessage(in: InMessage) {
  }
}
`,
			want: "bounced handler must be `func onBouncedMessage(in: InMessageBounced)`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(DefaultOptions())
			require.NoError(t, err)
			_, err = c.Compile([]byte(tt.src))
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// TestCompileRejectsSelectorCollision: explicit `selector = N` blocks are gone
// from the surface; the canonical collision is two @message structs bound to
// the same opcode (E_MESSAGE_OPCODE).
func TestCompileRejectsSelectorCollision(t *testing.T) {
	src := `
struct CounterState {
  count: u64 = 0
}

@message(0x4D01)
struct First {
  amount: u64
}

@message(0x4D01)
struct Second {
  note: u64
}

type InternalMsg = First | Second

contract Counter {
  storage: CounterState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }
}
`
	c, _ := New(DefaultOptions())
	_, err := c.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "opcode") || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("expected opcode collision error, got %v", err)
	}
}

type testResolver struct {
	files map[string]NamedSource
}

func (r testResolver) ResolveImport(imp ImportDecl) (ResolvedDependency, *SourceFile, error) {
	src, ok := r.files[imp.Path+"@"+imp.Version]
	if !ok {
		sum := sha256.Sum256([]byte(imp.Path + "@" + imp.Version))
		dep := dependencyFromParts(imp.Path, imp.Version, imp.Alias, sum, sum)
		return dep, nil, nil
	}
	parsed, err := ParseSourceNamed(src.Name, string(src.Data))
	if err != nil {
		return ResolvedDependency{}, nil, err
	}
	sum := sha256.Sum256([]byte(imp.Path + "@" + imp.Version))
	dep := dependencyFromParts(imp.Path, imp.Version, imp.Alias, sum, sum)
	return dep, parsed, nil
}

// TestCompileDexAmmExample locks in the canonical constant-product AMM DEX
// example (examples/avm/dex/dex_amm.atlx). It proves the ATLX surface can
// express a full DEX protocol on top of an on-chain dictionary: the contract
// keeps LP balances in a Map<address, uint64> and exercises the whole map
// surface (get/set/has/delete/keys/entries), swaps with the constant-product
// fee formula, add/remove liquidity that mints and burns LP balances in the
// dictionary, and outgoing payouts built with buildMessage's mode: and
// textComment: fields. Modeled on the "token wallet and master example"
// subtest above: it reads the real .atlx file and asserts the compiler emits
// byte-identical output across repeated runs (i.e. it compiles stably).
func TestCompileDexAmmExample(t *testing.T) {
	root := filepath.Join("..", "..", "..", "examples", "avm", "dex")
	dexData, err := os.ReadFile(filepath.Join(root, "dex_amm.atlx"))
	if err != nil {
		t.Fatalf("read dex_amm.atlx: %v", err)
	}

	sources := []NamedSource{{Name: "dex_amm.atlx", Data: dexData}}
	opts := DefaultOptions()

	const iterations = 100
	var firstModuleBytes []byte
	var firstModuleHash, firstManifestHash, firstStateInitHash, firstLockHash, firstRegistryHash [32]byte
	for i := 0; i < iterations; i++ {
		c, err := New(opts)
		if err != nil {
			t.Fatalf("dex_amm: new compiler on iteration %d: %v", i, err)
		}
		res, err := c.CompileFiles(append([]NamedSource(nil), sources...))
		if err != nil {
			t.Fatalf("dex_amm: compile on iteration %d: %v", i, err)
		}
		if i == 0 {
			firstModuleBytes = res.ModuleBytes
			firstModuleHash = res.ModuleHash
			firstManifestHash = res.ManifestHash
			firstStateInitHash = res.StateInitHash
			firstLockHash = res.DependencyLock.LockHash
			firstRegistryHash = res.SelectorRegistry.RegistryHash
			continue
		}
		if !bytes.Equal(res.ModuleBytes, firstModuleBytes) {
			t.Fatalf("dex_amm: module bytes differ on iteration %d (nondeterministic codegen)", i)
		}
		if res.ModuleHash != firstModuleHash {
			t.Fatalf("dex_amm: module hash differs on iteration %d", i)
		}
		if res.ManifestHash != firstManifestHash {
			t.Fatalf("dex_amm: manifest hash differs on iteration %d", i)
		}
		if res.StateInitHash != firstStateInitHash {
			t.Fatalf("dex_amm: state init hash differs on iteration %d", i)
		}
		if res.DependencyLock.LockHash != firstLockHash {
			t.Fatalf("dex_amm: dependency lock hash differs on iteration %d", i)
		}
		if res.SelectorRegistry.RegistryHash != firstRegistryHash {
			t.Fatalf("dex_amm: selector registry hash differs on iteration %d", i)
		}
	}
}

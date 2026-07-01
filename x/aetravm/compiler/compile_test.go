package compiler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

const counterSource = `
struct CounterState {
  count: u64 = 0
  owner: Address = "AEowner"
}

contract Counter {
  storage CounterState
  namespace "counter"
  chain "avm-local"
  deploy {
    set state.count = 0
    return 0
  }
  message external Increment(amount: u64) selector = 11 {
    set state.count = amount
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
    return state.count
  }
  event CountChanged(old: u64, new: u64)
  wallet action Increment {
    title = "Increment counter"
    risk = "low"
    confirm_label = "Increment"
    warning_level = "info"
    expected_side_effects = ["state write"]
    fund_access = false
    approval_semantics = "request"
  }
}
`

const richSemanticsSource = `
enum Mode {
  Off;
  On;
}

struct CounterState {
  count: u64 = 0
}

fn plusOne(x: u64) -> u64 {
  if 1 == 1 {
    return x + 1
  } else {
    return x
  }
}

fn choose(mode: Mode) -> u64 {
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
  storage CounterState
  namespace "counter"
  chain "avm-local"
  deploy {
    set state.count = 0
    return 0
  }
  message external Increment() selector = 11 {
    let sanity = plusOne(1)
    let mode_check = choose(Mode.On)
    let step = 2
    let rounds = 3
    for i in 0 to rounds {
      set state.count = state.count + step
    }
    return 0
  }
  message bounced Refund() selector = 12 {
    set state.count = state.count + 3
    return 0
  }
  message migrate Upgrade() selector = 14 {
    set state.count = state.count + 10
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
    return state.count
  }
}
`

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

	selectorMutatedSource := strings.Replace(counterSource, "selector = 11", "selector = 21", 1)
	selectorMutated, err := c.Compile([]byte(selectorMutatedSource))
	if err != nil {
		t.Fatalf("compile selector mutated: %v", err)
	}
	if base.SelectorRegistry.RegistryHash == selectorMutated.SelectorRegistry.RegistryHash {
		t.Fatal("selector registry hash did not change when selector changed")
	}
	if base.ManifestHash == selectorMutated.ManifestHash {
		t.Fatal("manifest hash did not change when selector changed")
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
	res, err := c.CompileFiles([]NamedSource{{Name: "counter.avm", Data: []byte(src)}})
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

fn bump(x: u64) -> u64 {
  return addOne(x)
}

fn label(x: u64) -> u64 {
  return decorate(x)
}

contract Counter {
  storage CounterState
  message external Increment() selector = 11 {
    set state.count = 1
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
    return state.count
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

fn label(x: u64) -> u64 {
  return decorate(x)
}

fn bump(x: u64) -> u64 {
  return addOne(x)
}

contract Counter {
  storage CounterState
  // reorder-only comment noise should not affect canonical commitments
  getter GetCount() -> u64 selector = 13 {
    return state.count
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  message external Increment() selector = 11 {
    set state.count = 1
    return 0
  }
}
`
	libMath := `
package lib.math

fn addOne(x: u64) -> u64 {
  return x + 1
}
`
	libMeta := `
package lib.meta

fn decorate(x: u64) -> u64 {
  return x
}
`
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	resolver := testResolver{
		files: map[string]NamedSource{
			"lib/math@1.0.0": {Name: "math.avm", Data: []byte(libMath)},
			"lib/meta@1.0.0": {Name: "meta.avm", Data: []byte(libMeta)},
		},
	}
	c.opts.Resolver = resolver
	left, err := c.CompileFiles([]NamedSource{{Name: "base.avm", Data: []byte(base)}})
	if err != nil {
		t.Fatalf("compile base: %v", err)
	}
	right, err := c.CompileFiles([]NamedSource{{Name: "reordered.avm", Data: []byte(reordered)}})
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
}

func TestCompileRuntimeDifferentialStabilityAcrossFormattingAndRepeatedRuns(t *testing.T) {
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}

	parsed, err := ParseSourceNamed("counter.avm", counterSource)
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

	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	ctx := avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 9, GasLimit: 100_000},
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

func TestCompileDiagnosticsAreStableAcrossRepeatedRuns(t *testing.T) {
	bad := `
struct CounterState {
  count: u64 = "bad"
}

contract Counter {
  storage CounterState
  message bounced Refund() selector = 12 {
    return 0
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

fn bump(x: u64) -> u64 {
  return addOne(x)
}

contract Counter {
  storage CounterState
  deploy {
    set state.count = addOne(0)
    return 0
  }
  message external Increment() selector = 11 {
    set state.count = state.count + 1
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
    return addOne(1)
  }
}
`
	libMath := `
package lib.math
import "lib/core@1.0.0"

fn addOne(x: u64) -> u64 {
  return bumpCore(x)
}
`
	libCore := `
package lib.core

fn bumpCore(x: u64) -> u64 {
  return x + 1
}
`
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	resolver := testResolver{
		files: map[string]NamedSource{
			"lib/math@1.0.0": {Name: "math.avm", Data: []byte(libMath)},
			"lib/core@1.0.0": {Name: "core.avm", Data: []byte(libCore)},
		},
	}
	merged, err := parsePackageSources([]NamedSource{{Name: "root.avm", Data: []byte(root)}}, resolver)
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
	res, err := c.CompileFiles([]NamedSource{{Name: "root.avm", Data: []byte(root)}})
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

func TestCompileReportsFileSpans(t *testing.T) {
	src := `
package demo

struct CounterState {
  count: u64 = "bad"
}

contract Counter {
  storage CounterState
  message bounced Refund() selector = 12 {
    return 0
  }
}
`
	c, _ := New(DefaultOptions())
	_, err := c.CompileFiles([]NamedSource{{Name: "bad.avm", Data: []byte(src)}})
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), "bad.avm") {
		t.Fatalf("expected file span in error, got %v", err)
	}
}

func TestGetterRejectsStateWrites(t *testing.T) {
	src := `
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage CounterState
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
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
	if _, ok := res.Module.Exports[avm.EntryMigrate]; !ok {
		t.Fatal("rich semantics module missing migrate entrypoint export")
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

	migrateExec, err := runner.Run(res.Module, bouncedExec.State, avm.RuntimeContext{
		Entry:    avm.EntryMigrate,
		Message:  async.MessageEnvelope{Opcode: 14, QueryID: 3, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	if err != nil {
		t.Fatalf("run migrate handler: %v", err)
	}
	if got := avm.DecodeU64(migrateExec.State["count"]); got != 19 {
		t.Fatalf("expected migrate count 19, got %d", got)
	}
	getExec, err := runner.Run(res.Module, migrateExec.State, avm.RuntimeContext{Entry: avm.EntryQuery, Message: async.MessageEnvelope{Opcode: 13, QueryID: 2, GasLimit: 100_000}, GasLimit: 100_000})
	if err != nil {
		t.Fatalf("run getter: %v", err)
	}
	if getExec.ReturnValue != 19 {
		t.Fatalf("expected getter return 19, got %d", getExec.ReturnValue)
	}
}

func TestCompileSendLowering(t *testing.T) {
	src := `
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage CounterState
  message external Emit() selector = 11 {
    send 0 to "AEreceiver" opcode = 77;
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
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
		Message:  async.MessageEnvelope{Opcode: 11, QueryID: 9, GasLimit: 100_000},
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

func TestCompileCanonicalExamplesCoverAllEntrypointKinds(t *testing.T) {
	examples, err := filepath.Glob("../../../examples/avm/*.avm")
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
		avm.EntryDeploy,
		avm.EntryReceiveExternal,
		avm.EntryReceiveInternal,
		avm.EntryReceiveBounced,
		avm.EntryQuery,
		avm.EntryMigrate,
	}
	for _, entry := range required {
		if !covered[entry] {
			t.Fatalf("canonical examples do not cover entrypoint %v", entry)
		}
	}
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
		"storage":  result.StorageCodec,
		"messages": result.MessageCodecs,
		"getters":  result.GetterCodecs,
		"events":   result.EventCodecs,
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

func TestCompileRejectsMissingBouncedHandler(t *testing.T) {
	c, _ := New(DefaultOptions())
	_, err := c.Compile([]byte(strings.ReplaceAll(counterSource, "  message bounced Refund() selector = 12 {\n    return 0\n  }\n", "")))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bounce") {
		t.Fatalf("expected bounce error, got %v", err)
	}
}

func TestCompileRejectsSelectorCollision(t *testing.T) {
	src := `
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage CounterState
  message deploy Deploy() selector = 77 {
    return 0
  }
  message external First() selector = 77 {
    return 0
  }
  message bounced Refund() selector = 78 {
    return 0
  }
}
`
	c, _ := New(DefaultOptions())
	_, err := c.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "selector") {
		t.Fatalf("expected selector collision error, got %v", err)
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

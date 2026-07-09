package compiler

import (
	"strings"
	"testing"
)

func compileForHardening(t *testing.T, src string) error {
	t.Helper()
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Compile([]byte(src))
	return err
}

// Finding #1: @get functions and getter blocks must not call mutating builtins.
const pureBaseStorage = `
@storage
struct S {
    counter: uint64
    m: Map<uint64, uint64>
}

@message(0x1001)
struct Ping {}

type InternalMsg = Ping

contract C {
    storage: S
    incomingMessages: InternalMsg

    @store
    func S.save(self) {
        contract.setData(self.toChunk())
    }

    @store
    func S.load() {
        return S.fromChunk(contract.getData())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        var st = lazy S.load()
        st.save()
    }
`

func TestHardeningGetFuncCannotSetData(t *testing.T) {
	src := pureBaseStorage + `
    @get
    func bad(): uint64 {
        contract.setData(0)
        return 0
    }
}
`
	err := compileForHardening(t, src)
	if err == nil {
		t.Fatalf("expected E_PURE_MUTATION error, got nil")
	}
	if !strings.Contains(err.Error(), "mutate") && !strings.Contains(err.Error(), "pure") {
		t.Fatalf("expected purity error, got %v", err)
	}
}

func TestHardeningGetFuncCannotMapSet(t *testing.T) {
	src := pureBaseStorage + `
    @get
    func bad(): uint64 {
        var st = lazy S.load()
        st.m.set(1, 2)
        return 0
    }
}
`
	err := compileForHardening(t, src)
	if err == nil {
		t.Fatalf("expected E_PURE_MUTATION for map set in @get, got nil")
	}
}

// Finding #2: a self-recursive user function whose name collides with a
// builtin (hash/len/ok/err) must still be flagged as recursive.
func TestHardeningRecursiveBuiltinNamedFunc(t *testing.T) {
	for _, name := range []string{"hash", "len", "ok", "err"} {
		src := `
@storage
struct S { counter: uint64 }

@message(0x1001)
struct Ping {}

type InternalMsg = Ping

func ` + name + `(x: uint64) -> uint64 {
    return ` + name + `(x)
}

contract C {
    storage: S
    incomingMessages: InternalMsg

    @internal
    func onInternalMessage(in: InMessage) {
    }
}
`
		err := compileForHardening(t, src)
		if err == nil {
			t.Fatalf("func %q: expected E_RECURSION, got nil", name)
		}
		if !strings.Contains(err.Error(), "recursi") {
			t.Fatalf("func %q: expected recursion error, got %v", name, err)
		}
	}
}

// Finding #4: deep nesting must produce a parse error, not a stack overflow.
func TestHardeningDeepNestingRejected(t *testing.T) {
	src := "const x = " + strings.Repeat("(", 100000) + "1" + strings.Repeat(")", 100000)
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected parse error for deeply nested parens")
	}
	if !strings.Contains(err.Error(), "too deep") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestHardeningDeepUnaryRejected(t *testing.T) {
	src := "const x = " + strings.Repeat("!", 100000) + "1"
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected parse error for deeply nested unary ops")
	}
	if !strings.Contains(err.Error(), "too deep") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestHardeningDeepTypeRejected(t *testing.T) {
	src := "type T = " + strings.Repeat("Chunk<", 1000) + "u64" + strings.Repeat(">", 1000)
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected parse error for deeply nested type args")
	}
	if !strings.Contains(err.Error(), "too deep") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestHardeningOversizeSourceRejected(t *testing.T) {
	src := strings.Repeat(" ", (1<<20)+1)
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected parse error for oversize source")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

// Finding #5: opcode/selector literals beyond uint32 must be rejected, not
// silently truncated.
func TestHardeningOpcodeOverflowRejected(t *testing.T) {
	src := `
@message(0x100000001)
struct Ping {}
`
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected opcode overflow error")
	}
	if !strings.Contains(err.Error(), "uint32 range") {
		t.Fatalf("expected uint32 range error, got %v", err)
	}
}

// Explicit `selector = N` blocks were removed from the surface; message
// selectors now come exclusively from @message(opcode) annotations, so the
// overflow guard is exercised on the annotation literal inside a full
// contract.
func TestHardeningSelectorOverflowRejected(t *testing.T) {
	src := `
struct S {
  count: u64 = 0
}

@message(0x1FFFFFFFF)
struct Inc {
  amount: u64
}

type InternalMsg = Inc

contract C {
  storage: S
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }
}
`
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected selector overflow error")
	}
	if !strings.Contains(err.Error(), "uint32 range") {
		t.Fatalf("expected uint32 range error, got %v", err)
	}
}

// Finding #6: non-ASCII digits/spaces must not be accepted by the lexer.
func TestHardeningNonASCIIDigitRejected(t *testing.T) {
	// U+0660 ARABIC-INDIC DIGIT ZERO is a unicode digit but not ASCII.
	src := "const x = " + string(rune(0x0660))
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected lexer error for non-ASCII digit")
	}
}

func TestHardeningNonASCIISpaceRejected(t *testing.T) {
	// U+00A0 NO-BREAK SPACE is unicode whitespace but not ASCII whitespace.
	src := "const x = 1"
	src = "const" + string(rune(0x00A0)) + "x = 1"
	_, err := ParseSource(src)
	if err == nil {
		t.Fatalf("expected lexer error for non-ASCII space")
	}
}

func TestHardeningGetterBlockCannotSave(t *testing.T) {
	src := pureBaseStorage + `
    getter Bad() -> uint64 {
        var st = lazy S.load()
        st.save()
        return 0
    }
}
`
	err := compileForHardening(t, src)
	if err == nil {
		t.Fatalf("expected E_PURE_MUTATION for getter-block save(), got nil")
	}
}

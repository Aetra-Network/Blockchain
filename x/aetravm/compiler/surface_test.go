package compiler

import (
	"testing"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/stretchr/testify/require"
)

const canonicalReferenceContractSource = `
const ERR_BAD_MSG = 0xFFFF

@storage
struct TreasuryState {
  balance: Coins = aet("0")
}

enum TreasuryAction {
  Deposit(amount: u64)
  Withdraw(amount: u64)
}

@message(0x4101)
struct DepositFunds {
  amount: uint64
}

@message(0x4102)
struct WithdrawFunds {
  amount: uint64
}

type TreasuryMsg = DepositFunds | WithdrawFunds

@pure func quote(amount: Coins) -> Coins {
  return amount
}

@pure func route(action: TreasuryAction) -> u64 {
  match action {
    Deposit(amount) {
      return amount
    }
    Withdraw(amount) {
      return amount
    }
  }
}

contract Treasury {
  author: "Aetralis reference"
  description: "Canonical treasury reference contract"
  version: "1.0.0"

  storage: TreasuryState
  incomingMessages: TreasuryMsg

  @store
  func TreasuryState.load() {
    return TreasuryState.fromChunk(contract.getData())
  }

  @store
  func TreasuryState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy TreasuryMsg.fromSegment(in.body)

    match (msg) {
      DepositFunds => {
        var st = lazy TreasuryState.load()
        st.balance += msg.amount
        st.save()
      }

      WithdrawFunds => {
        var st = lazy TreasuryState.load()
        st.balance -= msg.amount
        st.save()
      }

      else => {
        assert (in.body.isEmpty()) throw ERR_BAD_MSG
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func balance(): Coins {
    const st = lazy TreasuryState.load()
    return st.balance
  }
}
`

// legacySpecTreasurySource is the pre-ATLX Treasury example that used to live
// in docs/architecture/language-spec.md. Every declaration form in it
// (colon-less storage, message/getter/event/wallet action items, selector
// pins) is outside the language; the compiler must reject it.
const legacySpecTreasurySource = `
struct TreasuryState {
  balance: Coins = aet("0")
}

contract Treasury {
  storage TreasuryState

  @internal message internal Receive(amount: u64) selector = auto {
    set state.balance = state.balance + amount
    return 0
  }

  @external message external Transfer(amount: u64) selector = auto {
    set state.balance = amount
    return 0
  }

  @bounced message bounced Refund() selector = auto {
    set state.balance = state.balance
    return 0
  }

  @get getter GetBalance() -> Coins selector = auto {
    return state.balance
  }

  event BalanceChanged(old: Coins, new: Coins)

  wallet action Transfer {
    title = "Transfer funds"
    risk = "high"
    confirm_label = "Send"
    warning_level = "warn"
    expected_side_effects = ["state write", "value transfer"]
    fund_access = true
    approval_semantics = "spend"
  }
}
`

const decimalAETContractSource = `
@storage
struct VaultState {
  balance: Coins = aet("0.05")
}

@message(0x1201)
struct Poke {}

type VaultMsg = Poke

contract Vault {
  storage: VaultState
  incomingMessages: VaultMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

const localBindingsSurfaceSource = `
@pure func choose(flag: bool) -> uint64 {
  const fallback = 1
  var current = fallback
  if flag {
    return current
  } else {
    return fallback
  }
}
`

const assertSurfaceSource = `
struct DemoState {
  owner: address
  seqno: uint32
}

@message(0x2001)
struct Touch {
  seqno: uint32
}

type ExternalMsg = Touch

contract Demo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy ExternalMsg.fromSegment(inMsg)
    const st = lazy DemoState.fromChunk(contract.getData())

    match (msg) {
      Touch(seqno) => {
        assert (seqno == st.seqno) throw 401
      }
      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

func TestCanonicalReferenceContractCompiles(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(canonicalReferenceContractSource))
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())
	require.Empty(t, res.Diagnostics)
	require.Equal(t, "Treasury", res.Manifest.Name)
	require.Equal(t, "reject", res.Manifest.UnknownMessagePolicy)
	require.Equal(t, "Treasury", res.SelectorRegistry.Contract)
	require.Equal(t, "TreasuryState", res.StorageLayout.Name)
	require.Empty(t, res.Manifest.Events)
	require.Empty(t, res.Manifest.WalletActions)

	kinds := map[string]bool{}
	for _, entry := range res.SelectorRegistry.Entries {
		kinds[entry.Kind] = true
	}
	require.True(t, kinds["message"])
	require.True(t, kinds["getter"])
	require.False(t, kinds["event"])
	require.False(t, kinds["wallet_action"])

	require.Contains(t, res.Module.Exports, avm.EntryReceiveInternal)
	require.Contains(t, res.Module.Exports, avm.EntryReceiveBounced)
	require.Contains(t, res.Module.Exports, avm.EntryQuery)
}

// TestLegacySpecTreasuryExampleIsRejected pins the removal of the legacy
// declaration surface: the old language-spec Treasury example must fail to
// parse, and each of its declaration forms must fail on its own too.
func TestLegacySpecTreasuryExampleIsRejected(t *testing.T) {
	_, err := ParseSourceNamed("legacy.atlx", legacySpecTreasurySource)
	require.Error(t, err)
	require.ErrorContains(t, err, "requires a colon")

	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(legacySpecTreasurySource))
	require.Error(t, err)

	tests := []struct {
		name string
		item string
		want string
	}{
		{
			name: "message declaration",
			item: `message internal Receive(amount: u64) selector = auto {
    return 0
  }`,
			want: `legacy declaration "message" is not part of ATLX`,
		},
		{
			name: "getter declaration",
			item: `getter GetBalance() -> Coins selector = auto {
    return 0
  }`,
			want: `legacy declaration "getter" is not part of ATLX`,
		},
		{
			name: "event declaration",
			item: `event BalanceChanged(old: Coins, new: Coins)`,
			want: `legacy declaration "event" is not part of ATLX`,
		},
		{
			name: "wallet action declaration",
			item: `wallet action Transfer {
    title = "Transfer funds"
  }`,
			want: `legacy declaration "wallet action" is not part of ATLX`,
		},
		{
			name: "selector item",
			item: `selector = 11`,
			want: `"selector" is not part of ATLX`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `
struct DemoState {
  balance: Coins
}

@message(0x2101)
struct Ping {}

type DemoMsg = Ping

contract Demo {
  storage: DemoState
  incomingMessages: DemoMsg

  ` + tt.item + `
}
`
			_, err := ParseSourceNamed("legacy_item.atlx", src)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestContractMetadataKeysRequireColon(t *testing.T) {
	tests := []struct {
		name string
		item string
	}{
		{name: "storage", item: `storage DemoState`},
		{name: "author", item: `author "someone"`},
		{name: "description", item: `description "text"`},
		{name: "version", item: `version "1.0.0"`},
		{name: "incomingMessages", item: `incomingMessages DemoMsg`},
		{name: "incomingExternal", item: `incomingExternal DemoMsg`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `
struct DemoState {
  balance: Coins
}

@message(0x2102)
struct Ping {}

type DemoMsg = Ping

contract Demo {
  ` + tt.item + `
}
`
			_, err := ParseSourceNamed("metadata.atlx", src)
			require.Error(t, err)
			require.ErrorContains(t, err, "requires a colon")
		})
	}
}

func TestAetBuiltinFoldsDecimalLiteralIntoCanonicalDefaults(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(decimalAETContractSource))
	require.NoError(t, err)

	defaults, err := res.StorageCodec.EncodeDefaults()
	require.NoError(t, err)
	require.Contains(t, string(defaults), "50000000")
	require.NotContains(t, string(defaults), "0.05")
	require.NotContains(t, string(defaults), "aet")
}

func TestLocalBindingsSurfaceParsesAndFormatsCanonically(t *testing.T) {
	formatted, err := FormatSourceNamed("bindings.atlx", localBindingsSurfaceSource)
	require.NoError(t, err)
	require.Contains(t, formatted, "const fallback = 1")
	require.Contains(t, formatted, "var current = fallback")
	require.NotContains(t, formatted, "let ")
	require.NotContains(t, formatted, "val ")
	require.NotContains(t, formatted, "mut ")
}

func TestAssertWithComparisonCompiles(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	_, err = c.Compile([]byte(assertSurfaceSource))
	require.NoError(t, err)
}

func TestParseRejectsInvalidSurfaceSyntax(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "invalid annotation placement",
			src: `
@get struct DemoState {
  root: Chunk
}
`,
			want: "annotation \"@get\" is not valid on struct",
		},
		{
			name: "impure annotation on struct",
			src: `
@impure struct DemoState {
  root: Chunk
}
`,
			want: "annotation \"@impure\" is not valid on struct",
		},
		{
			name: "unknown top-level item",
			src: `
trait Demo {}
`,
			want: "unexpected top-level declaration",
		},
		{
			name: "multiple annotations on struct",
			src: `
@storage @message(0x1001) struct DemoState {
  root: Chunk
}
`,
			want: "only one annotation is allowed per declaration",
		},
		{
			name: "external annotation with argument list",
			src: `
@external(inMsg: Segment)
func onExternalMessage(inMsg: Segment) {
}
`,
			want: "annotation @external takes no arguments",
		},
		{
			name: "internal annotation with argument list",
			src: `
@internal(in: InMessage)
func onInternalMessage(in: InMessage) {
}
`,
			want: "annotation @internal takes no arguments",
		},
		{
			name: "message annotation without numeric opcode",
			src: `
@message(Ping)
struct Ping {}
`,
			want: "@message requires a numeric opcode argument",
		},
		{
			name: "forbidden local keyword let",
			src: `
func demo() {
  let x = 1
}
`,
			want: "local bindings must use const or var",
		},
		{
			name: "forbidden local keyword val",
			src: `
func demo() {
  val x = 1
}
`,
			want: "local bindings must use const or var",
		},
		{
			name: "forbidden local keyword mut",
			src: `
func demo() {
  mut x = 1
}
`,
			want: "local bindings must use const or var",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSourceNamed("surface.atlx", tt.src)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCompileRejectsInvalidSurfaceSemantics(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "if condition must be bool",
			src: `
struct DemoState {
  count: uint32
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
    if (1) {
    } else {
    }
  }
}
`,
			want: "if condition must be bool",
		},
		{
			// storage is optional on a contract, but when it IS declared it
			// must resolve to a real struct.
			name: "storage type not found",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: NoSuchState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "storage type",
		},
		{
			name: "duplicate field names",
			src: `
struct DemoState {
  root: Chunk
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
			want: "duplicate field",
		},
		{
			name: "duplicate enum variants",
			src: `
enum Mode {
  Off;
  Off;
}

struct DemoState {
  mode: Mode = Mode.Off
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "duplicate variant",
		},
		{
			name: "duplicate message opcodes",
			src: `
struct DemoState {
  root: Chunk
}

@message(0x11)
struct Ping {}

@message(0x11)
struct Pong {}

type DemoMsg = Ping | Pong

contract Demo {
  storage: DemoState
  incomingMessages: DemoMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "is already bound to message schema",
		},
		{
			name: "@pure mutation",
			src: `
struct DemoState {
  root: Chunk
}

@pure func bump() -> u64 {
  set state.root = state.root
  return 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "function \"bump\" is annotated @pure but has side effects",
		},
		{
			name: "@get mutation",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getRoot(): Chunk {
    set state.root = state.root
    return state.root
  }
}
`,
			want: "pure functions cannot write state or perform chain-visible side effects",
		},
		{
			name: "multiple annotations on function",
			src: `
struct DemoState {
  root: Chunk
}

@pure @impure func bump() -> u64 {
  return 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "only one annotation is allowed per declaration",
		},
		{
			name: "non-exhaustive match",
			src: `
enum Mode {
  Off;
  On;
}

func choose(mode: Mode) -> u64 {
  match mode {
    Mode.Off {
      return 0
    }
  }
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
			want: "missing variant",
		},
		{
			name: "legacy bounced annotation alias",
			src: `
struct DemoState {
  root: Chunk
}

@bouncee func onBouncedMessage(in: InMessageBounced) -> u64 {
  return 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState
}
`,
			want: "unknown annotation",
		},
		{
			name: "reserved internal handler name without annotation",
			src: `
struct DemoState {
  root: Chunk
}

func onInternalMessage(in: InMessage) -> u64 {
  return 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "reserved message handler name",
		},
		{
			name: "annotated internal handler must use reserved name",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @internal func handle(in: InMessage) -> u64 {
    return 0
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "Expected function name `onInternalMessage`",
		},
		{
			name: "reserved bounced handler name without annotation",
			src: `
struct DemoState {
  root: Chunk
}

func onBouncedMessage(in: InMessageBounced) -> u64 {
  return 0
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState
}
`,
			want: "reserved message handler name",
		},
		{
			name: "annotated external handler must use reserved name",
			src: `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @external func handle(inMsg: Segment) -> u64 {
    return 0
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`,
			want: "Expected function name `onExternalMessage`",
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

func TestCompileAllowsContractWithoutStorage(t *testing.T) {
	src := `
@message(0x2103)
struct Ping {}

type ExternalMsg = Ping

contract Demo {
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
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
}

func TestCompileRejectsContractWithoutIncomingMessagesOrExternal(t *testing.T) {
	src := `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.ErrorContains(t, err, "must declare incomingMessages, incomingExternal, or both")
}

func TestCanonicalReferenceSelectorKindsAreStable(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(canonicalReferenceContractSource))
	require.NoError(t, err)

	counts := map[string]int{}
	for _, entry := range res.SelectorRegistry.Entries {
		counts[entry.Kind]++
	}
	require.Equal(t, 2, counts["message"])
	require.Equal(t, 1, counts["getter"])
	require.Equal(t, 0, counts["event"])
	require.Equal(t, 0, counts["wallet_action"])
	require.Equal(t, "reject", res.Manifest.UnknownMessagePolicy)
}

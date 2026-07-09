package compiler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const legacySurfaceSource = `
struct DemoState {
  root: Chunk
  child: Ref<Chunk>
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

const canonicalSurfaceSource = `
struct DemoState {
  root: Chunk
  window: Segment
  child: ChunkRef<Chunk>
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

// autoSelectorSource keeps the removed legacy surface (message/getter blocks
// with `selector = auto`) so tests can assert the parser rejects it.
const autoSelectorSource = `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  message external Ping() selector = auto {
    return 0
  }
  getter Count() -> u64 selector = auto {
    return 0
  }
}
`

const annotatedSurfaceSource = `
@pure func addOne(x: u64) -> u64 {
  return x + 1
}

@impure func helper(x: u64) -> u64 {
  return x
}

struct DemoState {
  root: Chunk
}

@message(0x1001)
struct Ping {
  ticket: u64
}

type ExternalMsg = Ping

contract Demo {
  storage: DemoState
  incomingExternal: ExternalMsg

  @external
  func onExternalMessage(inMsg: Segment) {
  }

  @get
  func count(): u64 {
    return 0
  }
}
`

const typedSchemaSurfaceSource = `
@storage struct DemoState {
  root: Chunk
  cache: lazy Chunk<Chunk>?
}

@message(0x1001) struct Ping {
  ticket: u64
}

type InternalMsg = Ping

contract Demo {
  storage: DemoState
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }
}
`

const messageBuilderSurfaceSource = `
@pure func makeEnvelope(receiver: Address) -> MessageEnvelope {
  return buildMessage({
    bounce: false,
    amount: 0,
    receiver: receiver,
    body: Ping {
      ticket: 1
    }
  })
}
`

func TestFormatSourceCanonicalizesLegacyAliases(t *testing.T) {
	formatted, err := FormatSourceNamed("legacy.avm", legacySurfaceSource)
	require.NoError(t, err)
	require.Contains(t, formatted, "Chunk")
	require.Contains(t, formatted, "ChunkRef<Chunk>")
	require.NotContains(t, formatted, "\n  child: Ref<")
}

func TestCompileReportsLegacyAliasAndExtensionWarnings(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.CompileFiles([]NamedSource{{Name: "legacy.avm", Data: []byte(legacySurfaceSource)}})
	require.NoError(t, err)
	require.NotEmpty(t, res.Diagnostics)

	var warningMessages []string
	for _, diag := range res.Diagnostics {
		if diag.Severity == SeverityWarning {
			warningMessages = append(warningMessages, diag.Message)
		}
	}
	require.NotEmpty(t, warningMessages)
	require.True(t, containsWarning(warningMessages, "legacy .avm extension"))
	require.True(t, containsWarning(warningMessages, "Ref<T> is deprecated"))
	require.Equal(t, "Chunk", res.Source.Structs[0].Fields[0].Type.Name)
	require.Equal(t, "ChunkRef", res.Source.Structs[0].Fields[1].Type.Name)
}

func TestCompileRejectsLegacySurfaceInStrictMode(t *testing.T) {
	opts := DefaultOptions()
	opts.SurfaceCompatibility = SurfaceCompatibilityStrict
	c, err := New(opts)
	require.NoError(t, err)
	_, err = c.CompileFiles([]NamedSource{{Name: "legacy.atlx", Data: []byte(legacySurfaceSource)}})
	require.Error(t, err)
	require.ErrorContains(t, err, "deprecated")
}

func TestCompileAcceptsATLXCanonicalSurface(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.CompileFiles([]NamedSource{{Name: "canonical.atlx", Data: []byte(canonicalSurfaceSource)}})
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
}

// TestSelectorAutoLegacySurfaceRejected replaces the old
// TestSelectorAutoParsesAsImplicitSelector: `selector = auto` only existed on
// legacy message/getter declarations, which the parser now rejects outright.
func TestSelectorAutoLegacySurfaceRejected(t *testing.T) {
	_, err := ParseSourceNamed("auto.atlx", autoSelectorSource)
	require.Error(t, err)
	require.ErrorContains(t, err, `legacy declaration "message" is not part of ATLX`)

	_, err = FormatSourceNamed("auto.atlx", autoSelectorSource)
	require.Error(t, err)
}

func TestAnnotationsParseAndFormatCanonically(t *testing.T) {
	formatted, err := FormatSourceNamed("annotated.atlx", annotatedSurfaceSource)
	require.NoError(t, err)
	require.Contains(t, formatted, "@pure func addOne")
	require.Contains(t, formatted, "@impure func helper")
	require.Contains(t, formatted, "@message(0x1001) struct Ping")
	require.Contains(t, formatted, "@external func onExternalMessage(inMsg: Segment)")
	require.Contains(t, formatted, "@get func count()")
	require.NotContains(t, formatted, "selector =")
}

func TestTypedSchemasParseAndFormatCanonically(t *testing.T) {
	formatted, err := FormatSourceNamed("schemas.atlx", typedSchemaSurfaceSource)
	require.NoError(t, err)
	require.Contains(t, formatted, "@storage struct DemoState")
	require.Contains(t, formatted, "@message(0x1001) struct Ping")
	require.Contains(t, formatted, "type InternalMsg = Ping")
	require.Contains(t, formatted, "cache: lazy Chunk<Chunk>?")
}

func TestMessageBuilderSurfaceParsesAndFormatsCanonically(t *testing.T) {
	formatted, err := FormatSourceNamed("message-builder.atlx", messageBuilderSurfaceSource)
	require.NoError(t, err)
	require.Contains(t, formatted, "buildMessage({")
	require.Contains(t, formatted, "bounce: false")
	require.Contains(t, formatted, "amount: 0")
	require.Contains(t, formatted, "receiver: receiver")
	require.Contains(t, formatted, "body: Ping {ticket: 1}")
}

const sendModeBuiltinSource = `
struct DemoState {
  root: Chunk
}

@pure func keepMode() -> u64 {
  const mode = SEND_BOUNCE_ON_FAIL
  return mode
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

const contractMetadataSource = `
struct DemoState {
  root: Chunk
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState
  author: "Ada"
  description: "Demo contract"
  version: "1.2.3"

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

func TestSendModeBuiltinsLowerAsTypedConstants(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(sendModeBuiltinSource))
	require.NoError(t, err)
	require.Empty(t, res.Diagnostics)
}

func TestContractMetadataParsesAndFormatsCanonically(t *testing.T) {
	formatted, err := FormatSourceNamed("metadata.atlx", contractMetadataSource)
	require.NoError(t, err)
	require.Contains(t, formatted, "author: \"Ada\"")
	require.Contains(t, formatted, "description: \"Demo contract\"")
	require.Contains(t, formatted, "version: \"1.2.3\"")
}

func TestPureFunctionCannotCallImpureHelper(t *testing.T) {
	src := `
struct DemoState {
  root: Chunk
}

@impure func mutate() -> u64 {
  send 0 to "AEreceiver" opcode = 77;
  return 1
}

@pure func use() -> u64 {
  return mutate()
}

contract Demo {
  storage: DemoState
  incomingMessages: DemoState

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.Error(t, err)
	require.ErrorContains(t, err, "function \"use\" is annotated @pure but has side effects")
}

func containsWarning(messages []string, needle string) bool {
	for _, msg := range messages {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

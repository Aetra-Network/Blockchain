package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// formatRoundTripSource reuses the canonical counter fixture: counterSource
// deliberately sticks to the formatter-representable subset of the surface
// (see compile_test.go) and exercises @internal/@external/@bounced handlers,
// @message structs, a @get getter, and contract metadata.
const formatRoundTripSource = counterSource

func TestFormatSourceRoundTrip(t *testing.T) {
	file, err := ParseSourceNamed("counter.atlx", formatRoundTripSource)
	require.NoError(t, err)
	formatted := FormatSource(file)
	require.NotEmpty(t, formatted)

	again, err := FormatSourceNamed("counter.atlx", formatted)
	require.NoError(t, err)
	require.Equal(t, formatted, again)

	c, err := New(DefaultOptions())
	require.NoError(t, err)
	original, err := c.Compile([]byte(formatRoundTripSource))
	require.NoError(t, err)
	rewritten, err := c.Compile([]byte(formatted))
	require.NoError(t, err)
	require.Equal(t, original.ModuleHash, rewritten.ModuleHash)
	require.Equal(t, original.ManifestHash, rewritten.ManifestHash)
	require.Equal(t, original.StateInitHash, rewritten.StateInitHash)
}

func FuzzFormatSourceRoundTrip(f *testing.F) {
	f.Add(formatRoundTripSource)
	f.Add("struct S {\n  x: u64 = 0\n}\n\ncontract C {\n  storage: S\n  incomingMessages: S\n\n  @bounced\n  func onBouncedMessage(in: InMessageBounced) {\n  }\n}\n")
	f.Fuzz(func(t *testing.T, src string) {
		file, err := ParseSourceNamed("fuzz.atlx", src)
		if err != nil {
			return
		}
		formatted := FormatSource(file)
		if formatted == "" {
			return
		}
		again, err := ParseSourceNamed("fuzz.atlx", formatted)
		require.NoError(t, err)
		_ = again
	})
}

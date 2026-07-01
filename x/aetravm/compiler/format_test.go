package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const formatRoundTripSource = `
struct CounterState {
  count: u64 = 0
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
}
`

func TestFormatSourceRoundTrip(t *testing.T) {
	file, err := ParseSourceNamed("counter.avm", formatRoundTripSource)
	require.NoError(t, err)
	formatted := FormatSource(file)
	require.NotEmpty(t, formatted)

	again, err := FormatSourceNamed("counter.avm", formatted)
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
	f.Add("struct S { x: u64 = 0 }\ncontract C { storage S message bounced Refund() selector = 1 { return 0 } }\n")
	f.Fuzz(func(t *testing.T, src string) {
		file, err := ParseSourceNamed("fuzz.avm", src)
		if err != nil {
			return
		}
		formatted := FormatSource(file)
		if formatted == "" {
			return
		}
		again, err := ParseSourceNamed("fuzz.avm", formatted)
		require.NoError(t, err)
		_ = again
	})
}


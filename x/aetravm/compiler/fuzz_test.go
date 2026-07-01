package compiler

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

func FuzzCompileSelectorCollisionAndArtifactRoundTripNoPanic(f *testing.F) {
	f.Add("11", "12")
	f.Add("77", "77")
	f.Add("abc", "def")
	f.Fuzz(func(t *testing.T, leftSeed string, rightSeed string) {
		left := normalizeSelectorSeed(leftSeed, 11)
		right := normalizeSelectorSeed(rightSeed, 12)

		src := fmt.Sprintf(`
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage CounterState
  message external First() selector = %d {
    return 0
  }
  message bounced Refund() selector = 98 {
    return 0
  }
  getter GetCount() -> u64 selector = %d {
    return state.count
  }
}
`, left, right)

		c, err := New(DefaultOptions())
		require.NoError(t, err)

		res, err := c.Compile([]byte(src))
		if left == right {
			require.Error(t, err)
			require.ErrorContains(t, err, "selector")
			return
		}
		if err != nil {
			return
		}

		_, err = avm.DecodeModule(res.ModuleBytes)
		require.NoError(t, err)
		_, err = avm.HashStateInit(res.StateInit)
		require.NoError(t, err)
	})
}

func FuzzCompileMalformedSourceNoPanic(f *testing.F) {
	f.Add(counterSource)
	f.Add("bad")
	f.Add("contract C {")
	f.Fuzz(func(t *testing.T, src string) {
		c, err := New(DefaultOptions())
		require.NoError(t, err)

		res, err := c.Compile([]byte(src))
		if err != nil {
			return
		}

		_, err = avm.DecodeModule(res.ModuleBytes)
		require.NoError(t, err)
		_, err = avm.HashStateInit(res.StateInit)
		require.NoError(t, err)
	})
}

func normalizeSelectorSeed(seed string, fallback uint32) uint32 {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return fallback
	}
	value, err := strconv.ParseUint(seed, 10, 32)
	if err != nil {
		return fallback
	}
	return uint32(value)
}

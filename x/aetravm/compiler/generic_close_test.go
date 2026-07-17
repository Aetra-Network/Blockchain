package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseNestedGenericAbuttingBrackets is the regression test for the lexer/
// parser disagreement on abutting generic close brackets: the lexer greedily
// emits a single '>>' (right-shift) token, so a nested generic whose brackets
// touch -- Chunk<Chunk<Leaf>> or Map<address, Chunk<Leaf>> -- failed to parse
// even though it is well-formed and type-valid. Before the fix these sources
// errored with `expected ... got ">>"`.
func TestParseNestedGenericAbuttingBrackets(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "double nested",
			src: `struct Leaf { value: uint64 }
struct Nested { tree: Chunk<Chunk<Leaf>> }`,
		},
		{
			name: "map value nested",
			src: `struct Leaf { value: uint64 }
struct Nested { table: Map<address, Chunk<Leaf>> }`,
		},
		{
			name: "triple nested",
			src: `struct Leaf { value: uint64 }
struct Nested { tree: Chunk<Chunk<Chunk<Leaf>>> }`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSource(tc.src)
			require.NoError(t, err,
				"FIX REGRESSION: abutting generic close brackets must parse; the lexer's greedy '>>' must be split in type context")
		})
	}
}

// TestParseSpacedNestedGenericStillParses guards against a regression in the
// opposite direction: the spaced form (which never hit the '>>' merge) must
// keep parsing identically after the fix.
func TestParseSpacedNestedGenericStillParses(t *testing.T) {
	src := `struct Leaf { value: uint64 }
struct Nested { tree: Chunk<Chunk<Leaf> > }`
	_, err := ParseSource(src)
	require.NoError(t, err)
}

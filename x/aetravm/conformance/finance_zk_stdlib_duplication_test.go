package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFinanceZKStdlibDuplicatesStayInSync guards the deliberate source
// duplication documented in finance_stdlib.atlx / finance_types.atlx /
// groth16_stdlib.atlx's own header comments: AVM v1 cannot share a
// multi-statement or looping routine across compiled units (every ordinary
// function call is textually inlined at its call site -- compile.go's
// tryInlineUserFunctionCall only handles a single-return-expression body --
// and the AVM requires exactly one contract per compiled unit, so a bare
// function library can't itself compile), so reference contracts embed a
// verbatim copy of the canonical stdlib routine they need instead of
// importing it. That is a correct, deliberate design (see the referenced
// files' own header comments for the full reasoning), but it means a future
// edit to the canonical copy that is not mirrored into every duplicate site
// would silently diverge -- for fee-bps / liquidation-ratio / pairing-check
// logic, that is a real financial-correctness bug class, not a style nit.
//
// This test extracts each duplicated function/const declaration by name from
// both the canonical source and every duplicate site, normalizes ONLY
// whitespace (collapsing indentation/line-break differences -- never
// reordering or renaming anything), and asserts the two are identical.
//
// NOT covered here: finance_types.atlx's bpApplyFloor vs. health_factor.atlx's
// bpApplyFloor. That pair has ALREADY DRIFTED (finance_types.atlx calls
// mulDivFloor; health_factor.atlx calls the bare mulDiv spelling) -- see
// TestMulDivFloorCeilAreAliases in
// x/aetravm/compiler/sig_math_builtins_test.go, which proves the two
// spellings lower to the exact same opcode, so this is NOT a behavioral bug.
// It IS real textual drift between a duplicate and its canonical source, and
// picking one spelling to silently "fix" the other is exactly the kind of
// judgment call this codebase reserves for a human (see the doc comments on
// both functions in finance_types.atlx and health_factor.atlx). Do not add
// this pair to dupCases below until that call is made and the two files
// agree.
func TestFinanceZKStdlibDuplicatesStayInSync(t *testing.T) {
	type dupCase struct {
		kind          string // "func" or "const"
		name          string // function/const name being compared
		canonicalFile string // relative to examples/avm/
		dupFile       string // relative to examples/avm/
	}

	cases := []dupCase{
		// ---- finance_stdlib.atlx (canonical) -> health_factor.atlx / perp_pnl.atlx / sqrt_price.atlx ----
		{"func", "bpApplyTo", "finance/finance_stdlib.atlx", "finance/health_factor.atlx"},
		{"func", "decDiv", "finance/finance_stdlib.atlx", "finance/health_factor.atlx"},
		{"func", "decDiv", "finance/finance_stdlib.atlx", "finance/sqrt_price.atlx"},
		{"func", "pnlOf", "finance/finance_stdlib.atlx", "finance/perp_pnl.atlx"},
		{"func", "ratioGtBounded", "finance/finance_stdlib.atlx", "finance/sqrt_price.atlx"},

		// ---- finance_types.atlx (canonical) -> health_factor.atlx ----
		{"func", "bpFromPercentBounded", "finance/finance_types.atlx", "finance/health_factor.atlx"},
		// bpApplyFloor is DELIBERATELY OMITTED here -- see the drifted-pair
		// note in this test's doc comment above.

		// ---- groth16_stdlib.atlx (canonical) -> groth16_verifier.atlx ----
		{"func", "groth16VkAlpha", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16VkBeta", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16VkGamma", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16VkDelta", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16VkIC", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16ProofA", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16ProofB", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16ProofC", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16PublicInputAt", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"func", "groth16NegateG1", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},

		// ---- ZK ABI constant block, byte-for-byte per both files' own header comments ----
		{"const", "BN254_P_BE", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"const", "G1_LEN", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"const", "G2_LEN", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"const", "SCALAR_LEN", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"const", "VK_FIXED_LEN", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"const", "PROOF_LEN", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
		{"const", "MAX_PUBLIC_INPUTS", "zk/groth16_stdlib.atlx", "zk/groth16_verifier.atlx"},
	}

	sourceCache := map[string]string{}
	loadSource := func(rel string) string {
		if src, ok := sourceCache[rel]; ok {
			return src
		}
		// Mirrors compileExampleFile's own relative path convention
		// (examples_acceptance_test.go): three levels up from
		// x/aetravm/conformance to the repo root, then examples/avm/.
		path := filepath.Clean(filepath.Join("..", "..", "..", "examples", "avm", rel))
		data, err := os.ReadFile(path)
		require.NoError(t, err, "reading %s", path)
		src := string(data)
		sourceCache[rel] = src
		return src
	}

	for _, tc := range cases {
		tc := tc
		subName := tc.name + "__" + strings.ReplaceAll(tc.dupFile, "/", "_")
		t.Run(subName, func(t *testing.T) {
			canonicalSrc := loadSource(tc.canonicalFile)
			dupSrc := loadSource(tc.dupFile)

			var canonicalSpan, dupSpan string
			switch tc.kind {
			case "func":
				canonicalSpan = extractFuncSpan(t, canonicalSrc, tc.canonicalFile, tc.name)
				dupSpan = extractFuncSpan(t, dupSrc, tc.dupFile, tc.name)
			case "const":
				canonicalSpan = extractConstSpan(t, canonicalSrc, tc.canonicalFile, tc.name)
				dupSpan = extractConstSpan(t, dupSrc, tc.dupFile, tc.name)
			default:
				t.Fatalf("unknown dupCase kind %q", tc.kind)
			}

			canonicalNorm := normalizeATLXSpan(canonicalSpan)
			dupNorm := normalizeATLXSpan(dupSpan)
			require.Equalf(t, canonicalNorm, dupNorm,
				"%s's %q has drifted from its canonical source %s's %q -- "+
					"re-sync %s's copy against %s (or, if the change is "+
					"intentional, update BOTH files and this test's case list "+
					"together)",
				tc.dupFile, tc.name, tc.canonicalFile, tc.name, tc.dupFile, tc.canonicalFile)
		})
	}
}

// extractFuncSpan pulls the signature+body of the named top-level function
// out of an ATLX source file, from the `func <name>(` keyword through the
// matching closing brace of its body (brace-balanced, so nested struct
// literals / if / while blocks inside the body do not truncate the span
// early). Doc comments above the function are deliberately NOT included --
// canonical and duplicate sites are expected to carry DIFFERENT prose (a
// per-site provenance note pointing back at the canonical source); only the
// code itself is required to match byte-for-byte.
func extractFuncSpan(t *testing.T, source, file, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^[ \t]*func[ \t]+` + regexp.QuoteMeta(name) + `[ \t]*\(`)
	loc := re.FindStringIndex(source)
	require.NotNilf(t, loc, "function %s not found in %s", name, file)
	start := loc[0]

	relBrace := strings.IndexByte(source[start:], '{')
	require.GreaterOrEqualf(t, relBrace, 0, "no body found for function %s in %s", name, file)
	braceStart := start + relBrace

	depth := 0
	end := -1
	for i := braceStart; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end != -1 {
			break
		}
	}
	require.NotEqualf(t, -1, end, "unbalanced braces reading function %s's body in %s", name, file)
	return source[start : end+1]
}

// extractConstSpan pulls a single top-level `const NAME = ...` declaration
// (one physical line, per this file family's own convention -- none of the
// consts covered by this test span multiple lines) out of an ATLX source
// file. A trailing same-line `//` comment, if any, is stripped: it documents
// the constant, it is not part of its value.
func extractConstSpan(t *testing.T, source, file, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^[ \t]*const[ \t]+` + regexp.QuoteMeta(name) + `[ \t]*=.*$`)
	line := re.FindString(source)
	require.NotEmptyf(t, line, "const %s not found in %s", name, file)
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = line[:idx]
	}
	return line
}

// normalizeATLXSpan collapses ALL whitespace (including newlines and
// indentation, and CRLF vs LF line-ending differences -- some of these files
// use one, some the other) in an extracted span down to single spaces
// between tokens, WITHOUT reordering or renaming anything: strings.Fields
// already splits on any run of Unicode whitespace and drops empty strings, so
// this only ever changes layout, never content.
func normalizeATLXSpan(span string) string {
	return strings.Join(strings.Fields(span), " ")
}

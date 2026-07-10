package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAVMToolingDocsStaySynchronizedWithImplementation(t *testing.T) {
	languagePath := filepath.Join("architecture", "language-spec.md")
	language, err := os.ReadFile(languagePath)
	if err != nil {
		t.Fatalf("read language-spec.md: %v", err)
	}
	languageText := string(language)
	for _, want := range []string{
		"Canonical Surface Grammar",
		"getter selectors are derived from the canonical getter name",
		"@storage struct",
		"@message(0x",
		"type InternalMsg =",
		"union matching MUST be exhaustive",
		"is a parse error",
		"stable contract language track",
		"grammar and ABI rules",
		"Symbol Resolution",
		"@impure",
		"Top-level helper functions",
		"wallet action",
		"local bindings MUST use one of these two forms only in stable-track source",
		"canonical serialization MUST be deterministic across compiler runs",
		"wallet action MUST remain stable across compilation",
	} {
		if !strings.Contains(languageText, want) {
			t.Fatalf("language-spec.md should contain %q", want)
		}
	}

	matrixPath := filepath.Join("architecture", "avm-tooling.md")
	matrix, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatalf("read avm-tooling.md: %v", err)
	}
	matrixText := string(matrix)
	for _, want := range []string{
		"Traceability Matrix",
		"avm compile",
		"avm inspect",
		"avm disasm",
		"avm gas",
		"avm test",
		"avm selectors",
		"avm lsp",
		"examples/avm/counter_should_be.atlx",
		"examples/avm/token/token_master.atlx",
		"test-report.json",
		"Top-level JSON keys",
	} {
		if !strings.Contains(matrixText, want) {
			t.Fatalf("avm-tooling.md should contain %q", want)
		}
	}

	avmDocPath := filepath.Join("architecture", "avm.md")
	avmDoc, err := os.ReadFile(avmDocPath)
	if err != nil {
		t.Fatalf("read avm.md: %v", err)
	}
	avmText := string(avmDoc)
	for _, want := range []string{
		"disassembler",
		"gas profiler",
		"avm test",
		"developer CLI",
		"production wiring",
	} {
		if !strings.Contains(avmText, want) {
			t.Fatalf("avm.md should contain %q", want)
		}
	}

	gatesPath := filepath.Join("public-testnet-production-gates.md")
	gates, err := os.ReadFile(gatesPath)
	if err != nil {
		t.Fatalf("read public-testnet-production-gates.md: %v", err)
	}
	gatesText := string(gates)
	for _, want := range []string{
		"AVM developer tooling exists",
		"AVM remains non-production",
		"go test ./x/aetravm/compiler ./x/aetravm/avm ./x/aetravm/async ./cmd/l1d/cmd",
	} {
		if !strings.Contains(gatesText, want) {
			t.Fatalf("public-testnet-production-gates.md should contain %q", want)
		}
	}
}

package module

import (
	"bytes"
	"fmt"
	"testing"
)

func FuzzVerifierMatchesReferenceModel(f *testing.F) {
	f.Add(encodeModule(makeValidModule()))
	f.Add([]byte{})
	f.Add([]byte("bad"))
	f.Add(append(encodeModule(makeValidModule())[:8], []byte("corrupt")...))

	f.Fuzz(func(t *testing.T, data []byte) {
		verifier, err := NewVerifier(DefaultVerifierParams())
		if err != nil {
			t.Fatal(err)
		}

		got, err := verifier.Verify(data)
		if err != nil {
			t.Fatalf("Verify returned unexpected error: %v", err)
		}
		want := referenceVerify(t, verifier, data)

		if got.Passed != want.Passed {
			t.Fatalf("reference mismatch: passed=%v want=%v", got.Passed, want.Passed)
		}
		if got.ErrorMessage != want.ErrorMessage {
			t.Fatalf("reference mismatch: error=%q want=%q", got.ErrorMessage, want.ErrorMessage)
		}
		if got.ErrorCode != want.ErrorCode {
			t.Fatalf("reference mismatch: error code=%d want=%d", got.ErrorCode, want.ErrorCode)
		}
		if got.Passed {
			if !bytes.Equal(got.ModuleHash, want.ModuleHash) {
				t.Fatalf("module hash mismatch: %x want %x", got.ModuleHash, want.ModuleHash)
			}
			if !bytes.Equal(got.CFGHash, want.CFGHash) {
				t.Fatalf("cfg hash mismatch: %x want %x", got.CFGHash, want.CFGHash)
			}
			if got.AnalyzedStackBound != want.AnalyzedStackBound {
				t.Fatalf("stack bound mismatch: %d want %d", got.AnalyzedStackBound, want.AnalyzedStackBound)
			}
			if got.TrustLevel != want.TrustLevel {
				t.Fatalf("trust level mismatch: %v want %v", got.TrustLevel, want.TrustLevel)
			}
			if got.ABICompatibility != want.ABICompatibility {
				t.Fatalf("abi compatibility mismatch: %v want %v", got.ABICompatibility, want.ABICompatibility)
			}
		}
	})
}

func TestVerifierCorpusMatchesReferenceModel(t *testing.T) {
	verifier, err := NewVerifier(DefaultVerifierParams())
	if err != nil {
		t.Fatal(err)
	}

	cases := [][]byte{
		encodeModule(makeValidModule()),
		{},
		[]byte("bad"),
		append(encodeModule(makeValidModule())[:12], []byte("broken")...),
	}
	for i, data := range cases {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			got, err := verifier.Verify(data)
			if err != nil {
				t.Fatalf("Verify returned unexpected error: %v", err)
			}
			want := referenceVerify(t, verifier, data)
			if got.Passed != want.Passed || got.ErrorMessage != want.ErrorMessage {
				t.Fatalf("reference mismatch: got=%+v want=%+v", got, want)
			}
		})
	}
}

func referenceVerify(t *testing.T, verifier *Verifier, data []byte) VerificationResult {
	t.Helper()
	if len(data) == 0 {
		return verifier.failWithCode(0, "empty module data")
	}

	mod, err := verifier.decode(data)
	if err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	if err := verifier.validateMagic(mod); err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	if err := verifier.validateVersion(mod); err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	if err := verifier.validateCodeSize(mod); err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	if err := verifier.validateImports(mod); err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	if err := verifier.validateExports(mod); err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	cfg, err := verifier.buildCFG(mod.Instructions)
	if err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	bounds, err := verifier.analyzeStackBounds(mod.Instructions)
	if err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	if bounds.MaxDepth > int(verifier.params.MaxStackDepth) {
		return verifier.failWithMessage(0, fmt.Sprintf("stack overflow: max depth %d exceeds limit %d", bounds.MaxDepth, verifier.params.MaxStackDepth))
	}
	if err := verifier.validateDependencyDAG(mod); err != nil {
		return verifier.failWithMessage(0, err.Error())
	}
	return VerificationResult{
		ModuleHash:         verifier.computeModuleHash(data),
		VerifierVersion:    VerifierVersion,
		Passed:             true,
		ErrorCode:          0,
		AnalyzedStackBound: uint32(bounds.MaxDepth),
		CFGHash:            verifier.computeCFGHash(cfg),
		TrustLevel:         Verified,
		DependencyHashes:   mod.DependencyHashes,
		ABICompatibility:   true,
	}
}

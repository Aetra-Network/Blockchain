package compiler

import (
	"strings"
	"testing"
)

// TestUnicodeOperatorGlyphsLexAsAsciiTwins guards the lexer's acceptance of the
// four "pretty" operator glyphs the Aetralis editor extension live-substitutes
// (ecosystem/extension/src/unicodeSubstitution.js): ≠ ⇒ ≤ ≥. Each must lex to
// the IDENTICAL token (kind + canonical ASCII text) as its ASCII digraph, and a
// contract written with the glyphs must compile to byte-identical
// Module/Manifest/StateInit hashes as its ASCII twin (determinism).
func TestUnicodeOperatorGlyphsLexAsAsciiTwins(t *testing.T) {
	for _, pair := range [][2]string{{"!=", "≠"}, {"=>", "⇒"}, {"<=", "≤"}, {">=", "≥"}} {
		asciiTok, err := newLexer("t.atlx", pair[0]).nextToken()
		if err != nil {
			t.Fatalf("lex %q: %v", pair[0], err)
		}
		glyphTok, err := newLexer("t.atlx", pair[1]).nextToken()
		if err != nil {
			t.Fatalf("lex %q: %v", pair[1], err)
		}
		if asciiTok.kind != glyphTok.kind || asciiTok.text != glyphTok.text {
			t.Fatalf("glyph %q lexes as (%v,%q), want (%v,%q) from %q",
				pair[1], glyphTok.kind, glyphTok.text, asciiTok.kind, asciiTok.text, pair[0])
		}
		// The glyph must not swallow following runes (advance counts runes).
		l := newLexer("t.atlx", "a"+pair[1]+"b")
		_, _ = l.nextToken() // a
		op, _ := l.nextToken()
		tail, _ := l.nextToken()
		if op.kind != asciiTok.kind || tail.text != "b" {
			t.Fatalf("glyph %q mis-advanced: op=%v tail=%q", pair[1], op.kind, tail.text)
		}
	}
}

func TestUnicodeContractCompilesIdenticallyToAsciiTwin(t *testing.T) {
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	ascii := counterSource
	glyph := strings.ReplaceAll(ascii, "=>", "⇒")
	if glyph == ascii {
		t.Skip("counterSource has no => to substitute")
	}
	a, err := c.Compile([]byte(ascii))
	if err != nil {
		t.Fatalf("ascii compile: %v", err)
	}
	g, err := c.Compile([]byte(glyph))
	if err != nil {
		t.Fatalf("glyph compile: %v", err)
	}
	if a.ModuleHash != g.ModuleHash || a.ManifestHash != g.ManifestHash || a.StateInitHash != g.StateInitHash {
		t.Fatal("unicode-glyph source produced different canonical hashes than its ASCII twin")
	}
}

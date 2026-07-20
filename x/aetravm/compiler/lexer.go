package compiler

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenNumber
	tokenString
	tokenLBrace
	tokenRBrace
	tokenAt
	tokenLParen
	tokenRParen
	tokenComma
	tokenColon
	tokenSemicolon
	tokenArrow
	tokenFatArrow
	tokenEqual
	tokenLess
	tokenGreater
	tokenQuestion
	tokenQuestionQuestion
	tokenPlus
	tokenMinus
	tokenStar
	tokenSlash
	tokenPercent
	tokenAmpersand
	tokenCaret
	tokenTilde
	tokenPlusEqual
	tokenMinusEqual
	tokenLessLess
	tokenGreaterGreater
	tokenDot
	tokenDotDot
	tokenEqualEqual
	tokenBangEqual
	tokenLessEqual
	tokenGreaterEqual
	tokenSpaceship
	tokenAndAnd
	tokenOrOr
	tokenBang
	tokenPipe
	tokenLBracket
	tokenRBracket
	// tokenColonColon is '::', the generic-instantiation turbofish marker
	// (AVM generics v1 design, revised §1.1). Introduced alongside this
	// design: no two-colon sequence was ever a valid token before (`:` had
	// no multi-character lookahead, unlike every other multi-char operator
	// below), so every ATLX source that contained "::" was already an
	// unconditional parse error — repurposing it here changes no existing
	// program's meaning.
	tokenColonColon
)

type token struct {
	kind tokenKind
	text string
	pos  Position
}

type lexer struct {
	file   string
	src    string
	offset int
	line   int
	col    int
}

func newLexer(file, src string) *lexer {
	if strings.HasPrefix(src, "\ufeff") {
		src = strings.TrimPrefix(src, "\ufeff")
	}
	return &lexer{file: file, src: src, line: 1, col: 1}
}

func (l *lexer) nextToken() (token, error) {
	l.skipSpaceAndComments()
	if l.offset >= len(l.src) {
		return token{kind: tokenEOF, pos: Position{File: l.file, Line: l.line, Column: l.col}}, nil
	}
	start := Position{File: l.file, Line: l.line, Column: l.col}
	r, size := utf8.DecodeRuneInString(l.src[l.offset:])
	switch r {
	case '{':
		l.advance(size)
		return token{kind: tokenLBrace, text: "{", pos: start}, nil
	case '}':
		l.advance(size)
		return token{kind: tokenRBrace, text: "}", pos: start}, nil
	case '@':
		l.advance(size)
		return token{kind: tokenAt, text: "@", pos: start}, nil
	case '(':
		l.advance(size)
		return token{kind: tokenLParen, text: "(", pos: start}, nil
	case ')':
		l.advance(size)
		return token{kind: tokenRParen, text: ")", pos: start}, nil
	case ',':
		l.advance(size)
		return token{kind: tokenComma, text: ",", pos: start}, nil
	case ':':
		if strings.HasPrefix(l.src[l.offset:], "::") {
			l.advance(2)
			return token{kind: tokenColonColon, text: "::", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenColon, text: ":", pos: start}, nil
	case ';':
		l.advance(size)
		return token{kind: tokenSemicolon, text: ";", pos: start}, nil
	case '=':
		if strings.HasPrefix(l.src[l.offset:], "=>") {
			l.advance(2)
			return token{kind: tokenFatArrow, text: "=>", pos: start}, nil
		}
		if strings.HasPrefix(l.src[l.offset:], "==") {
			l.advance(2)
			return token{kind: tokenEqualEqual, text: "==", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenEqual, text: "=", pos: start}, nil
	case '<':
		if strings.HasPrefix(l.src[l.offset:], "<=>") {
			l.advance(3)
			return token{kind: tokenSpaceship, text: "<=>", pos: start}, nil
		}
		if strings.HasPrefix(l.src[l.offset:], "<=") {
			l.advance(2)
			return token{kind: tokenLessEqual, text: "<=", pos: start}, nil
		}
		if strings.HasPrefix(l.src[l.offset:], "<<") {
			l.advance(2)
			return token{kind: tokenLessLess, text: "<<", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenLess, text: "<", pos: start}, nil
	case '>':
		if strings.HasPrefix(l.src[l.offset:], ">=") {
			l.advance(2)
			return token{kind: tokenGreaterEqual, text: ">=", pos: start}, nil
		}
		if strings.HasPrefix(l.src[l.offset:], ">>") {
			l.advance(2)
			return token{kind: tokenGreaterGreater, text: ">>", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenGreater, text: ">", pos: start}, nil
	case '?':
		if strings.HasPrefix(l.src[l.offset:], "??") {
			l.advance(2)
			return token{kind: tokenQuestionQuestion, text: "??", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenQuestion, text: "?", pos: start}, nil
	case '+':
		if strings.HasPrefix(l.src[l.offset:], "+=") {
			l.advance(2)
			return token{kind: tokenPlusEqual, text: "+=", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenPlus, text: "+", pos: start}, nil
	case '-':
		if strings.HasPrefix(l.src[l.offset:], "-=") {
			l.advance(2)
			return token{kind: tokenMinusEqual, text: "-=", pos: start}, nil
		}
		if strings.HasPrefix(l.src[l.offset:], "->") {
			l.advance(2)
			return token{kind: tokenArrow, text: "->", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenMinus, text: "-", pos: start}, nil
	case '*':
		l.advance(size)
		return token{kind: tokenStar, text: "*", pos: start}, nil
	case '/':
		l.advance(size)
		return token{kind: tokenSlash, text: "/", pos: start}, nil
	case '%':
		l.advance(size)
		return token{kind: tokenPercent, text: "%", pos: start}, nil
	case '.':
		if strings.HasPrefix(l.src[l.offset:], "..") {
			l.advance(2)
			return token{kind: tokenDotDot, text: "..", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenDot, text: ".", pos: start}, nil
	case '!':
		if strings.HasPrefix(l.src[l.offset:], "!=") {
			l.advance(2)
			return token{kind: tokenBangEqual, text: "!=", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenBang, text: "!", pos: start}, nil
	case '&':
		if strings.HasPrefix(l.src[l.offset:], "&&") {
			l.advance(2)
			return token{kind: tokenAndAnd, text: "&&", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenAmpersand, text: "&", pos: start}, nil
	case '|':
		if strings.HasPrefix(l.src[l.offset:], "||") {
			l.advance(2)
			return token{kind: tokenOrOr, text: "||", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenPipe, text: "|", pos: start}, nil
	case '^':
		l.advance(size)
		return token{kind: tokenCaret, text: "^", pos: start}, nil
	case '~':
		l.advance(size)
		return token{kind: tokenTilde, text: "~", pos: start}, nil
	case '[':
		l.advance(size)
		return token{kind: tokenLBracket, text: "[", pos: start}, nil
	case ']':
		l.advance(size)
		return token{kind: tokenRBracket, text: "]", pos: start}, nil
	// Unicode operator aliases. The Aetralis editor extension live-substitutes
	// these four ASCII digraphs for their math glyphs as you type (see
	// ecosystem/extension/src/unicodeSubstitution.js). Accept the glyphs and
	// emit the IDENTICAL token (same kind, canonical ASCII text) as the ASCII
	// form so a Unicode source and its ASCII twin lex to the same token stream
	// — and therefore compile to identical Module/Manifest/StateInit hashes.
	// Only reached in operator position: comments are consumed by
	// skipSpaceAndComments and string bodies by scanString, so glyphs inside
	// either are preserved verbatim and never remapped.
	// advance() counts RUNES, not bytes; each glyph below is a single rune, so
	// advance(1) — using size (byte length 3) would wrongly skip the next two
	// runes.
	case '≠': // U+2260 -> "!="
		l.advance(1)
		return token{kind: tokenBangEqual, text: "!=", pos: start}, nil
	case '⇒': // U+21D2 -> "=>"
		l.advance(1)
		return token{kind: tokenFatArrow, text: "=>", pos: start}, nil
	case '≤': // U+2264 -> "<="
		l.advance(1)
		return token{kind: tokenLessEqual, text: "<=", pos: start}, nil
	case '≥': // U+2265 -> ">="
		l.advance(1)
		return token{kind: tokenGreaterEqual, text: ">=", pos: start}, nil
	case '"':
		return l.scanString()
	default:
		if isASCIIDigit(r) {
			return l.scanNumber()
		}
		if isIdentStart(r) {
			return l.scanIdent()
		}
		return token{}, fmt.Errorf("unexpected character %q at %s", r, start)
	}
}

func (l *lexer) scanIdent() (token, error) {
	start := Position{File: l.file, Line: l.line, Column: l.col}
	begin := l.offset
	for l.offset < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.offset:])
		if !isIdentPart(r) {
			break
		}
		l.advance(size)
	}
	return token{kind: tokenIdent, text: l.src[begin:l.offset], pos: start}, nil
}

func (l *lexer) scanNumber() (token, error) {
	start := Position{File: l.file, Line: l.line, Column: l.col}
	begin := l.offset
	if strings.HasPrefix(strings.ToLower(l.src[l.offset:]), "0x") {
		l.advance(2)
		for l.offset < len(l.src) {
			r, size := utf8.DecodeRuneInString(l.src[l.offset:])
			if !isASCIIHexDigit(r) {
				break
			}
			l.advance(size)
		}
		return token{kind: tokenNumber, text: l.src[begin:l.offset], pos: start}, nil
	}
	for l.offset < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.offset:])
		if !isASCIIDigit(r) {
			break
		}
		l.advance(size)
	}
	return token{kind: tokenNumber, text: l.src[begin:l.offset], pos: start}, nil
}

func (l *lexer) scanString() (token, error) {
	start := Position{File: l.file, Line: l.line, Column: l.col}
	l.advance(1)
	var b strings.Builder
	for l.offset < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.offset:])
		if r == '"' {
			l.advance(size)
			return token{kind: tokenString, text: b.String(), pos: start}, nil
		}
		if r == '\\' {
			l.advance(size)
			if l.offset >= len(l.src) {
				break
			}
			esc, escSize := utf8.DecodeRuneInString(l.src[l.offset:])
			switch esc {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				return token{}, fmt.Errorf("unsupported escape \\%c at %s", esc, Position{File: l.file, Line: l.line, Column: l.col})
			}
			l.advance(escSize)
			continue
		}
		b.WriteRune(r)
		l.advance(size)
	}
	return token{}, fmt.Errorf("unterminated string at %s", start)
}

func (l *lexer) skipSpaceAndComments() {
	for l.offset < len(l.src) {
		if strings.HasPrefix(l.src[l.offset:], "//") {
			for l.offset < len(l.src) {
				r, size := utf8.DecodeRuneInString(l.src[l.offset:])
				l.advance(size)
				if r == '\n' {
					break
				}
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(l.src[l.offset:])
		if isASCIISpace(r) {
			l.advance(size)
			continue
		}
		break
	}
}

func (l *lexer) advance(n int) {
	for i := 0; i < n && l.offset < len(l.src); i++ {
		r, size := utf8.DecodeRuneInString(l.src[l.offset:])
		l.offset += size
		if r == '\n' {
			l.line++
			l.col = 1
			continue
		}
		l.col++
	}
}

// Identifiers are ASCII-only: the first character is a latin letter or an
// underscore, the rest are latin letters, digits, or underscores. A digit can
// never start an identifier (digit-leading input lexes as a number).
func isIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
}

// The language surface is ASCII-only. These helpers replace unicode.IsDigit /
// unicode.IsSpace so the lexer does not accept non-ASCII digits or spaces and
// stays independent of the Go unicode tables version.
func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isASCIIHexDigit(r rune) bool {
	return isASCIIDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isASCIISpace(r rune) bool {
	switch r {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

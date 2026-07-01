package compiler

import (
	"fmt"
	"strings"
	"unicode"
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
	tokenLParen
	tokenRParen
	tokenComma
	tokenColon
	tokenSemicolon
	tokenArrow
	tokenEqual
	tokenLess
	tokenGreater
	tokenQuestion
	tokenPlus
	tokenMinus
	tokenDot
	tokenDotDot
	tokenEqualEqual
	tokenBangEqual
	tokenLessEqual
	tokenGreaterEqual
	tokenAndAnd
	tokenOrOr
	tokenLBracket
	tokenRBracket
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
		l.advance(size)
		return token{kind: tokenColon, text: ":", pos: start}, nil
	case ';':
		l.advance(size)
		return token{kind: tokenSemicolon, text: ";", pos: start}, nil
	case '=':
		if strings.HasPrefix(l.src[l.offset:], "==") {
			l.advance(2)
			return token{kind: tokenEqualEqual, text: "==", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenEqual, text: "=", pos: start}, nil
	case '<':
		if strings.HasPrefix(l.src[l.offset:], "<=") {
			l.advance(2)
			return token{kind: tokenLessEqual, text: "<=", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenLess, text: "<", pos: start}, nil
	case '>':
		if strings.HasPrefix(l.src[l.offset:], ">=") {
			l.advance(2)
			return token{kind: tokenGreaterEqual, text: ">=", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenGreater, text: ">", pos: start}, nil
	case '?':
		l.advance(size)
		return token{kind: tokenQuestion, text: "?", pos: start}, nil
	case '+':
		l.advance(size)
		return token{kind: tokenPlus, text: "+", pos: start}, nil
	case '-':
		if strings.HasPrefix(l.src[l.offset:], "->") {
			l.advance(2)
			return token{kind: tokenArrow, text: "->", pos: start}, nil
		}
		l.advance(size)
		return token{kind: tokenMinus, text: "-", pos: start}, nil
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
		return token{}, fmt.Errorf("unexpected character %q at %s", r, start)
	case '&':
		if strings.HasPrefix(l.src[l.offset:], "&&") {
			l.advance(2)
			return token{kind: tokenAndAnd, text: "&&", pos: start}, nil
		}
		return token{}, fmt.Errorf("unexpected character %q at %s", r, start)
	case '|':
		if strings.HasPrefix(l.src[l.offset:], "||") {
			l.advance(2)
			return token{kind: tokenOrOr, text: "||", pos: start}, nil
		}
		return token{}, fmt.Errorf("unexpected character %q at %s", r, start)
	case '[':
		l.advance(size)
		return token{kind: tokenLBracket, text: "[", pos: start}, nil
	case ']':
		l.advance(size)
		return token{kind: tokenRBracket, text: "]", pos: start}, nil
	case '"':
		return l.scanString()
	default:
		if unicode.IsDigit(r) {
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
	for l.offset < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.offset:])
		if !unicode.IsDigit(r) {
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
		if unicode.IsSpace(r) {
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

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

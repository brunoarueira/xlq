package filter

import (
	"fmt"
	"strings"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokDot
	tokLBracket
	tokRBracket
	tokPipe
	tokEquals
	tokColon
	tokIdent
	tokString
	tokInt
	// tokNumber is a decimal value literal (ADR-0014's NUMBER, e.g.
	// "5.5") - distinct from tokInt so a bracket index like
	// ".Sheet1[5]" keeps lexing exactly as it always has, unaffected
	// by assignment values gaining their own, separate numeric form.
	tokNumber
)

// token is one lexical token. text is the raw identifier text, the
// unescaped string value, or the integer/decimal literal text
// (including a leading "-", if present) - empty for punctuation
// tokens.
type token struct {
	kind tokenKind
	text string
	pos  int // byte offset into the source, for error messages
}

// describe returns a human-readable form of t for error messages.
func (t token) describe() string {
	switch t.kind {
	case tokEOF:
		return "end of input"
	case tokDot:
		return `"."`
	case tokLBracket:
		return `"["`
	case tokRBracket:
		return `"]"`
	case tokPipe:
		return `"|"`
	case tokEquals:
		return `"="`
	case tokColon:
		return `":"`
	default:
		return fmt.Sprintf("%q", t.text)
	}
}

// lex tokenizes a filter string per docs/adr/0008-filter-grammar-v1.md.
// The returned slice always ends with a tokEOF token.
func lex(input string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(input) {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '.':
			toks = append(toks, token{kind: tokDot, pos: i})
			i++
		case c == '[':
			toks = append(toks, token{kind: tokLBracket, pos: i})
			i++
		case c == ']':
			toks = append(toks, token{kind: tokRBracket, pos: i})
			i++
		case c == '|':
			toks = append(toks, token{kind: tokPipe, pos: i})
			i++
		case c == '=':
			toks = append(toks, token{kind: tokEquals, pos: i})
			i++
		case c == ':':
			toks = append(toks, token{kind: tokColon, pos: i})
			i++
		case c == '"':
			tok, next, err := lexString(input, i)
			if err != nil {
				return nil, err
			}
			toks, i = append(toks, tok), next
		case c == '-' || isDigit(c):
			tok, next, err := lexNumber(input, i)
			if err != nil {
				return nil, err
			}
			toks, i = append(toks, tok), next
		case isIdentStart(c):
			tok, next := lexIdent(input, i)
			toks, i = append(toks, tok), next
		default:
			return nil, fmt.Errorf("filter: unexpected character %q at position %d", c, i)
		}
	}
	return append(toks, token{kind: tokEOF, pos: len(input)}), nil
}

func lexIdent(input string, start int) (token, int) {
	i := start + 1
	for i < len(input) && isIdentContinue(input[i]) {
		i++
	}
	return token{kind: tokIdent, text: input[start:i], pos: start}, i
}

// lexNumber consumes an optional leading "-" followed by one or more
// digits, producing tokInt - unless that's immediately followed by a
// "." and at least one more digit, in which case the "." and its
// digits are consumed too, producing tokNumber instead (ADR-0014's
// NUMBER: the fraction is mandatory, so a bare "5" always stays
// tokInt, exactly as it did before tokNumber existed - a trailing "."
// with no digit after it, as in ".Sheet1[5]." or just "5.", is left
// untouched, to be re-lexed separately as the ordinary path-anchor
// token).
func lexNumber(input string, start int) (token, int, error) {
	i := start
	if input[i] == '-' {
		i++
	}
	digitsStart := i
	for i < len(input) && isDigit(input[i]) {
		i++
	}
	if i == digitsStart {
		return token{}, 0, fmt.Errorf("filter: expected digits after %q at position %d", "-", start)
	}
	if i < len(input) && input[i] == '.' && i+1 < len(input) && isDigit(input[i+1]) {
		i++ // consume "."
		for i < len(input) && isDigit(input[i]) {
			i++
		}
		return token{kind: tokNumber, text: input[start:i], pos: start}, i, nil
	}
	return token{kind: tokInt, text: input[start:i], pos: start}, i, nil
}

// lexString consumes a double-quoted, backslash-escaped string literal,
// unescaping it as it goes. Recognized escapes: \" \\ \n \t \r.
func lexString(input string, start int) (token, int, error) {
	i := start + 1 // skip the opening quote
	var sb strings.Builder
	for {
		if i >= len(input) {
			return token{}, 0, fmt.Errorf("filter: unterminated string starting at position %d", start)
		}
		switch c := input[i]; c {
		case '"':
			return token{kind: tokString, text: sb.String(), pos: start}, i + 1, nil
		case '\\':
			if i+1 >= len(input) {
				return token{}, 0, fmt.Errorf("filter: unterminated string starting at position %d", start)
			}
			switch input[i+1] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			default:
				return token{}, 0, fmt.Errorf("filter: invalid escape sequence %q at position %d", "\\"+string(input[i+1]), i)
			}
			i += 2
		default:
			sb.WriteByte(c)
			i++
		}
	}
}

func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isIdentContinue(c byte) bool {
	return isIdentStart(c) || isDigit(c) || c == '_'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

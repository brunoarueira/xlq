package filter

import (
	"fmt"
	"strconv"
)

// Parse parses a filter string into an Expr - either a read-only
// pipeline or an in-place assignment - per
// docs/adr/0008-filter-grammar-v1.md, the grammar correction in
// docs/adr/0009-correct-filter-grammar-ebnf.md, the builtin-step
// addition in docs/adr/0011-header-aware-record-output.md (generalized
// by docs/adr/0022-extensible-builtins.md), the assignment form added
// by docs/adr/0014-in-place-editing.md, the slice form added by
// docs/adr/0020-array-slicing.md, and the comparison form added by
// docs/adr/0024-comparison-operators.md:
//
//	filter       = pipeline | assignment
//	pipeline     = first ( "|" step )*
//	first        = path ( cmp_op operand )?
//	step         = step_operand ( cmp_op operand )?
//	step_operand = path | builtin
//	operand      = path | builtin | value
//	cmp_op       = "==" | "!=" | "<" | ">" | "<=" | ">="
//	builtin      = IDENT ( "(" ( value ( "," value )* )? ")" )?
//	path         = "." ( head tail* )?
//	head         = IDENT | "[" STRING "]" | "[" INT "]" | "[" slice "]"
//	tail         = "." IDENT | "[" STRING "]" | "[" INT "]" | "[" slice "]"
//	slice        = INT? ":" INT?
//	assignment   = path "=" value
//	value        = STRING | NUMBER | INT | "true" | "false" | "null"
//
// assignment is a wholly separate top-level alternative to pipeline,
// never composed with a pipe - an Assign's Target is parsed as one
// path, not a pipeline, and parsing stops right after its value. A
// literal operand is only ever valid on a comparison's right-hand
// side, never as a filter's own first segment - "100 == .A1" isn't
// supported, a deliberate, narrower scope than full symmetry (ADR-0024).
func Parse(input string) (Expr, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: toks}

	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}

	if p.peek().kind == tokEquals {
		p.advance()
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokEOF {
			return nil, fmt.Errorf("filter: unexpected %s at position %d", p.peek().describe(), p.peek().pos)
		}
		return Assign{Target: path, Value: value}, nil
	}

	first, err := p.maybeComparison(path)
	if err != nil {
		return nil, err
	}

	expr := first
	for p.peek().kind == tokPipe {
		p.advance()
		right, err := p.parseStep()
		if err != nil {
			return nil, err
		}
		expr = Pipe{Left: expr, Right: right}
	}
	if p.peek().kind != tokEOF {
		if p.peek().kind == tokIdent && spacedAfterIdent(input, p.tokens, p.pos) {
			return nil, fmt.Errorf("filter: unexpected %s at position %d (a name with spaces needs brackets, e.g. .[\"TOP POSTS\"])", p.peek().describe(), p.peek().pos)
		}
		return nil, fmt.Errorf("filter: unexpected %s at position %d", p.peek().describe(), p.peek().pos)
	}
	return expr, nil
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	return p.tokens[p.pos]
}

// advance returns the current token and moves past it, except at EOF,
// which is never consumed.
func (p *parser) advance() token {
	t := p.tokens[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

// builtinNames are the identifiers parseStep recognizes as a builtin
// reference rather than a malformed path (ADR-0022) - checked here so
// a misspelled name still fails at parse time, with a precise
// position, the same as it always has (when "records" was the only
// name parseStep knew about at all, anything else already fell
// through to parsePath and failed there). internal/eval's own builtin
// dispatch is still where each name's actual argument count/types are
// validated - this set exists only to keep typo-detection at parse
// time, not to duplicate that validation.
var builtinNames = map[string]bool{
	"records":  true,
	"tonumber": true, // #37
	"todate":   true, // #42
}

// parseStep parses one pipeline step after a "|": a path or a builtin
// reference (ADR-0011, generalized by ADR-0022), now optionally
// followed by a comparison against a second operand (ADR-0024). A
// builtin is only valid here - Parse always parses a filter's first
// segment via parsePath, never parseStep, so a builtin alone (with no
// preceding "|") isn't a valid filter on its own.
func (p *parser) parseStep() (Expr, error) {
	var left Expr
	var err error
	if p.peek().kind == tokIdent && builtinNames[p.peek().text] {
		name := p.advance().text
		var args []any
		if args, err = p.parseBuiltinArgs(); err != nil {
			return nil, err
		}
		left = Builtin{Name: name, Args: args}
	} else if left, err = p.parsePath(); err != nil {
		return nil, err
	}
	return p.maybeComparison(left)
}

// cmpTokens maps each comparison token to its operator text (ADR-0024).
var cmpTokens = map[tokenKind]string{
	tokEq: "==", tokNe: "!=", tokLt: "<", tokLe: "<=", tokGt: ">", tokGe: ">=",
}

// maybeComparison checks for a trailing comparison operator after an
// already-parsed left operand (ADR-0024), parsing the right-hand
// operand if one follows. Returns left unchanged if no operator
// follows - this is how both Parse's own first segment and parseStep
// stay optional-comparison, not comparison-required.
func (p *parser) maybeComparison(left Expr) (Expr, error) {
	op, ok := cmpTokens[p.peek().kind]
	if !ok {
		return left, nil
	}
	p.advance()
	right, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	return Comparison{Op: op, Left: left, Right: right}, nil
}

// parseOperand parses a comparison's right-hand operand: a path, a
// builtin reference, or a literal value (ADR-0024) - never another
// comparison (no chaining) and never the first thing in the whole
// filter (Parse always starts from parsePath, not parseOperand).
func (p *parser) parseOperand() (Expr, error) {
	if p.peek().kind == tokDot {
		return p.parsePath()
	}
	if p.peek().kind == tokIdent && builtinNames[p.peek().text] {
		name := p.advance().text
		args, err := p.parseBuiltinArgs()
		if err != nil {
			return nil, err
		}
		return Builtin{Name: name, Args: args}, nil
	}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	return Literal{Value: v}, nil
}

// parseBuiltinArgs parses an optional "(" value ("," value)* ")"
// argument list (ADR-0022). No "(" at all means zero arguments (the
// bare-keyword form, e.g. "records"), not a parse error - "("
// immediately followed by ")" (empty parens) is also zero arguments,
// both forms accepted identically.
func (p *parser) parseBuiltinArgs() ([]any, error) {
	if p.peek().kind != tokLParen {
		return nil, nil
	}
	p.advance()
	var args []any
	if p.peek().kind != tokRParen {
		for {
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			args = append(args, v)
			if p.peek().kind != tokComma {
				break
			}
			p.advance()
		}
	}
	if p.peek().kind != tokRParen {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", ")", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return args, nil
}

func (p *parser) parsePath() (Expr, error) {
	if p.peek().kind != tokDot {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", ".", p.peek().pos, p.peek().describe())
	}
	p.advance()

	var expr Expr = Identity{}

	// The segment immediately after the anchor "." doesn't need one of
	// its own: ".Sheet1" is one dot token followed by one ident token,
	// not two dots. A bracket suffix never needs a leading dot either,
	// with or without a preceding segment.
	switch p.peek().kind {
	case tokIdent:
		expr = Field{Base: expr, Name: p.advance().text}
	case tokLBracket:
		p.advance()
		suffix, err := p.parseBracketSuffix(expr)
		if err != nil {
			return nil, err
		}
		expr = suffix
	default:
		return expr, nil // bare "." - identity, no segments
	}

	// Every segment after the first needs its own leading "." for the
	// ident form; a bracket form still never does.
	for {
		switch p.peek().kind {
		case tokDot:
			p.advance()
			if p.peek().kind != tokIdent {
				return nil, fmt.Errorf("filter: expected a name after \".\" at position %d, found %s", p.peek().pos, p.peek().describe())
			}
			expr = Field{Base: expr, Name: p.advance().text}
		case tokLBracket:
			p.advance()
			suffix, err := p.parseBracketSuffix(expr)
			if err != nil {
				return nil, err
			}
			expr = suffix
		default:
			return expr, nil
		}
	}
}

// parseBracketSuffix parses the STRING, INT, or slice and closing "]"
// of a "[" STRING "]" | "[" INT "]" | "[" slice "]" suffix (ADR-0020
// adds the slice form; the opening "[" has already been consumed.
// Telling a slice from a plain index needs only one token of
// lookahead: an INT immediately followed by "]" is Index, the same as
// always; an INT immediately followed by ":" - or a bare ":" with no
// leading INT at all - is a Slice instead.
func (p *parser) parseBracketSuffix(base Expr) (Expr, error) {
	if p.peek().kind == tokColon {
		return p.parseSlice(base, nil)
	}
	var result Expr
	switch p.peek().kind {
	case tokString:
		result = Field{Base: base, Name: p.advance().text}
	case tokInt:
		tok := p.advance()
		n, err := strconv.Atoi(tok.text)
		if err != nil {
			return nil, fmt.Errorf("filter: invalid integer %q at position %d", tok.text, tok.pos)
		}
		if p.peek().kind == tokColon {
			return p.parseSlice(base, &n)
		}
		result = Index{Base: base, N: n}
	default:
		return nil, fmt.Errorf("filter: expected a string, integer, or slice inside \"[...]\" at position %d, found %s", p.peek().pos, p.peek().describe())
	}
	if p.peek().kind != tokRBracket {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", "]", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return result, nil
}

// parseSlice parses the rest of a "[" INT? ":" INT? "]" suffix
// (ADR-0020): the leading ":" (and, if present, the INT already
// parsed as a candidate start bound by parseBracketSuffix), an
// optional end INT, and the closing "]".
func (p *parser) parseSlice(base Expr, start *int) (Expr, error) {
	p.advance() // ":"
	var end *int
	if p.peek().kind == tokInt {
		tok := p.advance()
		n, err := strconv.Atoi(tok.text)
		if err != nil {
			return nil, fmt.Errorf("filter: invalid integer %q at position %d", tok.text, tok.pos)
		}
		end = &n
	}
	if p.peek().kind != tokRBracket {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", "]", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return Slice{Base: base, Start: start, End: end}, nil
}

// parseValue parses one literal value: STRING, NUMBER, INT, or one of
// the "true"/"false"/"null" keywords. Originally just an assignment's
// right-hand side (ADR-0014), it's reused unchanged by
// parseBuiltinArgs (ADR-0022) for each argument in a builtin call's
// "(" ... ")" list - both positions need exactly the same literal
// grammar, so there's one parser for it, not two. INT (the same token
// a bracket index like ".Sheet1[5]" uses) is accepted here too,
// alongside NUMBER, since NUMBER's fraction is mandatory and so has no
// case of its own for a bare integer - without INT, ".Sheet1.A1 = 5"
// would have no value alternative to parse at all. true/false/null
// already lex as ordinary tokIdent tokens (the same way any builtin
// name does) - recognizing them as literals rather than field names or
// a builtin reference is purely a parser-level distinction, made only
// here. The result is already the Go value both callers need:
// Assign.Value, or one element of a Builtin's Args - string, float64,
// bool, or nil either way.
func (p *parser) parseValue() (any, error) {
	tok := p.peek()
	switch tok.kind {
	case tokString:
		p.advance()
		return tok.text, nil
	case tokNumber, tokInt:
		p.advance()
		f, err := strconv.ParseFloat(tok.text, 64)
		if err != nil {
			return nil, fmt.Errorf("filter: invalid number %q at position %d", tok.text, tok.pos)
		}
		return f, nil
	case tokIdent:
		switch tok.text {
		case "true":
			p.advance()
			return true, nil
		case "false":
			p.advance()
			return false, nil
		case "null":
			p.advance()
			return nil, nil
		}
	}
	return nil, fmt.Errorf("filter: expected a value (a string, number, true, false, or null) at position %d, found %s", tok.pos, tok.describe())
}

// spacedAfterIdent reports whether the token at pos is an identifier
// separated by whitespace from the identifier just before it - the shape
// of an unquoted name with spaces, as opposed to an identifier glued to
// a bracket suffix like .foo[1]bar.
func spacedAfterIdent(input string, toks []token, pos int) bool {
	if pos == 0 || toks[pos-1].kind != tokIdent {
		return false
	}
	// A "true"/"false"/"null" literal operand (ADR-0024) is also a
	// tokIdent, but it was never a field name the bracket-quoting hint
	// below would actually fix - a stray token after one is some other
	// mistake (e.g. "and"/"or", which aren't supported), not a name
	// with spaces (caught in review).
	switch toks[pos-1].text {
	case "true", "false", "null":
		return false
	}
	start := toks[pos].pos
	return start > 0 && (input[start-1] == ' ' || input[start-1] == '\t')
}

package filter

import (
	"fmt"
	"strconv"
)

// Parse parses a filter string into an Expr, per
// docs/adr/0008-filter-grammar-v1.md and the grammar correction in
// docs/adr/0009-correct-filter-grammar-ebnf.md:
//
//	filter   = pipeline
//	pipeline = path ( "|" path )*
//	path     = "." ( head tail* )?
//	head     = IDENT | "[" STRING "]" | "[" INT "]"
//	tail     = "." IDENT | "[" STRING "]" | "[" INT "]"
func Parse(input string) (Expr, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: toks}

	expr, err := p.parsePipeline()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
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

func (p *parser) parsePipeline() (Expr, error) {
	left, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPipe {
		p.advance()
		right, err := p.parsePath()
		if err != nil {
			return nil, err
		}
		left = Pipe{Left: left, Right: right}
	}
	return left, nil
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

// parseBracketSuffix parses the STRING or INT and closing "]" of a
// "[" STRING "]" | "[" INT "]" suffix; the opening "[" has already been
// consumed.
func (p *parser) parseBracketSuffix(base Expr) (Expr, error) {
	var result Expr
	switch p.peek().kind {
	case tokString:
		result = Field{Base: base, Name: p.advance().text}
	case tokInt:
		text := p.advance().text
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("filter: invalid integer %q at position %d", text, p.peek().pos)
		}
		result = Index{Base: base, N: n}
	default:
		return nil, fmt.Errorf("filter: expected a string or integer inside \"[...]\" at position %d, found %s", p.peek().pos, p.peek().describe())
	}
	if p.peek().kind != tokRBracket {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", "]", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return result, nil
}

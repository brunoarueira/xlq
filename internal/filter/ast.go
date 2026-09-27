// Package filter implements xlq's filter language: lexing and parsing a
// filter string into the AST described in
// docs/adr/0008-filter-grammar-v1.md (grammar corrected in
// docs/adr/0009-correct-filter-grammar-ebnf.md). Evaluating that AST
// against a model.Workbook is a later package's job - this one only
// covers syntax.
package filter

// Expr is a parsed filter expression: one of Identity, Field, Index, or
// Pipe. What a Field or Index node actually does (sheet lookup, column or
// A1 cell access, row access, ...) depends on the runtime type it's
// evaluated against - the AST itself is deliberately untyped beyond this,
// per ADR-0008.
type Expr interface {
	exprNode()
}

// Identity is the bare "." filter: returns its input unchanged.
type Identity struct{}

func (Identity) exprNode() {}

// Field is ".Name" or ["Name"]: name-based access on Base. The two forms
// are equivalent - ["Name"] exists for a name that doesn't fit IDENT (has
// spaces, starts with a digit, ...).
type Field struct {
	Base Expr
	Name string
}

func (Field) exprNode() {}

// Index is "[N]": integer indexing on Base.
type Index struct {
	Base Expr
	N    int
}

func (Index) exprNode() {}

// Pipe is "Left | Right": evaluates Left, then evaluates Right with its
// input rebound to Left's result.
type Pipe struct {
	Left, Right Expr
}

func (Pipe) exprNode() {}

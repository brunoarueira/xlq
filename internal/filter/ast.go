// Package filter implements xlq's filter language: lexing and parsing a
// filter string into the AST described in
// docs/adr/0008-filter-grammar-v1.md (grammar corrected in
// docs/adr/0009-correct-filter-grammar-ebnf.md; the Records builtin
// added by docs/adr/0011-header-aware-record-output.md). Evaluating
// that AST against a model.Workbook is a later package's job - this one
// only covers syntax.
package filter

// Expr is a parsed filter expression: one of Identity, Field, Index,
// Pipe, or Records. What a Field or Index node actually does (sheet
// lookup, column or A1 cell access, row access, ...) depends on the
// runtime type it's evaluated against - the AST itself is deliberately
// untyped beyond this, per ADR-0008.
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

// Records is the bare "records" builtin (ADR-0011): transforms whatever
// value flows into it into an array of record objects keyed by header
// name. Like Identity, it has no Base - it isn't a suffix on some other
// expression, it operates on the current value directly. Unlike
// Identity, it's only valid as a pipeline step after a "|": the grammar
// never allows it as a filter's first segment.
type Records struct{}

func (Records) exprNode() {}

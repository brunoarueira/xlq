package filter

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Expr
	}{
		{"identity", ".", Identity{}},
		{"sheet field", ".Sheet1", Field{Identity{}, "Sheet1"}},
		{"cell reference", ".Sheet1.A1", Field{Field{Identity{}, "Sheet1"}, "A1"}},
		{"column letter", ".Sheet1.B", Field{Field{Identity{}, "Sheet1"}, "B"}},
		{"multi-letter column", ".Sheet1.AA", Field{Field{Identity{}, "Sheet1"}, "AA"}},
		{"lowercase cell reference", ".sheet1.a1", Field{Field{Identity{}, "sheet1"}, "a1"}},
		{"underscore in name", ".Sheet_1", Field{Identity{}, "Sheet_1"}},
		{
			"bracket string sheet name",
			`.["Q1 Report"]`,
			Field{Identity{}, "Q1 Report"},
		},
		{
			"bracket string equivalent to dot form",
			`.Sheet1["B2"]`,
			Field{Field{Identity{}, "Sheet1"}, "B2"},
		},
		{
			"bracket int row index",
			".Sheet1[5]",
			Index{Field{Identity{}, "Sheet1"}, 5},
		},
		{
			"bracket int directly on identity",
			".[5]",
			Index{Identity{}, 5},
		},
		{
			"negative bracket int parses (semantic validity is the evaluator's job)",
			".Sheet1[-1]",
			Index{Field{Identity{}, "Sheet1"}, -1},
		},
		{
			"slice with both bounds",
			".Sheet1[1:10]",
			Slice{Field{Identity{}, "Sheet1"}, intPtr(1), intPtr(10)},
		},
		{
			"slice with an omitted start",
			".Sheet1[:10]",
			Slice{Field{Identity{}, "Sheet1"}, nil, intPtr(10)},
		},
		{
			"slice with an omitted end",
			".Sheet1[1:]",
			Slice{Field{Identity{}, "Sheet1"}, intPtr(1), nil},
		},
		{
			"slice with both bounds omitted",
			".Sheet1[:]",
			Slice{Field{Identity{}, "Sheet1"}, nil, nil},
		},
		{
			"slice with negative bounds (semantic validity is the evaluator's job)",
			".Sheet1[-5:-1]",
			Slice{Field{Identity{}, "Sheet1"}, intPtr(-5), intPtr(-1)},
		},
		{
			"slice directly on identity",
			".[1:10]",
			Slice{Identity{}, intPtr(1), intPtr(10)},
		},
		{
			"slice chained with further field access",
			".Sheet1[1:10][0].A1",
			Field{Index{Slice{Field{Identity{}, "Sheet1"}, intPtr(1), intPtr(10)}, 0}, "A1"},
		},
		{
			"chained row then further field access",
			".Sheet1[5].A1",
			Field{Index{Field{Identity{}, "Sheet1"}, 5}, "A1"},
		},
		{
			"pipe",
			".Sheet1 | .A1",
			Pipe{Field{Identity{}, "Sheet1"}, Field{Identity{}, "A1"}},
		},
		{
			"pipe is left-associative",
			".a | .b | .c",
			Pipe{
				Pipe{Field{Identity{}, "a"}, Field{Identity{}, "b"}},
				Field{Identity{}, "c"},
			},
		},
		{
			"records builtin after a pipe",
			".Sheet1 | records",
			Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
		},
		{
			"records followed by further steps",
			".Sheet1 | records | .[0].Impressions",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				Field{Index{Identity{}, 0}, "Impressions"},
			},
		},
		{
			"records chained with itself parses fine (an eval-time error, not a syntax one)",
			".Sheet1 | records | records",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				Builtin{Name: "records"},
			},
		},
		{
			"builtin with empty parens is the same as the bare form (ADR-0022)",
			".Sheet1 | records()",
			Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
		},
		{
			"builtin call with literal arguments of every value type (ADR-0022) - argument count/type isn't validated until eval",
			`.Sheet1 | records("x", 5, true, false, null)`,
			Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records", Args: []any{"x", 5.0, true, false, nil}}},
		},
		{
			"tonumber builtin after a pipe (#37)",
			".Sheet1.A1 | tonumber",
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "tonumber"}},
		},
		{
			"todate builtin with a format argument (#42)",
			`.Sheet1.A1 | todate("M/D/YYYY")`,
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "todate", Args: []any{"M/D/YYYY"}}},
		},
		{
			"comparison between two paths, as a filter's first segment (ADR-0024)",
			".A1 == .B1",
			Comparison{"==", Field{Identity{}, "A1"}, Field{Identity{}, "B1"}},
		},
		{
			"comparison against a literal number",
			".Sheet1.Likes > 100",
			Comparison{">", Field{Field{Identity{}, "Sheet1"}, "Likes"}, Literal{100.0}},
		},
		{
			"comparison against a literal string",
			`.A1 == "hello"`,
			Comparison{"==", Field{Identity{}, "A1"}, Literal{"hello"}},
		},
		{
			"comparison against a literal bool",
			".A1 != true",
			Comparison{"!=", Field{Identity{}, "A1"}, Literal{true}},
		},
		{
			"comparison against a literal null",
			".A1 == null",
			Comparison{"==", Field{Identity{}, "A1"}, Literal{nil}},
		},
		{
			"every comparison operator lexes and parses",
			".A1 <= .B1",
			Comparison{"<=", Field{Identity{}, "A1"}, Field{Identity{}, "B1"}},
		},
		{
			"comparison after a pipe, left side a builtin (ADR-0024)",
			".A1 | tonumber == 5",
			Pipe{Field{Identity{}, "A1"}, Comparison{"==", Builtin{Name: "tonumber"}, Literal{5.0}}},
		},
		{
			"comparison composes with further pipeline steps",
			".Sheet1 | records | .[0].Likes > 100",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				Comparison{">", Field{Index{Identity{}, 0}, "Likes"}, Literal{100.0}},
			},
		},
		{
			"whitespace between tokens is insignificant",
			" .  Sheet1 . A1 ",
			Field{Field{Identity{}, "Sheet1"}, "A1"},
		},
		{
			"escaped string",
			`.["say \"hi\""]`,
			Field{Identity{}, `say "hi"`},
		},
		{
			"escaped backslash and whitespace chars",
			`.["a\\b\tc\nd\re"]`,
			Field{Identity{}, "a\\b\tc\nd\re"},
		},
		{
			"assignment with a string value",
			`.Sheet1.A1 = "hello"`,
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, "hello"},
		},
		{
			"assignment with a decimal number value",
			".Sheet1.A1 = 5.5",
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, 5.5},
		},
		{
			"assignment with a negative decimal value",
			".Sheet1.A1 = -5.5",
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, -5.5},
		},
		{
			"assignment with a bare integer value - NUMBER's fraction is mandatory, so INT fills this case",
			".Sheet1.A1 = 5",
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, 5.0},
		},
		{
			"assignment with a true value",
			".Sheet1.A1 = true",
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, true},
		},
		{
			"assignment with a false value",
			".Sheet1.A1 = false",
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, false},
		},
		{
			"assignment with a null value",
			".Sheet1.A1 = null",
			Assign{Field{Field{Identity{}, "Sheet1"}, "A1"}, nil},
		},
		{
			"assignment target via bracket-string sheet name",
			`.["Q1 Report"].A1 = "x"`,
			Assign{Field{Field{Identity{}, "Q1 Report"}, "A1"}, "x"},
		},
		{
			"assignment target shape isn't validated at parse time - resolution is internal/xlsx's job",
			`.Sheet1[0] = "x"`,
			Assign{Index{Field{Identity{}, "Sheet1"}, 0}, "x"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty input", ""},
		{"missing leading dot", "Sheet1"},
		{"dot followed by digit is not a valid name", ".5"},
		{"dot followed by string is not valid grammar", `."foo"`},
		{"trailing dot with nothing after", ".Sheet1."},
		{"unterminated bracket", ".Sheet1["},
		{"bracket must contain string or int, not bare ident", ".Sheet1[abc]"},
		{"stray closing bracket", ".Sheet1]"},
		{"trailing pipe", ".Sheet1 | "},
		{"leading pipe", "| .Sheet1"},
		{"unterminated string", `.["unterminated`},
		{"lone minus with no digits", ".Sheet1[-]"},
		{"invalid escape sequence", `.["bad \q escape"]`},
		{"unexpected character", ".Sheet1.A1 & .B2"},
		{"mismatched bracket for row index", ".Sheet1[1.5]"},
		{"trailing garbage after valid filter", ".Sheet1 extra"},
		{"records can't be a filter's first segment", "records"},
		{"records can't be chained directly without a pipe", ".Sheet1 | records.Impressions"},
		{"an unrecognized identifier fails as a malformed path, not a builtin (ADR-0022)", ".Sheet1 | recrods"},
		{"builtin call missing its closing paren", ".Sheet1 | records("},
		{"builtin call argument must be a literal, not a path", ".Sheet1 | records(.A1)"},
		{"builtin call with a trailing comma and no following argument", `.Sheet1 | records("x",)`},
		{"builtin call with a comma but no opening paren", ".Sheet1 | records, records"},
		{"assignment with no value after =", ".Sheet1.A1 = "},
		{"assignment value must be a recognized literal, not an arbitrary ident", ".Sheet1.A1 = banana"},
		{"assignment with trailing garbage after the value", `.Sheet1.A1 = "x" extra`},
		{"comparison chaining isn't supported (ADR-0024)", `.A1 == .B1 == .C1`},
		{"a literal can't start a filter's first segment", "100 == .A1"},
		{"a lone \"!\" is never valid (only \"!=\" is)", ".A1 ! .B1"},
		{"a comparison operator with nothing on the right", ".A1 =="},
		{"and/or/not aren't supported (ADR-0024 explicitly defers them)", ".A1 == true and .B1 == false"},
		{"assignment target can't itself contain a pipe", `.Sheet1 | records = "x"`},
		{"unterminated slice", ".Sheet1[1:"},
		{"slice with a string bound", `.Sheet1[1:"x"]`},
		{"slice with a second colon", ".Sheet1[1:2:3]"},
		{"slice bound separated by a stray character", ".Sheet1[1 2:3]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.input); err == nil {
				t.Errorf("Parse(%q): want error, got nil", tc.input)
			}
		})
	}
}

// intPtr returns a pointer to n, for building a Slice's *int bounds in
// test expectations.
func intPtr(n int) *int { return &n }

func TestParseIntegerOverflowReportsIntTokenPosition(t *testing.T) {
	// "9223372036854775808" (one past math.MaxInt64) starts at byte 2,
	// right after ".[". The error must point there, at the integer
	// token itself, not at the "]" that follows it.
	const input = ".[9223372036854775808]"
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("Parse(%q): want error, got nil", input)
	}
	if !strings.Contains(err.Error(), "position 2") {
		t.Errorf("Parse(%q) error = %q, want it to reference position 2 (the integer token)", input, err.Error())
	}
}

func TestParseUnquotedNameWithSpaceSuggestsBrackets(t *testing.T) {
	_, err := Parse(".TOP POSTS")
	if err == nil {
		t.Fatal("Parse(\".TOP POSTS\"): want error, got nil")
	}
	want := `.["TOP POSTS"]`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to suggest %q", err.Error(), want)
	}
}

func TestParseAdjacentIdentAfterBracketHasNoSpaceHint(t *testing.T) {
	for _, input := range []string{`.foo[1]bar`, `.["x"]bar`} {
		_, err := Parse(input)
		if err == nil {
			t.Fatalf("Parse(%q): want error, got nil", input)
		}
		if strings.Contains(err.Error(), "spaces") {
			t.Errorf("Parse(%q) error = %q, want no spaces hint (no whitespace involved)", input, err.Error())
		}
	}
}

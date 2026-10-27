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
			"builtin call with literal arguments of every value type (ADR-0022) - argument count/type isn't validated until eval; tonumber here is just the demo vehicle for the shared literal-value grammar, not a meaningful feature of tonumber itself",
			`.Sheet1.A1 | tonumber("x", 5, true, false, null)`,
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "tonumber", Args: []any{"x", 5.0, true, false, nil}}},
		},
		{
			"records with a headers(f) filter-expression argument (#125, ADR-0036)",
			".Sheet1 | records(headers(snake_case))",
			Pipe{
				Field{Identity{}, "Sheet1"},
				FilterArg{Name: "records", Arg: FilterArg{Name: "headers", Arg: Builtin{Name: "snake_case"}}},
			},
		},
		{
			"headers(f) standalone, not nested in records (#125, ADR-0036)",
			".Sheet1 | headers(snake_case)",
			Pipe{Field{Identity{}, "Sheet1"}, FilterArg{Name: "headers", Arg: Builtin{Name: "snake_case"}}},
		},
		{
			"records' argument isn't restricted to headers(...) at parse time - only at eval time (#125, ADR-0036)",
			".Sheet1 | records(.A1)",
			Pipe{Field{Identity{}, "Sheet1"}, FilterArg{Name: "records", Arg: Field{Identity{}, "A1"}}},
		},
		{
			"records still parses as a comparison's right-hand operand (ADR-0024) - a pre-existing capability #125 must not regress, since parseOperand consults builtinNames directly, not parseStep's own dedicated records branch",
			".Sheet1.A1 == records",
			Comparison{"==", Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "records"}},
		},
		{
			"tonumber builtin after a pipe (#37)",
			".Sheet1.A1 | tonumber",
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "tonumber"}},
		},
		{
			"upper/lower/trim builtins after a pipe (#124, ADR-0033)",
			".Sheet1.A1 | upper",
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "upper"}},
		},
		{
			"snake_case/camel_case builtins after a pipe (#124, ADR-0034)",
			".Sheet1.A1 | snake_case",
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "snake_case"}},
		},
		{
			"replace builtin with two literal arguments (#124, ADR-0035)",
			`.Sheet1.A1 | replace(",", "")`,
			Pipe{Field{Field{Identity{}, "Sheet1"}, "A1"}, Builtin{Name: "replace", Args: []any{",", ""}}},
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
			"\"<\" lexes and parses",
			".A1 < .B1",
			Comparison{"<", Field{Identity{}, "A1"}, Field{Identity{}, "B1"}},
		},
		{
			"\"<=\" lexes and parses",
			".A1 <= .B1",
			Comparison{"<=", Field{Identity{}, "A1"}, Field{Identity{}, "B1"}},
		},
		{
			"\">=\" lexes and parses",
			".A1 >= .B1",
			Comparison{">=", Field{Identity{}, "A1"}, Field{Identity{}, "B1"}},
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
			"map with a field-access argument (ADR-0025)",
			".Sheet1 | records | map(.Likes)",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				FilterArg{Name: "map", Arg: Field{Identity{}, "Likes"}},
			},
		},
		{
			"select with a comparison argument",
			".Sheet1 | records | select(.Likes > 100)",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				FilterArg{Name: "select", Arg: Comparison{">", Field{Identity{}, "Likes"}, Literal{100.0}}},
			},
		},
		{
			"map argument starting from a bare builtin",
			".Sheet1.B | map(tonumber)",
			Pipe{
				Field{Field{Identity{}, "Sheet1"}, "B"},
				FilterArg{Name: "map", Arg: Builtin{Name: "tonumber"}},
			},
		},
		{
			"map argument starting from bare identity",
			".Sheet1.B | map(.)",
			Pipe{
				Field{Field{Identity{}, "Sheet1"}, "B"},
				FilterArg{Name: "map", Arg: Identity{}},
			},
		},
		{
			"select argument is itself a pipe",
			".Sheet1 | records | select(.Likes | tonumber > 100)",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				FilterArg{
					Name: "select",
					Arg: Pipe{
						Field{Identity{}, "Likes"},
						Comparison{">", Builtin{Name: "tonumber"}, Literal{100.0}},
					},
				},
			},
		},
		{
			"nested map/select arguments parse",
			".Sheet1 | records | map(select(.Likes > 100))",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				FilterArg{
					Name: "map",
					Arg:  FilterArg{Name: "select", Arg: Comparison{">", Field{Identity{}, "Likes"}, Literal{100.0}}},
				},
			},
		},
		{
			"sum builtin after a pipe (#38)",
			".Sheet1.B | sum",
			Pipe{Field{Field{Identity{}, "Sheet1"}, "B"}, Builtin{Name: "sum"}},
		},
		{
			"count builtin after a pipe (#38)",
			".Sheet1 | records | count",
			Pipe{Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}}, Builtin{Name: "count"}},
		},
		{
			"avg builtin after a pipe (#38)",
			".Sheet1.B | avg",
			Pipe{Field{Field{Identity{}, "Sheet1"}, "B"}, Builtin{Name: "avg"}},
		},
		{
			"sum composes after map",
			".Sheet1 | records | map(.Likes) | sum",
			Pipe{
				Pipe{
					Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
					FilterArg{Name: "map", Arg: Field{Identity{}, "Likes"}},
				},
				Builtin{Name: "sum"},
			},
		},
		{
			"group_by with a field-access argument (#38)",
			".Sheet1 | records | group_by(.Category)",
			Pipe{
				Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
				FilterArg{Name: "group_by", Arg: Field{Identity{}, "Category"}},
			},
		},
		{
			"group_by composes with map to aggregate each group",
			".Sheet1 | records | group_by(.Category) | map(count)",
			Pipe{
				Pipe{
					Pipe{Field{Identity{}, "Sheet1"}, Builtin{Name: "records"}},
					FilterArg{Name: "group_by", Arg: Field{Identity{}, "Category"}},
				},
				FilterArg{Name: "map", Arg: Builtin{Name: "count"}},
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
		{"records' filter-expression argument missing its closing paren", ".Sheet1 | records("},
		{"records' argument can't start with a bare string literal (#125, ADR-0036)", `.Sheet1 | records("x",)`},
		{"builtin call with a comma but no opening paren", ".Sheet1 | records, records"},
		{"headers with no parentheses at all (#125, ADR-0036)", ".Sheet1 | headers"},
		{"headers with empty parentheses (#125, ADR-0036)", ".Sheet1 | headers()"},
		{"headers isn't valid as a filter's first segment (#125, ADR-0036)", "headers(snake_case)"},
		{"records' argument, when present, can't be omitted mid-call (#125, ADR-0036)", ".Sheet1 | records(headers("},
		{"assignment with no value after =", ".Sheet1.A1 = "},
		{"assignment value must be a recognized literal, not an arbitrary ident", ".Sheet1.A1 = banana"},
		{"assignment with trailing garbage after the value", `.Sheet1.A1 = "x" extra`},
		{"comparison chaining isn't supported (ADR-0024)", `.A1 == .B1 == .C1`},
		{"a literal can't start a filter's first segment", "100 == .A1"},
		{"a lone \"!\" is never valid (only \"!=\" is)", ".A1 ! .B1"},
		{"a comparison operator with nothing on the right", ".A1 =="},
		{"and/or/not aren't supported (ADR-0024 explicitly defers them)", ".A1 == true and .B1 == false"},
		{"map with no parentheses at all", ".Sheet1 | records | map"},
		{"map with empty parentheses", ".Sheet1 | records | map()"},
		{"select with no parentheses at all", ".Sheet1 | records | select"},
		{"select with empty parentheses", ".Sheet1 | records | select()"},
		{"map/select aren't valid as a filter's first segment (ADR-0025)", "map(.A1)"},
		{"map/select aren't valid comparison operands (ADR-0025)", ".A1 == map(.B1)"},
		{"map missing its closing paren", ".Sheet1 | records | map(.Likes"},
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

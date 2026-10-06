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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.input); err == nil {
				t.Errorf("Parse(%q): want error, got nil", tc.input)
			}
		})
	}
}

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

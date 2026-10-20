# 23. Explicit date parsing: `todate(format)`

## Status

Accepted

## Context

Issue #42 (M7, unblocked by #37/ADR-0022's builtins mechanism): a date
like `7/28/2026` is plain text in many exports, and which order its
parts come in is genuinely ambiguous (US month/day/year vs. everyone
else's day/month/year) - there's no way to guess correctly in general,
so (matching this project's standing rule, ADR-0010: never guess a
cell's type from its text) the conversion needs an explicit format
argument from the caller, the same reasoning `tonumber` (#37) already
applied to numeric text.

`todate` fits the mechanism ADR-0022 already built without needing any
grammar change: a builtin taking exactly one literal (string) argument,
same shape as `tonumber` taking zero. What's left to decide is specific
to dates: what language the format argument itself is written in, and
what `todate` actually produces.

## Decision

### Format tokens: Excel/moment.js-style (`YYYY`, `MM`, `DD`, ...), not Go's reference layout or strftime

Three reasonable options existed:

- **Go's own reference-time layout** (`"1/2/2006"`) - no translation
  needed internally, but `1/2/2006` means nothing to anyone who hasn't
  memorized Go's specific reference date; actively confusing to this
  project's actual audience (spreadsheet users, not Go programmers).
- **strftime-style** (`"%m/%d/%Y"`) - a widely-known POSIX standard,
  but still a programmer's convention, not a spreadsheet one.
- **Excel/moment.js-style tokens** (`"M/D/YYYY"`) - the same convention
  Excel's own custom number-format codes already use for displaying a
  date, and the exact form issue #42 and ADR-0022's own context section
  already used as the illustrative example. The most immediately
  recognizable to someone who thinks in spreadsheet terms, not code.

Chosen: the third. Supported tokens, deliberately scoped to exactly
what resolves the issue's own ambiguity - a plain numeric date, not a
general date/time formatting language:

| Token  | Meaning                      | Go layout equivalent |
| ------ | ----------------------------- | --------------------- |
| `YYYY` | 4-digit year                   | `2006`                 |
| `YY`   | 2-digit year                   | `06`                   |
| `MM`   | 2-digit month, zero-padded     | `01`                   |
| `M`    | 1-or-2-digit month             | `1`                    |
| `DD`   | 2-digit day, zero-padded       | `02`                   |
| `D`    | 1-or-2-digit day               | `2`                    |

No month-name (`MMM`/`MMMM`) or time-of-day (`H`/`m`/`s`) tokens - out
of scope for what #42 actually asks for; a natural, separate extension
later if a real need shows up.

### Translation: greedy longest-match tokenizing into a Go layout string, anything else passed through literally

```go
// dateFormatTokens maps each recognized format token to its Go
// reference-time layout equivalent (ADR-0023). Longer tokens are
// listed, and matched, before their shorter prefixes (YYYY before YY,
// MM before M, DD before D) so a tokenizer scanning left to right
// never mistakes "YYYY" for "YY" followed by two literal "Y"
// characters.
var dateFormatTokens = []struct {
	token, layout string
}{
	{"YYYY", "2006"},
	{"YY", "06"},
	{"MM", "01"},
	{"M", "1"},
	{"DD", "02"},
	{"D", "2"},
}

// translateDateFormat converts a todate format string (ADR-0023's own
// token language) into a Go time.Parse layout. Anything not one of
// dateFormatTokens - "/", "-", a literal space, ... - passes through
// unchanged, exactly as Go's own layout strings already treat any
// character that isn't part of the reference time as literal.
func translateDateFormat(format string) string {
	var layout strings.Builder
	for i := 0; i < len(format); {
		matched := false
		for _, tok := range dateFormatTokens {
			if strings.HasPrefix(format[i:], tok.token) {
				layout.WriteString(tok.layout)
				i += len(tok.token)
				matched = true
				break
			}
		}
		if !matched {
			layout.WriteByte(format[i])
			i++
		}
	}
	return layout.String()
}
```

No validation of the format string itself beyond this translation - an
unrecognized token is just literal text the input must match exactly,
and a format that doesn't actually match the real input (too few
tokens, wrong separator, ...) surfaces as `time.Parse`'s own error,
the same way a mismatched Go layout always has. No new error class
needed for "malformed format" as distinct from "input doesn't match
format."

### Output: an RFC3339 string, matching every other date value this grammar already produces

`cellValue` already converts a `model.Date` cell to an RFC3339 string
(`v.Format(time.RFC3339)`, ADR-0008) - `todate` produces the exact same
shape, not a new one, so a converted date composes with everything
downstream (`-o json`, `-o csv`, further field access) exactly like a
date that was already a real Date-kind cell:

```go
func applyToDate(cur any, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("eval: todate takes exactly one argument (a format string), got %d", len(args))
	}
	format, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("eval: todate: format argument must be a string, got %s", describeType(args[0]))
	}
	s, ok := cur.(string)
	if !ok {
		return nil, fmt.Errorf("eval: todate: %s cannot be parsed as a date", describeType(cur))
	}
	t, err := time.Parse(translateDateFormat(format), s)
	if err != nil {
		return nil, fmt.Errorf("eval: todate: %q does not match format %q", s, format)
	}
	return t.Format(time.RFC3339), nil
}
```

`cur` must already be a string - matching `tonumber`'s own precedent,
`null`/a number/a bool are all errors, with no "null propagates"
exception (converting a value is not the same kind of operation as
navigating into one, the same reasoning ADR-0022 already gave for
`tonumber`).

## Consequences

`"todate"` is added to `internal/filter`'s `builtinNames` and as a new
case in `internal/eval`'s `applyBuiltin`, exactly the two-line addition
ADR-0022 designed this mechanism for - no grammar or parser change.
`internal/eval` gains `dateFormatTokens`/`translateDateFormat`/
`applyToDate`, all new, self-contained code with no effect on any
existing operation. A later, genuinely general date-formatting need
(month names, time components, locale-aware parsing) stays out of
scope here and would be its own future decision, not a reason to widen
this one now.

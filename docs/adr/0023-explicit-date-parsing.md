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

| Token  | Meaning                                                |
| ------ | ------------------------------------------------------- |
| `YYYY` | 4-digit year                                             |
| `YY`   | 2-digit year, pivoted: 00-68 -> 2000-2068, 69-99 -> 1969-1999 |
| `MM`   | 2-digit month, zero-padded                               |
| `M`    | 1-or-2-digit month                                       |
| `DD`   | 2-digit day, zero-padded                                 |
| `D`    | 1-or-2-digit day                                         |

No month-name (`MMM`/`MMMM`) or time-of-day (`H`/`m`/`s`) tokens - out
of scope for what #42 actually asks for; a natural, separate extension
later if a real need shows up.

### Parsing: direct token-by-token extraction against the input, never through Go's own `time.Parse` layout

A first draft translated a format string into a Go `time.Parse` layout
(`"YYYY"` → `"2006"`, anything else passed through as literal) and
handed that straight to `time.Parse`. Caught in review, and confirmed
by actually running it: **Go's layout strings have no real escaping
mechanism** - `time.Parse` doesn't distinguish "translated-from-a-token
text" from "literal passthrough text," it just scans the whole layout
for its own recognized reference-time substrings (`"2006"`, `"06"`,
`"Jan"`, `"January"`, `"15"`, `"PM"`, ...) wherever they appear. A
format like `"January/D/YYYY"` - "January" meant as an arbitrary
literal separator, not a month name - would have its `"January"` text
reinterpreted by Go's own parser as the full-month-name directive,
silently accepting an unsupported format rather than matching the
literal text this ADR's own token table says it should.

Fixed by not using `time.Parse` at all: `applyToDate` walks the format
and input strings together, in one pass, consuming digits for each
recognized token and requiring an exact character match for everything
else - "January" (or `"2006"`, `"Jan"`, any other text that happens to
look like a Go directive) is then *always* literal, by construction,
since this code never hands anything to Go's own layout parser to
misinterpret in the first place:

```go
// dateToken describes one recognized format token (ADR-0023): how many
// digits it consumes from the input, and which date component it
// fills. component 'y' (lowercase) marks the 2-digit year, which needs
// the pivot rule below; 'Y' is the plain 4-digit year. Longer tokens
// are listed, and matched, before their shorter prefixes (YYYY before
// YY, MM before M, DD before D) so a left-to-right scan never mistakes
// "YYYY" for "YY" followed by two literal "Y" characters.
type dateToken struct {
	text                 string
	minDigits, maxDigits int
	component            byte
}

var dateTokens = []dateToken{
	{"YYYY", 4, 4, 'Y'},
	{"YY", 2, 2, 'y'},
	{"MM", 2, 2, 'M'},
	{"M", 1, 2, 'M'},
	{"DD", 2, 2, 'D'},
	{"D", 1, 2, 'D'},
}

// parseDate matches format against s one character/token at a time:
// a recognized token consumes 1-2 (or exactly 4, for YYYY) digits from
// s and fills the matching date component; anything else in format
// must match the identical character in s. A two-digit year pivots
// 00-68 to 2000-2068 and 69-99 to 1969-1999 - the same convention
// POSIX strftime's own "%y" uses, picked over Go's own "06" pivot
// (2000-2068/1969-1999 too, as it happens, but inherited implicitly
// rather than chosen) specifically so this ADR states the rule rather
// than leaving it to whatever a particular Go version's layout parser
// happens to do. A format missing a year, month, or day token entirely
// is rejected outright, not silently defaulted (e.g. to year 0) - an
// incomplete date is exactly the kind of ambiguity this builtin exists
// to never guess through. The result is validated via time.Date's own
// calendar rules (comparing the constructed time's fields back against
// what was parsed) rather than accepted as whatever rolls over - a
// nonexistent date like February 30 is a format/input mismatch, not a
// silently-corrected March 1 or 2.
func parseDate(format, s string) (time.Time, error) {
	var year, month, day int
	haveYear, haveMonth, haveDay := false, false, false
	fi, si := 0, 0
	for fi < len(format) {
		matched := false
		for _, tok := range dateTokens {
			if !strings.HasPrefix(format[fi:], tok.text) {
				continue
			}
			start := si
			for si < len(s) && si-start < tok.maxDigits && isDigit(s[si]) {
				si++
			}
			if si-start < tok.minDigits {
				return time.Time{}, fmt.Errorf("expected %d-%d digits for %q in %q at position %d", tok.minDigits, tok.maxDigits, tok.text, s, start)
			}
			n, _ := strconv.Atoi(s[start:si])
			switch tok.component {
			case 'Y':
				year, haveYear = n, true
			case 'y':
				if n <= 68 {
					year = 2000 + n
				} else {
					year = 1900 + n
				}
				haveYear = true
			case 'M':
				month, haveMonth = n, true
			case 'D':
				day, haveDay = n, true
			}
			fi += len(tok.text)
			matched = true
			break
		}
		if matched {
			continue
		}
		if si >= len(s) || s[si] != format[fi] {
			return time.Time{}, fmt.Errorf("%q does not match format %q at format position %d", s, format, fi)
		}
		fi++
		si++
	}
	if si != len(s) {
		return time.Time{}, fmt.Errorf("%q has trailing text %q not consumed by format %q", s, s[si:], format)
	}
	if !haveYear || !haveMonth || !haveDay {
		return time.Time{}, fmt.Errorf("format %q must include a year, month, and day token", format)
	}
	if month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("month %d in %q is out of range", month, s)
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Day() != day || int(t.Month()) != month || t.Year() != year {
		return time.Time{}, fmt.Errorf("%04d-%02d-%02d (from %q) is not a valid date", year, month, day, s)
	}
	return t, nil
}
```

Verified empirically before writing this down, not assumed: `"M/D/YYYY"`
against `"7/28/2026"` and `"D/M/YYYY"` against `"28/7/2026"` both
resolve to the same date; `"M/D/YY"` against `"7/28/26"` and `"7/28/95"`
pivot to 2026 and 1995 respectively; `"January/D/YYYY"` against
`"January/28/2026"` - "January" meant as literal separator text, with
no month token in the format at all - correctly errors ("must include
a ... month ... token"), confirming no Go-layout reinterpretation is
possible; `"M/D"` against `"7/28"` errors for the missing year;
`"M/D/YYYY"` against `"2/30/2026"` errors as an invalid date (February
never has 30 days); trailing text after a full match errors instead of
being silently ignored.

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
	t, err := parseDate(format, s)
	if err != nil {
		return nil, fmt.Errorf("eval: todate: %w", err)
	}
	return t.Format(time.RFC3339), nil
}
```

`cur` must already be a string - matching `tonumber`'s own precedent,
`null`/a number/a bool are all errors, with no "null propagates"
exception (converting a value is not the same kind of operation as
navigating into one, the same reasoning ADR-0022 already gave for
`tonumber`). `parseDate`'s own error is wrapped with `%w`, not
discarded, so a format/input mismatch surfaces its actual diagnostic
(which token or character failed to match, and where) rather than a
generic "didn't work."

## Consequences

`"todate"` is added to `internal/filter`'s `builtinNames` and as a new
case in `internal/eval`'s `applyBuiltin`, exactly the two-line addition
ADR-0022 designed this mechanism for - no grammar or parser change.
`internal/eval` gains `dateTokens`/`parseDate`/`applyToDate`, all new,
self-contained code with no effect on any existing operation, and no
dependency on `time.Parse`'s own layout mechanism at all - deliberately,
given the reinterpretation risk that mechanism turned out to have for
this ADR's own token language. A later, genuinely general
date-formatting need (month names, time components, locale-aware
parsing) stays out of scope here and would be its own future decision,
not a reason to widen this one now.

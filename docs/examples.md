---
layout: page
title: Examples
permalink: /examples.html
---

Real problems people run into with spreadsheet exports, solved in one
`xlq` command. See the [Reference](reference.html) for the full filter
language behind these.

## "My SUM comes out as 0"

Numbers imported from a CSV or API export often land as *text*, not
numbers - one of the most common Excel import complaints, usually "fixed"
with an `ISNUMBER` check, a Paste Special multiply-by-1 trick, or
`VALUE()`. `tonumber` converts explicitly, so the total just works:

```sh
xlq '.Sheet1 | records | map(.Likes | tonumber) | sum' export.xlsx
```

## "Excel silently swapped my day and month"

A date like `7/8/2026` is genuinely ambiguous - US `M/D/YYYY` vs.
everyone else's `D/M/YYYY` - and Excel's locale-based guessing only swaps
the ones where it's ambiguous (day ≤ 12), silently corrupting some rows
and not others. `todate` always takes an explicit format, so nothing gets
guessed:

```sh
xlq '.Sheet1.B2 | todate("M/D/YYYY")' export.xlsx
```

## "My CSV opens as one giant column"

A semicolon-delimited export - common from European-locale systems, where
Windows' decimal-comma regional setting also changes the default list
separator - looks like one unparsed column to anything expecting commas:

```sh
xlq --delimiter ';' '.Sheet1 | records' export.csv
```

## "`awk` mangles my CSV because a field has a comma in it"

`awk -F','` splits on every comma literally, so a quoted field like
`"Portland, OR",97201` silently shifts every column after it. `xlq`'s CSV
reader is quote-aware, so a filter just reads the right column:

```sh
xlq '.Sheet1 | records | select(.Revenue | tonumber > 10000)' report.csv
```

## "I need one sheet out of this workbook as a CSV, no Excel"

A narrow, common enough need that multiple dedicated `xlsx2csv`-style
tools exist just for this:

```sh
xlq --csv '.Q3 Report | records' book.xlsx > q3.csv
```

## "A quick pivot-style total by category, without opening Excel"

```sh
xlq '.Sheet1 | records | group_by(.Category) | map(map(.Revenue | tonumber) | sum)' sales.xlsx
```

Groups by `Category`, then sums `Revenue` within each group - the same
job dedicated CLI pivot tools exist for.

## "How many rows match this condition?"

The kind of thing that's usually a throwaway `=COUNTIF` formula, or
opening the file just to eyeball it:

```sh
xlq -r '.Sheet1 | records | select(.Total | tonumber > 500) | count' orders.xlsx
```

## "My `.txt` file is actually comma-separated"

Export tools routinely mis-name or omit file extensions. `--input-format`
overrides the extension-based guess:

```sh
xlq --input-format csv '.Sheet1' export.txt
```

## "Remove duplicate rows, keeping the first by customer ID"

Plain `sort -u`/`uniq` only dedupes whole lines; deduping by *one column*
normally means a CSV-aware tool. `group_by` + `map` already expresses it:

```sh
xlq '.Sheet1 | records | group_by(.CustomerID) | map(.[0])' customers.xlsx
```

`group_by` keeps each group's records in first-encountered order, so
`.[0]` is the first occurrence for each `CustomerID`.

## "Just let me glance at this spreadsheet in my terminal"

```sh
xlq --table '.Sheet1 | records | .[0:20]' huge_export.xlsx
```

Aligned, readable, no waiting for Excel or LibreOffice to launch.

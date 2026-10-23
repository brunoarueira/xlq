# Demo GIF for docs/examples.md's "awk mangles my CSV" example.
# Run from docs/vhs/fixtures/ (vhs ../quoted-csv.tape).
# Renders to docs/assets/img/quoted-csv.gif.
#
# Narrative: a quoted field with an embedded comma ("Portland, OR")
# would break naive awk -F',' column splitting - xlq's CSV reader is
# quote-aware, so the filter just reads the right column.

Output ../../assets/img/quoted-csv.gif

Require xlq

Set Shell "bash"
Set FontSize 26
Set Width 1200
Set Height 560
Set Theme "Dracula"
Set Padding 24
Set TypingSpeed 40ms

Type "cat report.csv"
Sleep 500ms
Enter
Sleep 2s

Type "xlq '.Sheet1 | records | select(.Revenue | tonumber > 10000)' report.csv"
Sleep 500ms
Enter
Sleep 3s

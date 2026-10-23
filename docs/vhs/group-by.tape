# Demo GIF for docs/examples.md's "quick total by category" example.
# Run from docs/vhs/fixtures/ (vhs ../group-by.tape).
# Renders to docs/assets/img/group-by.gif.
#
# Narrative: group_by + map + sum totals Revenue per Category, then
# the companion command gets the matching category names - since the
# totals come back as an unlabeled array in first-encountered order.
# sales.xlsx stores Revenue as text (the same real-world export quirk
# the hero demo's Likes column has), so tonumber is load-bearing here
# too, not a no-op on already-numeric data.

Output ../../assets/img/group-by.gif

Require xlq

Set Shell "bash"
Set FontSize 26
Set Width 1200
Set Height 560
Set Theme "Dracula"
Set Padding 24
Set TypingSpeed 40ms

Type "xlq '.Sheet1 | records | group_by(.Category) | map(map(.Revenue | tonumber) | sum)' sales.xlsx"
Sleep 500ms
Enter
Sleep 2.5s

Type "xlq '.Sheet1 | records | group_by(.Category) | map(.[0].Category)' sales.xlsx"
Sleep 500ms
Enter
Sleep 3s

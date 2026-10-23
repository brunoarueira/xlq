# Hero GIF for the docs site landing page (docs/index.md).
# Run from docs/vhs/fixtures/ (vhs ../hero.tape), so the recording
# starts directly on the xlq commands - no setup "cd" to hide (VHS's
# Hide/Show didn't actually omit a hidden command's own frames when
# tried here, so the simplest fix is not needing one at all).
# Renders to docs/assets/img/hero.gif.
#
# Narrative: a social export stores "Likes" as text (a real, common
# export quirk) - sum errors clearly rather than silently giving 0,
# and tonumber fixes it in one step.

Output ../../assets/img/hero.gif

Require xlq

Set Shell "bash"
Set FontSize 26
Set Width 1200
Set Height 640
Set Theme "Dracula"
Set Padding 24
Set TypingSpeed 40ms

Type "xlq --table '.Sheet1 | records' social_export.xlsx"
Sleep 500ms
Enter
Sleep 2.5s

Type "xlq '.Sheet1 | records | map(.Likes) | sum' social_export.xlsx"
Sleep 500ms
Enter
Sleep 2.5s

Type "xlq '.Sheet1 | records | map(.Likes | tonumber) | sum' social_export.xlsx"
Sleep 500ms
Enter
Sleep 3s

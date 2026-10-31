package main

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

var shortcuts = []string{
	"j / k       Scroll down / up",
	"gg / G      First / last line",
	"ctrl-d/u    Half-page down / up",
	"ctrl-f/b    Page down / up",
	"ctrl-e/y    Scroll viewport down / up",
	"zt / zz / zb  Align active line",
	"zh / zl     Horizontal scroll",
	"} / {       Paragraph / 5 commits",
	"J / K       Next / previous file",
	"tab         Switch staged / unstaged",
	"space       Stage / unstage file",
	"w           Toggle line wrapping",
	"e           Expand / collapse deleted file",
	"h / l       Scroll horizontally",
	"f           File tree (j/k, h/l, / search)",
	"/           Search diff or history",
	"n / N       Next / previous diff match",
	"o           Open file in $EDITOR",
	"y           Copy file path / commit hash",
	"c           Commit changes",
	"p / P       Push / pull",
	"r           Refresh changes",
	"H           Open commit history",
	"enter       Open selected commit",
	"esc / q     Back from history",
	"?           Show shortcuts",
	"q           Quit",
}

func helpVisibleRows(height int) int { return max(1, height-5) }

func shortcutLines(width int) []string {
	innerWidth := max(1, min(54, width-2)-4)
	var lines []string
	for _, shortcut := range shortcuts {
		if innerWidth < 30 {
			shortcut = strings.Join(strings.Fields(shortcut), " ")
		}
		lines = append(lines, wrapLine(shortcut, innerWidth)...)
	}
	return lines
}

func showHelpDialog(lines []string, width, pos int) {
	if width < 8 || len(lines) < 5 {
		return
	}
	boxWidth := min(54, width-2)
	innerWidth := boxWidth - 4
	entries := shortcutLines(width)
	visible := min(len(entries), helpVisibleRows(len(lines)))
	pos = min(pos, max(0, len(entries)-visible))
	content := []string{
		"┌" + strings.Repeat("─", boxWidth-2) + "┐",
		dialogLine("Keyboard shortcuts", innerWidth),
	}
	for _, shortcut := range entries[pos : pos+visible] {
		content = append(content, dialogLine(shortcut, innerWidth))
	}
	content = append(content, "└"+strings.Repeat("─", boxWidth-2)+"┘")
	left := strings.Repeat(" ", max(0, (width-boxWidth)/2))
	top := max(0, (len(lines)-len(content))/2)
	for i, line := range content {
		lines[top+i] = left + line
	}
}

func showCommitDialog(lines []string, width int, input, message string) {
	boxWidth := min(64, width-2)
	innerWidth := boxWidth - 4
	input = clean(input)
	fieldWidth := innerWidth - 1
	start := max(0, runewidth.StringWidth(input)-fieldWidth)
	visible := sliceWidth(input, start, fieldWidth)
	infoLines := wrapLine(message, innerWidth)
	infoFirst := infoLines[0]
	infoSecond := ""
	if len(infoLines) > 1 {
		infoSecond = infoLines[1]
	}
	content := []string{
		"┌" + strings.Repeat("─", boxWidth-2) + "┐",
		dialogLine("Commit changes", innerWidth),
		dialogLine("", innerWidth),
		dialogLineWithCursor(visible, innerWidth),
		dialogLine(infoFirst, innerWidth),
		dialogLine(infoSecond, innerWidth),
		dialogLine("↵ commit  esc exit", innerWidth),
		"└" + strings.Repeat("─", boxWidth-2) + "┘",
	}
	left := strings.Repeat(" ", max(0, (width-boxWidth)/2))
	top := max(0, (len(lines)-len(content))/2)
	for i, line := range content {
		lines[top+i] = left + line
	}
}

func dialogLine(text string, width int) string {
	text = fit(text, width)
	return "│ " + text + strings.Repeat(" ", max(0, width-runewidth.StringWidth(text))) + " │"
}

func dialogLineWithCursor(text string, width int) string {
	return "│ " + text + "\x1b[7m \x1b[0m" + strings.Repeat(" ", max(0, width-runewidth.StringWidth(text)-1)) + " │"
}

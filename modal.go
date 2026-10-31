package main

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

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

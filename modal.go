package main

import (
	"strings"
	"time"

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
	"h / l       Collapse / expand file or nearby context",
	"tab         Switch staged / unstaged",
	"space       Stage / unstage file",
	"d           Discard unstaged hunk / file",
	"count + motion  Repeat a motion",
	"count + gg/G  Jump to source line",
	"gH / gM / gL  Top / middle / bottom of screen",
	"[c / ]c     Previous / next change",
	"ctrl-o / gi  Jump back / forward",
	"w           Toggle line wrapping",
	"e           Expand / collapse deleted file",
	"f           File tree (j/k, h/l, / search)",
	"/           Search diff or history",
	"g?          Search diff backward",
	"n / N       Next / previous diff match",
	"o           Open file in $EDITOR",
	"y           Copy file path / commit hash",
	"c           Commit changes",
	"p / P       Push / pull",
	"r           Refresh changes",
	"H           Open commit history",
	"enter/space Open selected commit",
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

func showCommitDialog(lines []string, width int, input, message string, commits []commitInfo, loading, failed bool) {
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
	}
	if infoFirst != "" {
		content = append(content, dialogLine(infoFirst, innerWidth))
	}
	if infoSecond != "" {
		content = append(content, dialogLine(infoSecond, innerWidth))
	}
	content = append(content, dialogLine("↵ commit  esc exit", innerWidth), "└"+strings.Repeat("─", boxWidth-2)+"┘")
	room := max(0, len(lines)-2-len(content))
	if room >= 1 && (loading || failed || commits != nil) {
		switch {
		case loading:
			content = append(content, "  Loading…")
		case failed:
			content = append(content, "  Recent commits unavailable")
		case len(commits) == 0:
			content = append(content, "  No previous commits")
		default:
			count := min(5, len(commits), room)
			spaced := room >= 2*count-1
			twoLines := room >= 3*count-1
			for i, commit := range commits[:count] {
				metadata := relativeCommitTime(commit.date, time.Now()) + " · " + clean(commit.author)
				if twoLines {
					content = append(content, fit("  "+clean(commit.subject), boxWidth), fit("  "+metadata, boxWidth))
				} else {
					metadata = fit(metadata, max(1, boxWidth/2))
					available := max(0, boxWidth-4-runewidth.StringWidth(metadata))
					content = append(content, fit("  "+fit(clean(commit.subject), available)+"  "+metadata, boxWidth))
				}
				if spaced && i < count-1 {
					content = append(content, "")
				}
			}
		}
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

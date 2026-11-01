package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type shortcut struct{ keys, action string }

var shortcutSections = []struct {
	title string
	keys  []shortcut
}{
	{"Changes", []shortcut{
		{"tab", "Switch unstaged / staged"},
		{"space", "Stage / unstage file"},
		{"s", "Stage / unstage hunk"},
		{"S", "Stash all changes, including untracked"},
		{"d", "Discard hunk / file"},
		{"c", "Commit changes"},
		{"p / P", "Push / pull"},
		{"F", "Fetch remote changes"},
		{"r", "Refresh"},
	}},
	{"Move", []shortcut{
		{"j / k", "Down / up"},
		{"gg / G", "First / last line"},
		{"ctrl-d / ctrl-u", "Half page down / up"},
		{"ctrl-f / ctrl-b", "Page down / up"},
		{"ctrl-e / ctrl-y", "Scroll viewport down / up"},
		{"} / {", "Next / previous paragraph"},
		{"J / K", "Next / previous file"},
		{") / (", "Next / previous commit in the list"},
		{"]c / [c", "Next / previous change"},
		{"gH / gM / gL", "Top / middle / bottom of screen"},
		{"zt / zz / zb", "Align active line"},
		{"zh / zl", "Scroll horizontally"},
		{"ctrl-o / gi", "Jump back / forward"},
		{"count + motion", "Repeat a motion"},
		{"count + gg / G", "Jump to source line"},
	}},
	{"View", []shortcut{
		{"f", "File tree (j/k, h/l fold, / filter)"},
		{"L", "Open history for the current file"},
		{"v", "Mark file viewed / unviewed"},
		{"/ / g?", "Search forward / backward"},
		{"n / N", "Next / previous match"},
		{"h / l", "Fold file or commit details / reveal nearby context"},
		{"e", "Expand / collapse deleted file"},
		{"w", "Toggle line wrapping"},
		{"W", "Toggle ignoring whitespace"},
		{"B", "Toggle short file paths"},
		{"T", "Toggle light / dark colors"},
	}},
	{"History", []shortcut{
		{"H", "Open commit history"},
		{"↵ / space", "Open selected commit"},
		{"} / {", "Jump five commits"},
		{"/", "Search commits"},
		{"u", "Edit latest unpublished commit message"},
		{"S", "Open stashes"},
		{"R", "Review from this commit to the working tree"},
		{"V", "Select a commit range to review"},
		{"esc / q", "Back"},
	}},
	{"Stashes", []shortcut{
		{"j / k", "Select stash"},
		{"↵ / space", "View stash"},
		{"p / x", "Pop / drop selected stash"},
		{"esc / q", "Back to history"},
	}},
	{"Search input", []shortcut{
		{"← / →, home / end", "Move within the query"},
		{"alt-b / alt-f, ctrl-a / ctrl-e", "Move by word or to start / end"},
		{"ctrl-w / alt-backspace", "Delete previous word"},
		{"backspace / delete", "Delete character"},
	}},
	{"General", []shortcut{
		{"o", "Open file in $EDITOR"},
		{"y", "Copy file path / commit hash"},
		{"Y", "Copy path:line / commit permalink"},
		{"?", "Show shortcuts"},
		{"q", "Quit"},
	}},
	{"Commit input", []shortcut{
		{"ctrl-g", "Write message in external editor"},
		{"esc / i / a", "Normal mode / insert before or after cursor"},
		{"h / l, w / b, 0 / $", "Normal mode: move in subject"},
		{"x / dw / dd / D", "Normal mode: delete character, word, or line"},
		{"I / A", "Insert at start / end"},
		{"← / →, ctrl-b / ctrl-f", "Move cursor"},
		{"alt-b / alt-f", "Move by word"},
		{"home / end, ctrl-a / ctrl-e", "Start / end of subject"},
		{"ctrl-w / alt-backspace", "Delete previous word"},
		{"ctrl-u / ctrl-k", "Delete to start / end"},
		{"backspace / delete, ctrl-d", "Delete character"},
	}},
}

const helpBoxWidth = 76

// helpVisibleRows is the number of shortcut rows shown in a help dialog on a
// screen of height rows: the title and status bars, the dialog border, title
// and hint take the rest.
func helpVisibleRows(height int) int { return max(1, height-6) }

func shortcutLines(width int, accent string) []string {
	innerWidth := max(1, min(helpBoxWidth, width-2)-4)
	var lines []string
	for i, section := range shortcutSections {
		if i > 0 {
			lines = append(lines, "")
		}
		// Each section sizes its own key column, so one long key does not
		// squeeze the descriptions everywhere else.
		keyWidth := 0
		for _, s := range section.keys {
			keyWidth = max(keyWidth, stringWidth(s.keys))
		}
		keyWidth += 2
		lines = append(lines, styleBold+section.title+styleReset)
		for _, s := range section.keys {
			if innerWidth < keyWidth+16 {
				for j, part := range wrapLine(s.keys+"  "+s.action, innerWidth) {
					if j == 0 {
						part = accent + part + styleReset
					}
					lines = append(lines, part)
				}
				continue
			}
			for j, part := range wrapWords(s.action, innerWidth-keyWidth) {
				key := ""
				if j == 0 {
					key = s.keys
				}
				lines = append(lines, accent+key+styleReset+strings.Repeat(" ", keyWidth-stringWidth(key))+part)
			}
		}
	}
	return lines
}

// wrapWords wraps s at spaces where possible, falling back to wrapLine for
// words wider than width.
func wrapWords(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case stringWidth(line)+1+stringWidth(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
		for stringWidth(line) > width {
			parts := wrapLine(line, width)
			lines = append(lines, parts[:len(parts)-1]...)
			line = parts[len(parts)-1]
		}
	}
	return append(lines, line)
}

func (m model) showHelpDialog(lines []string) {
	if m.width < 8 || len(lines) < 6 {
		return
	}
	boxWidth := min(helpBoxWidth, m.width-2)
	entries := shortcutLines(m.width, m.accent())
	visible := min(len(entries), helpVisibleRows(len(lines)))
	pos := min(m.helpPos, max(0, len(entries)-visible))
	info := ""
	if pos > 0 {
		info += "↑"
	}
	if pos+visible < len(entries) {
		info += "↓"
	}
	placeDialog(lines, m.width, dialog{
		width: boxWidth,
		title: "Keyboard shortcuts",
		info:  info,
		body:  entries[pos : pos+visible],
		hint:  keyHints("j/k", "scroll", "esc", "close"),
	}.render())
}

// commitScope describes which changes a commit will include.
func (m model) commitScope() string {
	files := func(n int) string {
		if n == 1 {
			return "1 file"
		}
		return fmt.Sprintf("%d files", n)
	}
	if staged := len(m.snapshot.staged); staged > 0 {
		return files(staged) + " staged"
	}
	if n := len(m.snapshot.unstaged); n > 0 {
		return "all " + files(n)
	}
	return ""
}

func (m model) showCommitDialog(lines []string) {
	boxWidth := min(64, m.width-2)
	innerWidth := boxWidth - 4
	body := []string{"", renderInput(m.input, m.inputCursor, innerWidth)}
	if m.inputBody != "" {
		body = append(body, styleDim+"Body preserved"+styleReset)
	}
	if m.message != "" {
		for _, line := range wrapLine(clean(m.message), innerWidth)[:min(2, len(wrapLine(clean(m.message), innerWidth)))] {
			body = append(body, m.danger()+line+styleReset)
		}
	}
	body = append(body, "")
	mode, hint := "INSERT", keyHints("↵", "commit", "ctrl-g", "editor", "esc", "normal")
	if m.inputNormal {
		mode, hint = "NORMAL", keyHints("i/a", "insert", "ctrl-g", "editor", "esc", "cancel")
	}
	info := mode
	if scope := m.commitScope(); scope != "" && m.amendHash == "" {
		info += " · " + scope
	}
	title := "Commit changes"
	if m.amendHash != "" {
		title = "Edit commit message"
	}
	content := dialog{
		width: boxWidth,
		title: title,
		info:  info,
		body:  body,
		hint:  hint,
	}.render()
	room := max(0, len(lines)-2-len(content)-1)
	commits := m.recentCommits
	if room >= 1 && (m.recentCommitLoading || m.recentCommitError || commits != nil) {
		content = append(content, "")
		switch {
		case m.recentCommitLoading:
			content = append(content, styleDim+"  Loading…"+styleReset)
		case m.recentCommitError:
			content = append(content, styleDim+"  Recent commits unavailable"+styleReset)
		case len(commits) == 0:
			content = append(content, styleDim+"  No previous commits"+styleReset)
		default:
			count := min(5, len(commits), room)
			spaced := room >= 2*count-1
			twoLines := room >= 3*count-1
			for i, commit := range commits[:count] {
				metadata := relativeCommitTime(commit.date, time.Now()) + " · " + clean(commit.author)
				if twoLines {
					content = append(content, fit("  "+clean(commit.subject), boxWidth), styleDim+fit("  "+metadata, boxWidth)+styleReset)
				} else {
					metadata = fit(metadata, max(1, boxWidth/2))
					available := max(0, boxWidth-4-stringWidth(metadata))
					content = append(content, "  "+fit(clean(commit.subject), available)+"  "+styleDim+metadata+styleReset)
				}
				if spaced && i < count-1 {
					content = append(content, "")
				}
			}
		}
	}
	// Center the box itself; the recent commits hang below it.
	left := strings.Repeat(" ", max(0, (m.width-boxWidth)/2))
	top := 1 + max(0, (len(lines)-2-len(content))/2)
	for i, line := range content {
		if top+i < len(lines)-1 {
			lines[top+i] = left + line
		}
	}
}

// showDiscardDialog asks for confirmation before discarding changes.
func (m model) showDiscardDialog(lines []string) {
	title := "Discard hunk?"
	if m.discard.whole {
		title = "Discard all unstaged changes in file?"
	}
	if m.discard.change.untracked {
		title = "Delete untracked file from disk?"
	}
	boxWidth := min(max(48, ansi.StringWidth(title)+6), m.width-2)
	placeDialog(lines, m.width, dialog{
		width:      boxWidth,
		title:      title,
		titleStyle: m.danger(),
		body:       []string{"", clean(m.discard.change.path), styleDim + "This cannot be undone." + styleReset, ""},
		hint:       keyHints("y", "discard", "n/esc", "cancel"),
	}.render())
}

// errorLines wraps the error text for a dialog of inner width.
func (m model) errorLines(width int) []string {
	var content []string
	for _, line := range strings.Split(strings.TrimRight(m.errorText, "\n"), "\n") {
		content = append(content, wrapLine(clean(line), max(1, width))...)
	}
	return content
}

// errorMaxPos is the last scroll position that still fills the error view.
func (m model) errorMaxPos() int {
	if m.width < 24 || m.height < 8 {
		return max(0, len(m.errorLines(m.width))-1)
	}
	return max(0, len(m.errorLines(min(100, m.width-2)-4))-max(1, m.height-6))
}

func (m model) showErrorDialog(lines []string) {
	boxWidth := min(100, m.width-2)
	content := m.errorLines(boxWidth - 4)
	visible := min(len(content), max(1, len(lines)-6))
	start := min(m.errorPos, max(0, len(content)-visible))
	info := ""
	if start > 0 {
		info += "↑"
	}
	if start+visible < len(content) {
		info += "↓"
	}
	placeDialog(lines, m.width, dialog{
		width:      boxWidth,
		title:      "Git error",
		titleStyle: m.danger(),
		info:       info,
		body:       content[start : start+visible],
		hint:       keyHints("j/k", "scroll", "↵/esc", "dismiss"),
	}.render())
}

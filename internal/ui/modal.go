package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

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
	entries := m.helpLines()
	visible := min(len(entries), helpVisibleRows(len(lines)))
	pos := min(m.help.selected, max(0, len(entries)-visible))
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
		hint:  keyHints("j/k", "scroll", "/", "filter", "esc", "close"),
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
	if staged := len(m.snapshot.Staged); staged > 0 {
		return files(staged) + " staged"
	}
	if n := len(m.snapshot.Unstaged); n > 0 {
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
		for _, line := range wrapLine(syntax.Clean(m.message), innerWidth)[:min(2, len(wrapLine(syntax.Clean(m.message), innerWidth)))] {
			body = append(body, m.danger()+line+styleReset)
		}
	}
	body = append(body, "")
	hint := keyHints("↵", "commit", "ctrl-g", "editor", "esc", "cancel")
	info := ""
	if scope := m.commitScope(); scope != "" && m.amendHash == "" {
		info = scope
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
				metadata := git.RelativeCommitTime(commit.Date, time.Now()) + " · " + syntax.Clean(commit.Author)
				if twoLines {
					content = append(content, fit("  "+syntax.Clean(commit.Subject), boxWidth), styleDim+fit("  "+metadata, boxWidth)+styleReset)
				} else {
					metadata = fit(metadata, max(1, boxWidth/2))
					available := max(0, boxWidth-4-stringWidth(metadata))
					content = append(content, "  "+fit(syntax.Clean(commit.Subject), available)+"  "+styleDim+metadata+styleReset)
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
	if m.discard.Whole {
		title = "Discard all unstaged changes in file?"
	}
	if m.discard.Change.Untracked {
		title = "Delete untracked file from disk?"
	}
	boxWidth := min(max(48, ansi.StringWidth(title)+6), m.width-2)
	placeDialog(lines, m.width, dialog{
		width:      boxWidth,
		title:      title,
		titleStyle: m.danger(),
		body:       []string{"", syntax.Clean(m.discard.Change.Path), styleDim + "This cannot be undone." + styleReset, ""},
		hint:       keyHints("y", "discard", "n/esc", "cancel"),
	}.render())
}

func (m model) showBranchDelete(lines []string) {
	title := "Delete branch " + m.branch.confirm.Name + "?"
	if m.branch.confirmForce {
		title = "Force delete branch " + m.branch.confirm.Name + "?"
	}
	boxWidth := min(max(48, ansi.StringWidth(title)+6), m.width-2)
	flag := "-d"
	if m.branch.confirmForce {
		flag = "-D"
	}
	body := []string{"", styleDim + "Pane runs git branch " + flag + "." + styleReset, ""}
	placeDialog(lines, m.width, dialog{
		width: boxWidth, title: title, titleStyle: m.danger(), body: body,
		hint: keyHints("y", "delete", "n/esc", "cancel"),
	}.render())
}

func (m model) showWorktreeDelete(lines []string) {
	title := "Remove worktree?"
	boxWidth := min(max(48, ansi.StringWidth(title)+6), m.width-2)
	body := []string{"", syntax.Clean(m.worktrees.confirm.Path), styleDim + "This cannot be undone." + styleReset, ""}
	if m.worktrees.confirmForce {
		title = "Force remove worktree?"
		note := "Uncommitted changes will be lost."
		if status := m.worktrees.status[m.worktrees.confirm.Path]; status.loaded && status.dirty > 0 {
			label := "files"
			if status.dirty == 1 {
				label = "file"
			}
			note = fmt.Sprintf("%d changed %s will be lost.", status.dirty, label)
		}
		body = []string{"", syntax.Clean(m.worktrees.confirm.Path), styleDim + note + styleReset, ""}
	}
	placeDialog(lines, m.width, dialog{
		width: boxWidth, title: title, titleStyle: m.danger(), body: body,
		hint: keyHints("y", "remove", "n/esc", "cancel"),
	}.render())
}

// errorLines wraps the error text for a dialog of inner width.
func (m model) errorLines(width int) []string {
	var content []string
	for _, line := range strings.Split(strings.TrimRight(m.errorText, "\n"), "\n") {
		content = append(content, wrapLine(syntax.Clean(line), max(1, width))...)
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

package main

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) startDiscard() (tea.Model, tea.Cmd) {
	if m.whitespaceRefreshing {
		m.flash = "Wait for the diff to refresh"
		return m, nil
	}
	if m.repo.IgnoreSpace {
		m.flash = "Turn off whitespace ignore (W) to discard changes"
		return m, nil
	}
	if m.busy || !m.onDiffFile() {
		return m, nil
	}
	if m.staged {
		m.flash = "Unstage changes before discarding"
		return m, nil
	}
	selected := m.rows[m.pos]
	for _, c := range m.snapshot.Unstaged {
		if c.Path != selected.path {
			continue
		}
		if c.Conflict {
			m.flash = "Resolve conflicts outside pane"
			return m, nil
		}
		if c.Untracked && strings.Contains(c.Diff, "Binary files ") && !c.UntrackedHashValid {
			m.flash = "Cannot verify untracked file; discard it outside pane"
			return m, nil
		}
		request := DiscardRequest{Change: c, Whole: selected.kind == 'f' || selected.kind == 'F' || c.Untracked}
		if selected.kind != 'f' && selected.kind != 'F' {
			if selected.kind != '+' && selected.kind != '-' && selected.kind != 'd' && selected.kind != ' ' {
				m.flash = "Select a diff line or file header"
				return m, nil
			}
			if !c.Untracked {
				request.Patch = selectedHunk(c.Diff, selected)
				if request.Patch == "" {
					m.flash = "No change hunk at this line"
					return m, nil
				}
			}
		}
		m.discard = request
		m.modal = "discard"
		return m, nil
	}
	return m, nil
}

func selectedHunk(diff string, selected row) string {
	if strings.Count(diff, "\ndiff --git ") > 0 {
		return ""
	}
	lines := strings.SplitAfter(diff, "\n")
	first := -1
	for i, line := range lines {
		if matchHunkHeader(line) != nil {
			first = i
			break
		}
	}
	if first < 0 {
		return ""
	}
	var header strings.Builder
	for _, line := range lines[:first] {
		if !strings.HasPrefix(line, "old mode ") && !strings.HasPrefix(line, "new mode ") {
			header.WriteString(line)
		}
	}
	for i := first; i < len(lines); {
		match := matchHunkHeader(lines[i])
		if match == nil {
			i++
			continue
		}
		start, count := match[3], match[4]
		if selected.kind == '-' || selected.kind == 'd' {
			start, count = match[1], match[2]
		}
		lineStart, _ := strconv.Atoi(start)
		lineCount := 1
		if count != "" {
			lineCount, _ = strconv.Atoi(count)
		}
		end := i + 1
		for end < len(lines) && matchHunkHeader(lines[end]) == nil && !strings.HasPrefix(lines[end], "diff --git ") {
			end++
		}
		if selected.sourceLine >= lineStart && selected.sourceLine < lineStart+lineCount {
			return header.String() + strings.Join(lines[i:end], "")
		}
		i = end
	}
	return ""
}

func (m model) discardView() string {
	action := "Discard hunk"
	if m.discard.Whole {
		action = "Discard all unstaged changes in file"
	}
	if m.discard.Change.Untracked {
		action = "Delete untracked file from disk"
	}
	content := []string{" " + action + "?", " " + clean(m.discard.Change.Path), " This cannot be undone.", " y confirm  n/esc cancel"}
	lines := make([]string, max(1, m.height))
	for i, line := range content {
		if i >= len(lines) {
			break
		}
		lines[i] = fit(line, m.width)
	}
	return strings.Join(lines, "\n")
}

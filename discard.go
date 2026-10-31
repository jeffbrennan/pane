package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type discardRequest struct {
	change change
	patch  string
	whole  bool
}

func (m model) startDiscard() (tea.Model, tea.Cmd) {
	if m.whitespaceRefreshing {
		m.flash = "Wait for the diff to refresh"
		return m, nil
	}
	if m.repo.ignoreSpace {
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
	for _, c := range m.snapshot.unstaged {
		if c.path != selected.path {
			continue
		}
		if c.conflict {
			m.flash = "Resolve conflicts outside pane"
			return m, nil
		}
		if c.untracked && strings.Contains(c.diff, "Binary files ") && !c.untrackedHashValid {
			m.flash = "Cannot verify untracked file; discard it outside pane"
			return m, nil
		}
		request := discardRequest{change: c, whole: selected.kind == 'f' || selected.kind == 'F' || c.untracked}
		if selected.kind != 'f' && selected.kind != 'F' {
			if selected.kind != '+' && selected.kind != '-' && selected.kind != 'd' && selected.kind != ' ' {
				m.flash = "Select a diff line or file header"
				return m, nil
			}
			if !c.untracked {
				request.patch = selectedHunk(c.diff, selected)
				if request.patch == "" {
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

func (r repository) discard(request discardRequest) error {
	current, err := r.load()
	if err != nil {
		return err
	}
	valid := false
	for _, c := range current.unstaged {
		if c.path == request.change.path && c.diff == request.change.diff && c.untracked == request.change.untracked && c.untrackedHashValid == request.change.untrackedHashValid && c.untrackedHash == request.change.untrackedHash && !c.conflict {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("file changed since confirmation; refresh and try again")
	}
	if request.change.untracked {
		return os.Remove(filepath.Join(r.root, request.change.path))
	}
	if request.whole {
		out, err := r.command("restore", "--worktree", "--", request.change.path)
		if err != nil {
			return gitError(out, err)
		}
		return nil
	}
	return r.applyPatch(request.patch, "--reverse")
}

func (m model) discardView() string {
	action := "Discard hunk"
	if m.discard.whole {
		action = "Discard all unstaged changes in file"
	}
	if m.discard.change.untracked {
		action = "Delete untracked file from disk"
	}
	content := []string{" " + action + "?", " " + clean(m.discard.change.path), " This cannot be undone.", " y confirm  n/esc cancel"}
	lines := make([]string, max(1, m.height))
	for i, line := range content {
		if i >= len(lines) {
			break
		}
		lines[i] = fit(line, m.width)
	}
	return strings.Join(lines, "\n")
}

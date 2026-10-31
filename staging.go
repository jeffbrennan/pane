package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type stagingCursor struct {
	path   string
	target int
	staged bool
}

func (m model) stageSelection(hunk bool) (tea.Model, tea.Cmd) {
	if m.whitespaceRefreshing {
		m.flash = "Wait for the diff to refresh"
		return m, nil
	}
	if m.busy || !m.onDiffFile() {
		return m, nil
	}
	selected := m.rows[m.pos]
	for _, c := range m.snapshot.unstaged {
		if c.path == selected.path && c.conflict {
			m.flash = "Resolve conflicts outside pane"
			return m, nil
		}
	}
	changes := m.snapshot.unstaged
	if m.staged {
		changes = m.snapshot.staged
	}
	for _, c := range changes {
		if c.path != selected.path {
			continue
		}
		whole := !hunk || selected.kind == 'f' || selected.kind == 'F' || c.untracked || strings.Contains(c.diff, "\ndeleted file mode ")
		if m.repo.ignoreSpace && !whole {
			m.flash = "Turn off whitespace ignore (W) to stage hunks"
			return m, nil
		}
		patch := ""
		if !whole {
			if selected.kind != '+' && selected.kind != '-' && selected.kind != ' ' {
				m.flash = "Select a diff line or file header"
				return m, nil
			}
			patch = selectedHunk(c.diff, selected)
			if patch == "" {
				m.flash = "No change hunk at this line"
				return m, nil
			}
		}
		m.busy = true
		m.generation++
		repo, staged := m.repo, m.staged
		return m, func() tea.Msg {
			if whole {
				return completed{message: "Updated staging", err: repo.stageChange(c, staged)}
			}
			err := repo.stageHunk(c, patch, staged)
			message := "Staged hunk"
			if staged {
				message = "Unstaged hunk"
			}
			return completed{message: message, err: err, stageCursor: &stagingCursor{path: c.path, target: selected.target, staged: staged}}
		}
	}
	return m, nil
}

func (r repository) stageHunk(c change, patch string, staged bool) error {
	current, err := r.diffsForChanges([]change{c}, staged)
	if err != nil {
		return err
	}
	if len(current) != 1 || current[0].diff != c.diff {
		return fmt.Errorf("file changed; refresh and try again")
	}
	if c.oldPath != "" {
		patch, err = renameContentPatch(patch)
		if err != nil {
			return err
		}
	}
	args := []string{"--cached"}
	if staged {
		args = append(args, "--reverse")
	}
	if err := r.applyPatch(patch, args...); err != nil {
		return fmt.Errorf("could not apply hunk; refresh and try again: %w", err)
	}
	return nil
}

func renameContentPatch(patch string) (string, error) {
	var destination string
	lines := strings.SplitAfter(patch, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "+++ ") {
			destination = strings.TrimSuffix(strings.TrimPrefix(line, "+++ "), "\n")
		}
		if matchHunkHeader(line) != nil && destination != "" {
			old := destination
			if strings.HasPrefix(old, `"b/`) {
				old = `"a/` + old[3:]
			} else if strings.HasPrefix(old, "b/") {
				old = "a/" + old[2:]
			} else {
				break
			}
			return "--- " + old + "\n+++ " + destination + "\n" + strings.Join(lines[i:], ""), nil
		}
	}
	return "", fmt.Errorf("could not prepare renamed hunk; refresh and try again")
}

func (m *model) restoreStagingCursor() {
	cursor := m.stageCursor
	m.stageCursor = nil
	if cursor == nil || m.screen != "" || m.staged != cursor.staged {
		return
	}
	last := -1
	for i, r := range m.rows {
		if r.path != cursor.path || (r.kind != '+' && r.kind != '-' && r.kind != 'd') {
			continue
		}
		last = i
		if r.target >= cursor.target {
			m.pos = i
			m.reveal()
			return
		}
	}
	if last >= 0 {
		m.pos = last
		m.reveal()
	}
}

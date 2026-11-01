package main

import (
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
	for _, c := range m.snapshot.Unstaged {
		if c.Path == selected.path && c.Conflict {
			m.flash = "Resolve conflicts outside pane"
			return m, nil
		}
	}
	changes := m.snapshot.Unstaged
	if m.staged {
		changes = m.snapshot.Staged
	}
	for _, c := range changes {
		if c.Path != selected.path {
			continue
		}
		whole := !hunk || selected.kind == 'f' || selected.kind == 'F' || c.Untracked || strings.Contains(c.Diff, "\ndeleted file mode ")
		if m.repo.IgnoreSpace && !whole {
			m.flash = "Turn off whitespace ignore (W) to stage hunks"
			return m, nil
		}
		patch := ""
		if !whole {
			if selected.kind != '+' && selected.kind != '-' && selected.kind != ' ' {
				m.flash = "Select a diff line or file header"
				return m, nil
			}
			patch = selectedHunk(c.Diff, selected)
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
				return completed{message: "Updated staging", err: repo.StageChange(c, staged)}
			}
			err := repo.StageHunk(c, patch, staged)
			message := "Staged hunk"
			if staged {
				message = "Unstaged hunk"
			}
			return completed{message: message, err: err, stageCursor: &stagingCursor{path: c.Path, target: selected.target, staged: staged}}
		}
	}
	return m, nil
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

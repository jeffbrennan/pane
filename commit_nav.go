package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type commitPosition struct {
	path string
	line int
}

// saveCommitPosition remembers where the reader was in the current commit so
// stepping back to it can return there.
func (m *model) saveCommitPosition() {
	if m.screen != "commit" || m.stash.viewing {
		return
	}
	view := m.commitModel()
	if view.pos < 0 || view.pos >= len(view.rows) {
		return
	}
	selected := view.rows[view.pos]
	if selected.path == "" || selected.sourceLine == 0 {
		return
	}
	if m.history.positions == nil {
		m.history.positions = make(map[string]commitPosition)
	}
	m.history.positions[m.history.current.Hash] = commitPosition{path: selected.path, line: selected.sourceLine}
}

// restoreCommitPosition moves to the remembered line once its file is loaded.
func (m *model) restoreCommitPosition(done bool) {
	position := m.history.pendingPosition
	if position == nil || len(m.history.rows) == 0 {
		return
	}
	best := -1
	for i, r := range m.history.rows {
		if r.path != position.path || r.sourceLine == 0 {
			continue
		}
		if r.sourceLine <= position.line {
			best = i
		}
		if r.sourceLine >= position.line {
			break
		}
	}
	if best < 0 {
		if done {
			m.history.pendingPosition = nil
		}
		return
	}
	m.history.pos = best
	m.history.pendingPosition = nil
	view := m.commitModel()
	view.reveal()
	m.history.viewTop = view.viewTop
}

// commitListPosition reports the current commit's place in the list it came
// from, or zero when the commit was opened directly.
func (m model) commitListPosition() (int, int) {
	if m.history.directCommit || m.stash.viewing {
		return 0, 0
	}
	commits := m.filteredHistory()
	for i, commit := range commits {
		if commit.Hash == m.history.current.Hash {
			return i + 1, len(commits)
		}
	}
	return 0, 0
}

// stepCommit opens the next or previous commit in the list the current one came
// from. At the end of a loaded page it fetches the next page and waits.
func (m model) stepCommit(direction int) (tea.Model, tea.Cmd) {
	if m.screen != "commit" || m.stash.viewing || m.history.directCommit {
		return m, nil
	}
	commits := m.filteredHistory()
	index := -1
	for i, commit := range commits {
		if commit.Hash == m.history.current.Hash {
			index = i
			break
		}
	}
	if index < 0 {
		return m, nil
	}
	next := index + direction
	if next >= 0 && next < len(commits) {
		return m.openCommit(commits[next])
	}
	if direction > 0 && m.history.more {
		m.history.pendingStep = direction
		m.history.fetching = true
		return m, m.fetchHistory(len(m.history.commits), git.HistoryPage)
	}
	if direction > 0 {
		m.flash = fmt.Sprintf("Last commit (%d)", len(commits))
	} else {
		m.flash = "First commit"
	}
	return m, nil
}

package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeffbrennan/pane/internal/git"
)

func (m model) toggleWhitespace() (tea.Model, tea.Cmd) {
	m.repo.IgnoreSpace = !m.repo.IgnoreSpace
	m.flash = ""
	if !m.repo.IgnoreSpace {
		m.flash = "Showing whitespace"
	}
	m.generation++
	m.refreshCache = nil
	m.whitespaceRefreshing = true
	m.contextFiles = nil
	m.contextRequest++
	var refreshCmd tea.Cmd
	m, refreshCmd = m.refresh()
	commands := []tea.Cmd{refreshCmd}
	switch m.screen {
	case "base":
		m.base.request++
		m.base.loading = false
		m.base.dirty = false
		var cmd tea.Cmd
		m, cmd = m.refreshBase(true)
		commands = append(commands, cmd)
	case "commit":
		m.history.request++
		repo := m.startCommitLoad()
		m.history.streamActive = !m.stash.viewing
		m.history.streamPaths, m.history.streamLoaded, m.history.streamGutter = nil, 0, 0
		m.history.streamError, m.picker.pendingPath = "", ""
		m.history.streamEndPending = false
		m.history.loading = true
		m.history.commitLoadingVisible = false
		hash, request := m.history.current.Hash, m.history.request
		if m.stash.viewing {
			commands = append(commands, func() tea.Msg {
				changes, err := repo.StashChanges(hash)
				return stashDiffLoaded{changes: changes, request: request, err: err}
			})
			break
		}
		m.history.rows, m.history.changes = nil, nil
		m.history.moved = nil
		commands = append(commands, startCommitStream(repo, hash, request, m.height), commitLoadingTimer(request))
	case "sequence":
		m.resetSequenceStream()
		m.sequence.request++
		m.sequence.rowCache = nil
		m.sequence.changes = make([][]git.Change, len(m.sequence.commits))
		m.sequence.states = make([]sequenceLoadState, len(m.sequence.commits))
		m.sequence.errors = make([]string, len(m.sequence.commits))
		m.sequence.loading = len(m.sequence.commits) > 0
		for _, i := range []int{0, len(m.sequence.commits) - 1} {
			if i < 0 || m.sequence.states[i] == sequenceLoading {
				continue
			}
			m.sequence.states[i] = sequenceLoading
			commands = append(commands, m.startSequenceStream(i))
		}
		m.rebuildSequence()
	}
	return m, tea.Batch(commands...)
}

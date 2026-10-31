package main

import tea "github.com/charmbracelet/bubbletea"

func (m model) toggleWhitespace() (tea.Model, tea.Cmd) {
	m.repo.ignoreSpace = !m.repo.ignoreSpace
	m.flash = ""
	if !m.repo.ignoreSpace {
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
		m.history.loading = true
		m.history.commitLoadingVisible = false
		repo, hash, request := m.repo, m.history.current.hash, m.history.request
		if m.stash.viewing {
			commands = append(commands, func() tea.Msg {
				changes, err := repo.stashChanges(hash)
				return stashDiffLoaded{changes: changes, request: request, err: err}
			})
			break
		}
		commands = append(commands, func() tea.Msg {
			changes, err := repo.commitChangesWithoutSyntax(hash)
			return commitLoaded{changes, err, request}
		}, commitLoadingTimer(request))
	case "sequence":
		m.sequence.request++
		m.sequence.rowCache = nil
		m.sequence.changes = make([][]change, len(m.sequence.commits))
		m.sequence.states = make([]sequenceLoadState, len(m.sequence.commits))
		m.sequence.errors = make([]string, len(m.sequence.commits))
		m.sequence.loading = len(m.sequence.commits) > 0
		for _, i := range []int{0, len(m.sequence.commits) - 1} {
			if i < 0 || m.sequence.states[i] == sequenceLoading {
				continue
			}
			m.sequence.states[i] = sequenceLoading
			commands = append(commands, loadSequenceCommit(m.repo, m.sequence.commits[i].hash, i, m.sequence.request))
		}
		m.rebuildSequence()
	}
	return m, tea.Batch(commands...)
}

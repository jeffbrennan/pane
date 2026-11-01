package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func (m model) openFileHistory() (tea.Model, tea.Cmd) {
	if m.history.path != "" {
		m.flash = "Already viewing file history"
		return m, nil
	}
	view := m
	if m.screen == "commit" || m.screen == "base" {
		view = m.commitModel()
	}
	if !view.onDiffFile() {
		m.flash = "Select a file to view its history"
		return m, nil
	}
	path := view.rows[view.pos].path
	previous := m
	previous.cancelCommitLoad()
	previous.fileHistoryReturn = nil
	m.fileHistoryReturn = &previous
	m.history = historyState{path: path, request: m.history.request}
	m.stash.viewing = false
	m.screen = "history"
	m.pending, m.count = "", 0
	return m.reloadHistory()
}

func (m model) leaveFileHistory() (tea.Model, tea.Cmd) {
	m.history.stream.Close()
	if m.fileHistoryReturn == nil {
		return m, tea.Quit
	}
	previous := *m.fileHistoryReturn
	previous.fileHistoryReturn = nil
	previous.history.request = max(previous.history.request, m.history.request) + 1
	whitespaceChanged := previous.repo.IgnoreSpace != m.repo.IgnoreSpace
	headChanged := previous.snapshot.Branch.Available && m.snapshot.Branch.Available && previous.snapshot.Branch.Head != m.snapshot.Branch.Head
	previous.repo, previous.snapshot, previous.refreshCache = m.repo, m.snapshot, m.refreshCache
	previous.loading, previous.busy, previous.generation = m.loading, m.busy, m.generation
	previous.activeAction, previous.whitespaceRefreshing = m.activeAction, m.whitespaceRefreshing
	previous.width, previous.height = m.width, m.height
	previous.lightTheme, previous.themeAuto, previous.themeNotify, previous.themeQuery = m.lightTheme, m.themeAuto, m.themeNotify, m.themeQuery
	previous.flash = ""
	switch previous.screen {
	case "commit":
		previous.history.stale = previous.history.stale || headChanged
		if whitespaceChanged {
			previous.history.loading = true
		}
		if previous.history.loading {
			previous.history.request++
			repo := previous.startCommitLoad()
			hash, request := previous.history.current.Hash, previous.history.request
			if previous.stash.viewing {
				return previous, func() tea.Msg {
					changes, err := repo.StashChanges(hash)
					return stashDiffLoaded{changes: changes, request: request, err: err}
				}
			}
			previous.history.streamActive = true
			previous.history.streamPaths, previous.history.streamLoaded, previous.history.streamGutter = nil, 0, 0
			previous.history.streamError = ""
			previous.history.streamEndPending = false
			if previous.history.pos >= 0 && previous.history.pos < len(previous.history.rows) {
				previous.picker.pendingPath = previous.history.rows[previous.history.pos].path
			}
			previous.history.changes, previous.history.rows = nil, nil
			return previous, startCommitStream(repo, hash, request, previous.height)
		}
		previous.startCommitLoad()
		previous.rebuildCommit()
	case "base":
		previous.base.request++
		previous.base.loading, previous.base.dirty = false, false
		previous.rebuildCommit()
		return previous.refreshBase(true)
	case "sequence":
		previous.resetSequenceStream()
		previous.sequence.request++
		if whitespaceChanged {
			previous.sequence.rowCache = nil
			previous.sequence.changes = make([][]git.Change, len(previous.sequence.commits))
			previous.sequence.states = make([]sequenceLoadState, len(previous.sequence.commits))
			previous.sequence.errors = make([]string, len(previous.sequence.commits))
			previous.sequence.loading = len(previous.sequence.commits) > 0
		}
		for i, state := range previous.sequence.states {
			if state == sequenceLoading {
				previous.sequence.states[i] = sequencePending
			}
		}
		previous.rebuildSequence()
		return previous, previous.nextSequenceLoad()
	default:
		previous.rebuild()
		return previous.refresh()
	}
	return previous, nil
}

func (m *model) jumpToHistoryFile() {
	path := m.history.current.FilePath
	if path == "" {
		return
	}
	for i, row := range m.history.rows {
		if row.kind == 'f' && (row.path == path || row.oldPath == path) {
			m.history.pos, m.history.viewTop = i, i
			return
		}
	}
}

func initialFileHistory(repo git.Repo, path string) (model, error) {
	resolved, err := git.FileHistoryPath(repo, path)
	if err != nil {
		return model{}, err
	}
	return model{repo: repo, wrap: true, loading: true, startup: true, screen: "history", history: historyState{path: resolved, stream: git.NewHistoryStream(repo, resolved), loading: true, request: 1}}, nil
}

func fileHistoryUsage() string {
	return "usage: pane [commit-or-range | --base <revision> | hot | [log] -- <path>]"
}

// openPathHistory opens file history for a path outside the diff views, as the
// hotspot screen does.
func (m model) openPathHistory(path string) (tea.Model, tea.Cmd) {
	if path == "" {
		return m, nil
	}
	previous := m
	previous.cancelCommitLoad()
	previous.fileHistoryReturn = nil
	m.fileHistoryReturn = &previous
	m.history = historyState{path: path, since: m.hotspots.since, request: m.history.request}
	m.stash.viewing = false
	m.screen = "history"
	m.pending, m.count = "", 0
	m, cmd := m.reloadHistory()
	return m, tea.Batch(cmd, m.loadWorktreeHeads())
}

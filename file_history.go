package main

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func fileHistoryPath(repo repository, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(repo.root)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file must be inside the repository")
	}
	return filepath.ToSlash(relative), nil
}

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
	m.history.stream.close()
	if m.fileHistoryReturn == nil {
		return m, tea.Quit
	}
	previous := *m.fileHistoryReturn
	previous.fileHistoryReturn = nil
	previous.history.request = max(previous.history.request, m.history.request) + 1
	whitespaceChanged := previous.repo.ignoreSpace != m.repo.ignoreSpace
	headChanged := previous.snapshot.branch.available && m.snapshot.branch.available && previous.snapshot.branch.head != m.snapshot.branch.head
	previous.repo, previous.snapshot, previous.refreshCache = m.repo, m.snapshot, m.refreshCache
	previous.loading, previous.busy, previous.generation = m.loading, m.busy, m.generation
	previous.activeAction, previous.whitespaceRefreshing = m.activeAction, m.whitespaceRefreshing
	previous.width, previous.height = m.width, m.height
	previous.lightTheme, previous.themeAuto, previous.themeQuery = m.lightTheme, m.themeAuto, m.themeQuery
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
			hash, request := previous.history.current.hash, previous.history.request
			if previous.stash.viewing {
				return previous, func() tea.Msg {
					changes, err := repo.stashChanges(hash)
					return stashDiffLoaded{changes: changes, request: request, err: err}
				}
			}
			return previous, func() tea.Msg {
				changes, err := repo.commitChangesWithoutSyntax(hash)
				return commitLoaded{changes: changes, request: request, err: err}
			}
		}
		previous.startCommitLoad()
		previous.rebuildCommit()
	case "base":
		previous.base.request++
		previous.base.loading, previous.base.dirty = false, false
		previous.rebuildCommit()
		return previous.refreshBase(true)
	case "sequence":
		previous.sequence.request++
		if whitespaceChanged {
			previous.sequence.rowCache = nil
			previous.sequence.changes = make([][]change, len(previous.sequence.commits))
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
	path := m.history.current.filePath
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

func initialFileHistory(repo repository, path string) (model, error) {
	resolved, err := fileHistoryPath(repo, path)
	if err != nil {
		return model{}, err
	}
	return model{repo: repo, wrap: true, loading: true, startup: true, screen: "history", history: historyState{path: resolved, stream: newHistoryStream(repo, resolved), loading: true, request: 1}}, nil
}

func fileHistoryUsage() string {
	return "usage: pane [commit-or-range | --base <revision> | [log] -- <path>]"
}

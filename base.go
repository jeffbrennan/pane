package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

type baseState struct {
	revision, hash              string
	request                     int
	loading, initialized, dirty bool
}

type baseLoaded struct {
	hash    string
	request int
	changed bool
	err     error
}

func (m model) openBaseFrom(revision string) (tea.Model, tea.Cmd) {
	hash, err := m.repo.MergeBase(revision)
	if err != nil {
		m.flash = "Base review: " + err.Error()
		return m, nil
	}
	m.screen = "base"
	m.base = baseState{revision: revision, hash: hash, initialized: true}
	m.history.directCommit = true
	m.history.changes, m.history.rows = nil, nil
	m.history.request++
	m.base.request++
	m.startCommitLoad()
	m.contextFiles = nil
	m.history.streamPaths, m.history.streamLoaded, m.history.streamGutter = nil, 0, 0
	m.history.streamError, m.history.streamEndPending = "", false
	m.history.streamActive = true
	m.history.loading = true
	m.history.commitLoadingVisible = false
	m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
	m.picker.pendingPath = ""
	m.collapsedFiles, m.generatedHandled = nil, nil
	repo := m.commitRepository()
	return m, tea.Batch(startBaseStream(repo, hash, m.base.request, m.height), commitLoadingTimer(m.history.request))
}

func (m model) refreshBase(force bool) (model, tea.Cmd) {
	if m.screen != "base" {
		return m, nil
	}
	if m.base.loading {
		m.base.dirty = m.base.dirty || force
		return m, nil
	}
	m.base.loading = true
	m.base.request++
	repo, base, request := m.repo, m.base, m.base.request
	return m, func() tea.Msg {
		hash, err := repo.MergeBase(base.revision)
		if err != nil {
			return baseLoaded{request: request, err: err}
		}
		changed := force || !base.initialized || hash != base.hash
		return baseLoaded{hash: hash, request: request, changed: changed}
	}
}

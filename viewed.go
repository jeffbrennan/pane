package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func nextUnviewedRow(rows []row, path string) int {
	current := -1
	for i, r := range rows {
		if r.kind == 'f' && r.path == path {
			current = i
			break
		}
	}
	if current < 0 {
		return -1
	}
	for i := current + 1; i < len(rows); i++ {
		if rows[i].kind == 'f' && rows[i].path != path && !rows[i].viewed {
			return i
		}
	}
	return -1
}

func (m model) toggleViewedCommit() (tea.Model, tea.Cmd) {
	view := m.commitModel()
	if view.pos < 0 || view.pos >= len(view.rows) {
		return m, nil
	}
	path := view.rows[view.pos].path
	if path == "" {
		m.flash = "No file under cursor"
		return m, nil
	}
	if m.viewed == nil || view.viewIdentity == "" {
		m.flash = "Viewed marks are available in commit, range and base reviews"
		return m, nil
	}
	blob := ""
	for _, c := range m.history.changes {
		if c.Path == path {
			blob = diffBlob(c.Diff)
			break
		}
	}
	if blob == "" {
		return m, nil
	}
	marked := !m.viewed.marked(view.viewIdentity, path, blob)
	m.viewed.set(view.viewIdentity, path, blob, marked)
	fileKey := contextKey{path: path, commit: view.contextCommit, staged: false, base: view.contextBase}
	if m.generatedHandled == nil {
		m.generatedHandled = make(map[contextKey]bool)
	}
	m.generatedHandled[fileKey] = true
	if marked {
		if m.collapsedFiles == nil {
			m.collapsedFiles = make(map[contextKey]bool)
		}
		m.collapsedFiles[fileKey] = true
	} else {
		delete(m.collapsedFiles, fileKey)
	}
	m.rebuildCommit()
	m.flash = fmt.Sprintf("Marked %s viewed", path)
	if !marked {
		m.flash = fmt.Sprintf("Marked %s unviewed", path)
		return m, nil
	}
	if next := nextUnviewedRow(m.history.rows, path); next >= 0 {
		m.history.pos = next
		m.history.viewTop = max(0, next-max(1, m.height-2)+1)
	}
	return m, nil
}

func (m model) toggleViewedSequence() (tea.Model, tea.Cmd) {
	if m.pos < 0 || m.pos >= len(m.rows) {
		return m, nil
	}
	path := m.rows[m.pos].path
	if path == "" {
		m.flash = "No file under cursor"
		return m, nil
	}
	identity := m.sequenceIdentity()
	index := int(m.rows[m.pos].commitIndex)
	if m.viewed == nil || identity == "" || index >= len(m.sequence.commits) || index >= len(m.sequence.changes) {
		m.flash = "Viewed marks are available in commit, range and base reviews"
		return m, nil
	}
	blob := ""
	for _, c := range m.sequence.changes[index] {
		if c.Path == path {
			blob = diffBlob(c.Diff)
			break
		}
	}
	if blob == "" {
		return m, nil
	}
	marked := !m.viewed.marked(identity, path, blob)
	m.viewed.set(identity, path, blob, marked)
	fileKey := contextKey{path: path, commit: m.sequence.commits[index].Hash}
	if m.generatedHandled == nil {
		m.generatedHandled = make(map[contextKey]bool)
	}
	m.generatedHandled[fileKey] = true
	if marked {
		if m.collapsedFiles == nil {
			m.collapsedFiles = make(map[contextKey]bool)
		}
		m.collapsedFiles[fileKey] = true
	} else {
		delete(m.collapsedFiles, fileKey)
	}
	m.sequence.rowCache[index] = nil
	m.rebuildSequence()
	m.flash = fmt.Sprintf("Marked %s viewed", path)
	if !marked {
		m.flash = fmt.Sprintf("Marked %s unviewed", path)
		return m, nil
	}
	if next := nextUnviewedRow(m.rows, path); next >= 0 {
		m.pos = next
		m.viewTop = max(0, next-max(1, m.height-2)+1)
	}
	return m, nil
}

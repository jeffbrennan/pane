package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) startFetch() (tea.Model, tea.Cmd) {
	m.busy = true
	m.activeAction = "fetch"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.Fetch()
		return completed{action: "fetch", message: message, err: err}
	}
}

package main

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type stashState struct {
	entries          []StashEntry
	selected, offset int
	request          int
	loading, viewing bool
	operation        string
	target           StashEntry
}

type stashesLoaded struct {
	entries []StashEntry
	request int
	err     error
}

type stashDiffLoaded struct {
	changes []Change
	meta    CommitInfo
	request int
	err     error
}

func (m model) openStashes() (tea.Model, tea.Cmd) {
	if m.screen == "history" {
		m.history.stream.Close()
		m.history.stream = nil
	}
	m.screen = "stashes"
	m.stash.viewing = false
	m.stash.request++
	m.stash.loading = true
	repo, request := m.repo, m.stash.request
	return m, func() tea.Msg {
		entries, err := repo.Stashes()
		return stashesLoaded{entries: entries, request: request, err: err}
	}
}

func (m model) startStashPush() (tea.Model, tea.Cmd) {
	m.busy, m.activeAction = true, "stash-push"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.StashPush()
		return completed{action: "stash-push", message: message, err: err}
	}
}

func (m model) startStashAction(operation string, target StashEntry) (tea.Model, tea.Cmd) {
	m.modal = ""
	m.busy, m.activeAction = true, "stash-"+operation
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.StashAction(operation, target)
		return completed{action: "stash-" + operation, message: message, err: err}
	}
}

func (m model) handleStashKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	count := len(m.stash.entries)
	switch key.String() {
	case "esc", "q":
		return m.openHistory()
	case "ctrl+c":
		m.closeHistoryStreams()
		return m, tea.Quit
	case "j", "down":
		m.stash.selected = min(m.stash.selected+1, max(0, count-1))
	case "k", "up":
		m.stash.selected = max(0, m.stash.selected-1)
	case "g", "home":
		m.stash.selected = 0
	case "G", "end":
		m.stash.selected = max(0, count-1)
	case "enter", " ":
		if count == 0 || m.stash.loading {
			return m, nil
		}
		entry := m.stash.entries[m.stash.selected]
		m.stash.viewing = true
		m.screen = "commit"
		m.history.current = CommitInfo{Hash: entry.Hash, Subject: entry.Ref + " · " + entry.Message}
		m.history.meta = CommitInfo{}
		m.history.moved = nil
		m.history.request++
		repo := m.startCommitLoad()
		m.history.streamActive = false
		m.history.streamPaths, m.history.streamGutter, m.history.streamError = nil, 0, ""
		m.history.loading = true
		m.history.rows, m.history.changes = nil, nil
		m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
		m.history.expandedDeleted, m.history.jumps = nil, nil
		m.history.jumpIndex = 0
		m.pickerReturn, m.pickerBackToHistory = false, false
		hash, request := entry.Hash, m.history.request
		return m, func() tea.Msg {
			changes, err := repo.StashChanges(hash)
			detail, detailErr := repo.CommitDetail(hash)
			if err == nil {
				err = detailErr
			}
			return stashDiffLoaded{changes: changes, meta: detail, request: request, err: err}
		}
	case "p", "x":
		if count == 0 || m.stash.loading || m.busy {
			return m, nil
		}
		m.stash.operation = "pop"
		if key.String() == "x" {
			m.stash.operation = "drop"
		}
		m.stash.target = m.stash.entries[m.stash.selected]
		m.modal = "stash-confirm"
	case "?":
		m.modal = "help"
	}
	return m, nil
}

func (m model) stashView() string {
	lines := []string{m.titleBar(" Stashes", "")}
	available := max(0, m.height-2)
	selected := min(m.stash.selected, max(0, len(m.stash.entries)-1))
	start := min(m.stash.offset, selected)
	if selected >= start+max(1, available) {
		start = selected - max(1, available) + 1
	}
	for i := 0; i < available; i++ {
		index := start + i
		line := ""
		if index < len(m.stash.entries) {
			entry := m.stash.entries[index]
			prefix := "  "
			if index == selected {
				prefix = m.accent() + "▸ " + styleReset
			}
			line = prefix + clean(entry.Ref+"  "+relativeCommitTime(entry.Date, time.Now())+"  "+entry.Message)
		} else if i == 0 {
			line = " No stashes"
			if m.stash.loading {
				line = " Loading stashes…"
			}
		}
		lines = append(lines, bar(m.width, line, ""))
	}
	status := m.flash
	if m.modal == "help" {
		status = ""
	}
	lines = append(lines, m.statusBar(status, keyHints("↵", "view", "p", "pop", "x", "drop", "esc", "back")))
	if m.modal == "help" {
		clearBody(lines)
		m.showHelpDialog(lines)
	}
	return strings.Join(lines, "\n")
}

func (m model) showStashConfirm(lines []string) {
	action := "Pop"
	warning := "May conflict with local changes."
	if m.stash.operation == "drop" {
		action = "Drop"
		warning = "This cannot be undone."
	}
	boxWidth := min(64, m.width-2)
	placeDialog(lines, m.width, dialog{
		width: boxWidth, title: action + " stash?", titleStyle: m.danger(),
		body: []string{clean(m.stash.target.Ref + " · " + shortHash(m.stash.target.Hash)), clean(m.stash.target.Message), styleDim + warning + styleReset},
		hint: keyHints("y", strings.ToLower(action), "n/esc", "cancel"),
	}.render())
}

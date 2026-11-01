package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

type stashState struct {
	list             listState
	entries          []git.StashEntry
	request          int
	loading, viewing bool
	operation        string
	target           git.StashEntry
}

type stashesLoaded struct {
	entries []git.StashEntry
	request int
	err     error
}

type stashDiffLoaded struct {
	changes []git.Change
	meta    git.CommitInfo
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

func (m model) startStashAction(operation string, target git.StashEntry) (tea.Model, tea.Cmd) {
	m.modal = ""
	m.busy, m.activeAction = true, "stash-"+operation
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.StashAction(operation, target)
		return completed{action: "stash-" + operation, message: message, err: err}
	}
}

func (m model) filteredStashes() []git.StashEntry {
	query := strings.ToLower(strings.TrimSpace(m.stash.list.filter))
	if query == "" {
		return m.stash.entries
	}
	var matches []git.StashEntry
	for _, entry := range m.stash.entries {
		if matchesPicker(entry.Ref+" "+entry.Message, query) {
			matches = append(matches, entry)
		}
	}
	return matches
}

func (m model) handleStashKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.filteredStashes()
	count := len(entries)
	if m.handleListKey(&m.stash.list, key.String(), listRunes(key), count, max(1, m.height-2), nil) {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		return m.openHistory()
	case "ctrl+c":
		m.closeHistoryStreams()
		return m, tea.Quit
	case "enter", " ":
		if count == 0 || m.stash.loading {
			return m, nil
		}
		entry := entries[min(m.stash.list.selected, count-1)]
		m.stash.viewing = true
		m.screen = "commit"
		m.history.current = git.CommitInfo{Hash: entry.Hash, Subject: entry.Ref + " · " + entry.Message}
		m.history.meta = git.CommitInfo{}
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
	case "p", "d":
		if count == 0 || m.stash.loading || m.busy {
			return m, nil
		}
		m.stash.operation = "pop"
		if key.String() == "d" {
			m.stash.operation = "drop"
		}
		m.stash.target = entries[min(m.stash.list.selected, count-1)]
		m.modal = "stash-confirm"
	case "?":
		m.modal = "help"
	}
	return m, nil
}

func (m model) stashView() string {
	entries := m.filteredStashes()
	selected := min(m.stash.list.selected, max(0, len(entries)-1))
	lines := []string{m.titleBar(" "+m.accent()+"stashes"+styleReset, stateBadges(m.whitespaceBadge(), m.filterBadge(m.stash.list.filter)))}
	available := max(0, m.height-2)
	start, _ := listViewport(m.stash.list, len(entries), available)
	now := time.Now()
	if len(entries) == 0 {
		text := " No stashes"
		if m.stash.loading {
			text = " Loading stashes…"
		}
		lines = append(lines, fit(text, m.width))
	}
	for i := start; i < min(len(entries), start+max(1, available)); i++ {
		entry := entries[i]
		lines = append(lines, m.listRow(i, selected, len(entries),
			entry.Ref+"  "+git.RelativeCommitTime(entry.Date, now)+"  "+entry.Message, ""))
	}
	status := m.flash
	if m.modal == "help" {
		status = ""
	} else if filter := m.stash.list.listStatus(m.width); filter != "" {
		status = filter
	}
	lines = append(lines, m.statusBar(status, footer(true, shortcut{"↵", "view"}, shortcut{"p", "pop"}, shortcut{"d", "drop"})))
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
		body: []string{syntax.Clean(m.stash.target.Ref + " · " + shortHash(m.stash.target.Hash)), syntax.Clean(m.stash.target.Message), styleDim + warning + styleReset},
		hint: keyHints("y", strings.ToLower(action), "n/esc", "cancel"),
	}.render())
}

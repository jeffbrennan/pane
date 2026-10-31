package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type stashEntry struct{ ref, hash, date, message string }

type stashState struct {
	entries          []stashEntry
	selected, offset int
	request          int
	loading, viewing bool
	operation        string
	target           stashEntry
}

type stashesLoaded struct {
	entries []stashEntry
	request int
	err     error
}

type stashDiffLoaded struct {
	changes []change
	request int
	err     error
}

func (r repository) stashes() ([]stashEntry, error) {
	out, err := r.command("stash", "list", "-z", "--format=%gd%x00%H%x00%cI%x00%gs")
	if err != nil {
		return nil, gitError(out, err)
	}
	fields := bytes.Split(out, []byte{0})
	var entries []stashEntry
	for i := 0; i < len(fields) && len(fields[i]) > 0; i += 4 {
		if i+3 >= len(fields) {
			return nil, fmt.Errorf("unexpected Git stash format")
		}
		entries = append(entries, stashEntry{ref: string(fields[i]), hash: string(fields[i+1]), date: string(fields[i+2]), message: string(fields[i+3])})
	}
	return entries, nil
}

func (r repository) stashChanges(hash string) ([]change, error) {
	changes, err := r.commitChanges(hash)
	if err != nil {
		return nil, err
	}
	parents, err := r.command("rev-list", "--parents", "-n", "1", hash)
	if err != nil {
		return nil, gitError(parents, err)
	}
	fields := strings.Fields(string(parents))
	if len(fields) >= 4 {
		untracked, err := r.commitChanges(fields[3])
		if err != nil {
			return nil, err
		}
		for i := range untracked {
			untracked[i].untracked = true
		}
		changes = append(changes, untracked...)
	}
	return changes, nil
}

func (r repository) stashPush() (string, error) {
	_, entries, err := r.status()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "Nothing to stash", nil
	}
	out, err := r.action("stash", "push", "--include-untracked")
	if err != nil {
		return "", err
	}
	if strings.Contains(out, "No local changes to save") {
		return "Nothing to stash", nil
	}
	label := "files"
	if len(entries) == 1 {
		label = "file"
	}
	return fmt.Sprintf("Stashed %d %s", len(entries), label), nil
}

func (r repository) stashAction(operation string, target stashEntry) (string, error) {
	entries, err := r.stashes()
	if err != nil {
		return "", err
	}
	valid := false
	for _, entry := range entries {
		if entry.ref == target.ref && entry.hash == target.hash {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("stash list changed; reopen the picker and try again")
	}
	_, err = r.action("stash", operation, target.ref)
	if err != nil {
		return "", err
	}
	if operation == "pop" {
		return "Popped " + target.ref, nil
	}
	return "Dropped " + target.ref, nil
}

func (m model) openStashes() (tea.Model, tea.Cmd) {
	if m.screen == "history" {
		m.history.stream.close()
		m.history.stream = nil
	}
	m.screen = "stashes"
	m.stash.viewing = false
	m.stash.request++
	m.stash.loading = true
	repo, request := m.repo, m.stash.request
	return m, func() tea.Msg {
		entries, err := repo.stashes()
		return stashesLoaded{entries: entries, request: request, err: err}
	}
}

func (m model) startStashPush() (tea.Model, tea.Cmd) {
	m.busy, m.activeAction = true, "stash-push"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.stashPush()
		return completed{action: "stash-push", message: message, err: err}
	}
}

func (m model) startStashAction(operation string, target stashEntry) (tea.Model, tea.Cmd) {
	m.modal = ""
	m.busy, m.activeAction = true, "stash-"+operation
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.stashAction(operation, target)
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
		m.history.current = commitInfo{hash: entry.hash, subject: entry.ref + " · " + entry.message}
		m.history.request++
		m.history.loading = true
		m.history.rows, m.history.changes = nil, nil
		m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
		m.history.expandedDeleted, m.history.jumps = nil, nil
		m.history.jumpIndex = 0
		m.pickerReturn, m.pickerBackToHistory = false, false
		repo, hash, request := m.repo, entry.hash, m.history.request
		return m, func() tea.Msg {
			changes, err := repo.stashChanges(hash)
			return stashDiffLoaded{changes: changes, request: request, err: err}
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
			line = prefix + clean(entry.ref+"  "+relativeCommitTime(entry.date, time.Now())+"  "+entry.message)
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
		body: []string{clean(m.stash.target.ref + " · " + shortHash(m.stash.target.hash)), clean(m.stash.target.message), styleDim + warning + styleReset},
		hint: keyHints("y", strings.ToLower(action), "n/esc", "cancel"),
	}.render())
}

package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

type branchState struct {
	entries          []git.BranchEntry
	selected, offset int
	filter, draft    string
	draftCursor      int
	request          int
	searching        bool
	loading          bool
	remote           bool
	newFrom          string
	confirm          git.BranchEntry
	confirmForce     bool
}

type branchesLoaded struct {
	entries []git.BranchEntry
	request int
	err     error
}

type branchActionDone struct {
	message string
	err     error
	stay    bool
}

func (m model) openBranches() (tea.Model, tea.Cmd) {
	m.screen = "branches"
	m.branch = branchState{loading: true}
	m.branch.request++
	return m, m.loadBranches()
}

func (m model) loadBranches() tea.Cmd {
	repo, request, remote := m.repo, m.branch.request, m.branch.remote
	return func() tea.Msg {
		entries, err := repo.Branches(remote)
		return branchesLoaded{entries: entries, request: request, err: err}
	}
}

func (m model) filteredBranches() []git.BranchEntry {
	query := strings.ToLower(strings.TrimSpace(m.branch.filter))
	if query == "" {
		return m.branch.entries
	}
	var matches []git.BranchEntry
	for _, entry := range m.branch.entries {
		if matchesPicker(entry.Name, query) {
			matches = append(matches, entry)
		}
	}
	return matches
}

func (m model) branchViewport(count int) (int, int) {
	available := max(0, m.height-2)
	selected := min(m.branch.selected, max(0, count-1))
	offset := m.branch.offset
	if selected < offset {
		offset = selected
	}
	if selected >= offset+available {
		offset = selected - available + 1
	}
	return min(offset, count), max(1, available)
}

func branchTrack(track string) string {
	track = strings.Trim(track, "[]")
	if track == "" || track == "gone" {
		return track
	}
	track = strings.ReplaceAll(track, "ahead ", "↑")
	track = strings.ReplaceAll(track, "behind ", "↓")
	track = strings.ReplaceAll(track, ", ", " ")
	return track
}

func (m model) branchView() string {
	lines := []string{}
	title := m.accent() + "branches" + styleReset
	if m.branch.remote {
		title += styleDim + " · including remotes" + styleReset
	}
	lines = append(lines, m.titleBar(" "+title, styleDim+"read-only"+styleReset))
	now := time.Now()
	matches := m.filteredBranches()
	offset, visible := m.branchViewport(len(matches))
	if len(matches) == 0 {
		text := " No branches"
		if m.branch.loading {
			text = " Loading branches…"
		}
		lines = append(lines, fit(text, m.width))
	}
	for i := offset; i < min(len(matches), offset+visible); i++ {
		entry := matches[i]
		marker := "  "
		if i == m.branch.selected {
			marker = "▸ "
		}
		current := " "
		if entry.Current {
			current = "*"
		}
		name := entry.Name
		meta := []string{branchTrack(entry.Track)}
		if entry.Worktree != "" {
			meta = append(meta, "⌂ "+filepath.Base(entry.Worktree))
		}
		meta = append(meta, git.HistoryCommitTime(entry.Date, now), syntax.Clean(entry.Subject))
		var parts []string
		for _, part := range meta {
			if part == "" {
				continue
			}
			parts = append(parts, part)
		}
		row := fit(fmt.Sprintf("%s%s %s  %s", marker, current, syntax.Clean(name), strings.Join(parts, "  ")), m.width)
		if i == m.branch.selected {
			row = "\x1b[7m" + row + strings.Repeat(" ", max(0, m.width-stringWidth(row))) + styleReset
		} else {
			row = styleDim + row + styleReset
		}
		lines = append(lines, row)
	}
	status, hints := "", keyHints("↵", "switch", "n", "new", "d", "delete", "r", "remotes", "?", "help")
	if m.branch.searching {
		status = "/" + renderInput(m.branch.draft, m.branch.draftCursor, max(1, m.width-1))
		hints = keyHints("↵", "filter", "esc", "cancel")
	} else if m.branch.filter != "" {
		status = "/" + syntax.Clean(m.branch.filter)
	}
	if m.flash != "" {
		status = syntax.Clean(m.flash)
	}
	if m.modal == "branch-new" {
		status, hints = "New branch from "+shortHash(m.branch.newFrom)+": "+renderInput(m.input, m.inputCursor, max(1, m.width-32)), keyHints("↵", "create", "esc", "cancel")
	}
	lines = append(lines, m.statusBar(status, hints))
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
}

func (m model) handleBranchKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyString := key.String()
	if m.branch.searching {
		switch keyString {
		case "esc":
			m.branch.searching = false
		case "enter":
			m.branch.filter = m.branch.draft
			m.branch.searching = false
			m.branch.selected, m.branch.offset = 0, 0
		default:
			var runes []rune
			if key.Type == tea.KeyRunes {
				runes = key.Runes
			} else if key.Type == tea.KeySpace {
				runes = []rune{' '}
			}
			m.branch.draft, m.branch.draftCursor, _ = editLine(m.branch.draft, m.branch.draftCursor, keyString, runes)
			m.branch.filter = m.branch.draft
			m.branch.selected, m.branch.offset = 0, 0
		}
		return m, nil
	}
	matches := m.filteredBranches()
	page := max(1, m.height-2)
	switch keyString {
	case "esc", "q":
		m.screen = "history"
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "j", "down":
		m.branch.selected = min(m.branch.selected+1, max(0, len(matches)-1))
	case "k", "up":
		m.branch.selected = max(0, m.branch.selected-1)
	case "ctrl+d":
		m.branch.selected = min(m.branch.selected+page/2, max(0, len(matches)-1))
	case "ctrl+u":
		m.branch.selected = max(0, m.branch.selected-page/2)
	case "g", "home":
		m.branch.selected = 0
	case "G", "end":
		m.branch.selected = max(0, len(matches)-1)
	case "/":
		m.branch.searching = true
		m.branch.draft = m.branch.filter
		m.branch.draftCursor = len([]rune(m.branch.draft))
	case "tab":
		return m.openWorktrees()
	case "r":
		m.branch.remote = !m.branch.remote
		m.branch.loading = true
		m.branch.request++
		return m, m.loadBranches()
	case "y":
		if len(matches) > 0 {
			name := matches[min(m.branch.selected, len(matches)-1)].Name
			return m, copyCmd(name, name)
		}
	case "enter":
		if len(matches) > 0 {
			return m.switchBranch(matches[min(m.branch.selected, len(matches)-1)])
		}
	case "n":
		if len(matches) > 0 {
			entry := matches[min(m.branch.selected, len(matches)-1)]
			return m.promptNewBranch(entry.Ref)
		}
	case "w":
		if len(matches) > 0 {
			entry := matches[min(m.branch.selected, len(matches)-1)]
			return m.promptNewWorktree(entry.Name, entry.Ref, true)
		}
	case "d", "D":
		if len(matches) > 0 {
			return m.confirmDeleteBranch(matches[min(m.branch.selected, len(matches)-1)], keyString == "D")
		}
	}
	return m, nil
}

func (m model) promptNewBranch(from string) (tea.Model, tea.Cmd) {
	m.branch.newFrom = from
	m.input, m.inputCursor = "", 0
	m.modal = "branch-new"
	return m, nil
}

func (m model) confirmDeleteBranch(entry git.BranchEntry, force bool) (tea.Model, tea.Cmd) {
	if entry.Remote {
		m.flash = "Remote branches cannot be deleted here"
		return m, nil
	}
	if entry.Current {
		m.flash = "Cannot delete the checked out branch"
		return m, nil
	}
	if entry.Worktree != "" {
		m.flash = "Checked out in " + entry.Worktree
		return m, nil
	}
	m.branch.confirm, m.branch.confirmForce = entry, force
	m.modal = "branch-delete"
	return m, nil
}

func (m model) runBranchAction(message string, stay bool, action func(git.Repo) (string, error)) (tea.Model, tea.Cmd) {
	repo := m.repo
	return m, func() tea.Msg {
		out, err := action(repo)
		if err != nil {
			return branchActionDone{message: out, err: err, stay: stay}
		}
		return branchActionDone{message: message, stay: stay}
	}
}

func (m model) switchBranch(entry git.BranchEntry) (tea.Model, tea.Cmd) {
	if entry.Worktree != "" {
		m.flash = "Checked out in " + entry.Worktree
		return m, nil
	}
	return m.runBranchAction("Switched to "+entry.Name, false, func(r git.Repo) (string, error) {
		args := []string{"switch"}
		if entry.Remote {
			remote, local, ok := strings.Cut(entry.Name, "/")
			if !ok || local == "" {
				return "", fmt.Errorf("cannot switch to %s", entry.Name)
			}
			if out, err := r.Command("rev-parse", "--verify", "--quiet", "refs/heads/"+local); err == nil && len(bytes.TrimSpace(out)) > 0 {
				args = append(args, local)
			} else {
				args = append(args, "--track", remote+"/"+local)
			}
		} else {
			args = append(args, entry.Name)
		}
		return r.Action(args...)
	})
}

func (m model) createBranch(name, from string) (tea.Model, tea.Cmd) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t") {
		m.flash = "Branch name must be a single word"
		return m, nil
	}
	return m.runBranchAction("Created branch "+name, false, func(r git.Repo) (string, error) {
		return r.Action("switch", "-c", name, from)
	})
}

func (m model) deleteBranch(entry git.BranchEntry, force bool) (tea.Model, tea.Cmd) {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return m.runBranchAction("Deleted branch "+entry.Name, true, func(r git.Repo) (string, error) {
		return r.Action("branch", flag, entry.Name)
	})
}

func (m model) reloadBranches() (model, tea.Cmd) {
	m.branch.loading = true
	m.branch.request++
	return m, m.loadBranches()
}

// branchNames presents branches in a stable order for tests.
func branchNames(entries []git.BranchEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	sort.Strings(names)
	return names
}

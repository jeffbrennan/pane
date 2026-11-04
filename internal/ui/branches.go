package ui

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

type branchState struct {
	entries      []git.BranchEntry
	list         listState
	request      int
	loading      bool
	remote       bool
	newFrom      string
	confirm      git.BranchEntry
	confirmForce bool
	// leaveTo is the branch pane switches to before deleting the checked-out
	// branch; empty for any other branch.
	leaveTo string
	// unmerged is set when git branch -d refused and pane offers -D.
	unmerged *unmergedBranch
}

type unmergedBranch struct {
	entry git.BranchEntry
	count int
	head  string
}

type branchesLoaded struct {
	entries []git.BranchEntry
	request int
	err     error
}

type branchActionDone struct {
	message  string
	err      error
	stay     bool
	unmerged *unmergedBranch
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
	query := strings.ToLower(strings.TrimSpace(m.branch.list.filter))
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
	lines = append(lines, m.titleBar(" "+title, stateBadges(m.whitespaceBadge(), m.filterBadge(m.branch.list.filter))))
	now := time.Now()
	matches := m.filteredBranches()
	selected := min(m.branch.list.selected, max(0, len(matches)-1))
	offset, visible := listViewport(m.branch.list, len(matches), max(0, m.height-2))
	if len(matches) == 0 {
		text := " No branches"
		if m.branch.loading {
			text = " Loading branches…"
		}
		lines = append(lines, fit(text, m.width))
	}
	for i := offset; i < min(len(matches), offset+visible); i++ {
		entry := matches[i]
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
		lines = append(lines, m.listRow(i, selected, len(matches), current+" "+name+"  "+strings.Join(parts, "  "), ""))
	}
	status := ""
	if filter := m.branch.list.listStatus(m.width); filter != "" {
		status = filter
	}
	hints := footer(true, shortcut{"↵", "switch"}, shortcut{"n", "new"}, shortcut{"d", "delete"}, shortcut{"r", "remotes"})
	if m.flash != "" {
		status = syntax.Clean(m.flash)
	}
	if m.modal == "branch-new" {
		status, hints = m.newBranchStatus()
	}
	lines = append(lines, m.statusBar(status, hints))
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
}

func (m model) handleBranchKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyString := key.String()
	matches := m.filteredBranches()
	if m.handleListKey(&m.branch.list, key.String(), listRunes(key), len(matches), max(1, m.height-2), nil) {
		return m, nil
	}
	switch keyString {
	case "esc", "q":
		m.screen = "history"
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.help.reset()
	case "tab":
		return m.openWorktrees()
	case "r":
		m.branch.remote = !m.branch.remote
		m.branch.loading = true
		m.branch.request++
		return m, m.loadBranches()
	case "y":
		if len(matches) > 0 {
			name := matches[min(m.branch.list.selected, len(matches)-1)].Name
			return m, copyCmd(name, name)
		}
	case "enter":
		if len(matches) > 0 {
			return m.switchBranch(matches[min(m.branch.list.selected, len(matches)-1)])
		}
	case "n":
		if len(matches) > 0 {
			entry := matches[min(m.branch.list.selected, len(matches)-1)]
			return m.promptNewBranch(entry.Ref)
		}
	case "w":
		if len(matches) > 0 {
			entry := matches[min(m.branch.list.selected, len(matches)-1)]
			return m.promptNewWorktree(entry.Name, entry.Ref, true)
		}
	case "d", "D":
		if len(matches) > 0 {
			return m.confirmDeleteBranch(matches[min(m.branch.list.selected, len(matches)-1)], keyString == "D")
		}
	}
	return m, nil
}

// refLabel names a ref the way a person would: branches drop their refs/
// prefix, while a bare commit hash keeps its short form.
func refLabel(ref string) string {
	label := strings.TrimPrefix(strings.TrimPrefix(ref, "refs/remotes/"), "refs/heads/")
	if (len(label) == 40 || len(label) == 64) && strings.Trim(label, "0123456789abcdef") == "" {
		return shortHash(label)
	}
	return label
}

// newBranchStatus renders the create-branch prompt with the input sized to
// the room the real label leaves on the status bar.
func (m model) newBranchStatus() (string, string) {
	prefix := "New branch from " + refLabel(m.branch.newFrom) + ": "
	hints := keyHints("↵", "create", "esc", "cancel")
	width := max(1, m.width-ansi.StringWidth(prefix)-ansi.StringWidth(hints)-2)
	return prefix + renderInput(m.input, m.inputCursor, width), hints
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
	m.branch.leaveTo, m.branch.unmerged = "", nil
	if entry.Current {
		if m.hasTrackedChanges() {
			m.flash = "Commit or stash changes before deleting the checked out branch"
			return m, nil
		}
		if m.branch.leaveTo = m.repo.DefaultBranchFor(entry.Name); m.branch.leaveTo == "" {
			m.flash = "Cannot delete the checked out branch"
			return m, nil
		}
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

// hasTrackedChanges ignores untracked files, which survive a branch switch.
func (m model) hasTrackedChanges() bool {
	for _, changes := range [][]git.Change{m.snapshot.Staged, m.snapshot.Unstaged} {
		for _, c := range changes {
			if !c.Untracked {
				return true
			}
		}
	}
	return false
}

func (m model) deleteBranch(entry git.BranchEntry, force bool) (tea.Model, tea.Cmd) {
	target := m.branch.leaveTo
	message := "Deleted branch " + entry.Name
	if target != "" {
		message = "Switched to " + target + " and deleted " + entry.Name
	}
	repo := m.repo
	return m, func() tea.Msg {
		var err error
		if target != "" {
			_, err = repo.LeaveAndDeleteBranch(entry.Name, target, force)
		} else {
			_, err = repo.DeleteBranch(entry.Name, force)
		}
		if count, head, ok := repo.UnmergedDelete(entry.Name, err); ok && !force {
			return branchActionDone{unmerged: &unmergedBranch{entry: entry, count: count, head: head}}
		}
		if err != nil {
			return branchActionDone{err: err, stay: target == ""}
		}
		return branchActionDone{message: message, stay: target == ""}
	}
}

// offerForceDelete reopens the delete dialog as a forced delete after git
// branch -d refused, saying how many commits the branch would take with it.
// Leaving the current branch may already have switched, so pane refreshes.
func (m model) offerForceDelete(unmerged *unmergedBranch) (tea.Model, tea.Cmd) {
	m.branch.confirm, m.branch.confirmForce = unmerged.entry, true
	m.branch.leaveTo = ""
	m.branch.unmerged = unmerged
	m.modal = "branch-delete"
	m, branchCmd := m.reloadBranches()
	m, refreshCmd := m.refresh()
	return m, tea.Batch(branchCmd, refreshCmd)
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

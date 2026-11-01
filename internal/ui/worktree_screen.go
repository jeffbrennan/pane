package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

type worktreeStatus struct {
	dirty    int
	activity time.Time
	loaded   bool
}

type worktreeState struct {
	entries          []git.WorktreeEntry
	status           map[string]worktreeStatus
	selected, offset int
	filter, draft    string
	draftCursor      int
	request          int
	searching        bool
	loading          bool
	confirm          git.WorktreeEntry
	confirmForce     bool
	newPath          string
	newBranch        string
	newStart         string
	newExisting      bool
}

type worktreesLoaded struct {
	entries []git.WorktreeEntry
	request int
	err     error
}

type worktreeStatusLoaded struct {
	path    string
	status  worktreeStatus
	request int
	err     error
}

type worktreeActionDone struct {
	message string
	err     error
}

func (m model) openWorktrees() (tea.Model, tea.Cmd) {
	m.screen = "worktrees"
	m.worktrees = worktreeState{loading: true, status: make(map[string]worktreeStatus)}
	m.worktrees.request++
	return m, m.loadWorktrees()
}

func (m model) loadWorktrees() tea.Cmd {
	repo, request := m.repo, m.worktrees.request
	return func() tea.Msg {
		entries, err := repo.Worktrees()
		return worktreesLoaded{entries: entries, request: request, err: err}
	}
}

func loadWorktreeStatus(repo git.Repo, path string, request int) tea.Cmd {
	return func() tea.Msg {
		dirty, activity, err := repo.StatusCount(path)
		return worktreeStatusLoaded{path: path, request: request, err: err, status: worktreeStatus{dirty: dirty, activity: activity, loaded: true}}
	}
}

func (m model) sortedWorktrees() []git.WorktreeEntry {
	entries := append([]git.WorktreeEntry(nil), m.worktrees.entries...)
	query := strings.ToLower(strings.TrimSpace(m.worktrees.filter))
	if query != "" {
		var filtered []git.WorktreeEntry
		for _, entry := range entries {
			if matchesPicker(entry.Path, query) || matchesPicker(entry.Branch, query) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := m.worktrees.status[entries[i].Path], m.worktrees.status[entries[j].Path]
		if left.loaded && right.loaded && !left.activity.Equal(right.activity) {
			return left.activity.After(right.activity)
		}
		return entries[i].Path < entries[j].Path
	})
	return entries
}

func (m model) worktreeViewport(count int) (int, int) {
	available := max(1, m.height-2)
	selected := min(m.worktrees.selected, max(0, count-1))
	offset := m.worktrees.offset
	if selected < offset {
		offset = selected
	}
	if selected >= offset+available {
		offset = selected - available + 1
	}
	return min(offset, count), available
}

func (m model) worktreeView() string {
	lines := []string{m.titleBar(" "+m.accent()+"worktrees"+styleReset, styleDim+"read-only"+styleReset)}
	entries := m.sortedWorktrees()
	offset, visible := m.worktreeViewport(len(entries))
	current, _ := os.Getwd()
	if len(entries) == 0 {
		text := " No worktrees"
		if m.worktrees.loading {
			text = " Loading worktrees…"
		}
		lines = append(lines, fit(text, m.width))
	}
	now := time.Now()
	for i := offset; i < min(len(entries), offset+visible); i++ {
		entry := entries[i]
		marker := "  "
		if i == m.worktrees.selected {
			marker = "▸ "
		}
		here := " "
		if entry.Path == current {
			here = "*"
		}
		branch := entry.Branch
		if branch == "" {
			branch = "(detached)"
		}
		status := m.worktrees.status[entry.Path]
		parts := []string{branch}
		if status.loaded {
			if status.dirty > 0 {
				parts = append(parts, fmt.Sprintf("%d changed", status.dirty))
			} else {
				parts = append(parts, "clean")
			}
			if !status.activity.IsZero() {
				parts = append(parts, git.HistoryCommitTime(status.activity.Format(time.RFC3339), now))
			}
		} else {
			parts = append(parts, "…")
		}
		if entry.Locked {
			parts = append(parts, "locked")
		}
		if entry.Prunable {
			parts = append(parts, "prunable")
		}
		path := git.WorktreeDisplayPath(m.worktrees.entries, entry.Path)
		row := fit(fmt.Sprintf("%s%s %s  %s  %s", marker, here, syntax.Clean(path), syntax.Clean(strings.Join(parts, "  ")), ""), m.width)
		if i == m.worktrees.selected {
			row = "\x1b[7m" + row + strings.Repeat(" ", max(0, m.width-stringWidth(row))) + styleReset
		} else {
			row = styleDim + row + styleReset
		}
		lines = append(lines, row)
	}
	hintKeys := []string{"↵", "switch", "n", "new", "d", "remove"}
	if m.anyPrunable() {
		hintKeys = append(hintKeys, "p", "prune")
	}
	status, hints := "", keyHints(hintKeys...)
	if m.worktrees.searching {
		status = "/" + renderInput(m.worktrees.draft, m.worktrees.draftCursor, max(1, m.width-1))
		hints = keyHints("↵", "filter", "esc", "cancel")
	} else if m.worktrees.filter != "" {
		status = "/" + syntax.Clean(m.worktrees.filter)
	}
	if m.flash != "" {
		status = syntax.Clean(m.flash)
	}
	if m.modal == "worktree-new" {
		status, hints = "New worktree (path  branch): "+renderInput(m.input, m.inputCursor, max(1, m.width-34)), keyHints("↵", "create", "esc", "cancel")
	}
	lines = append(lines, m.statusBar(status, hints))
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
}

func (m model) handleWorktreeKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyString := key.String()
	if m.worktrees.searching {
		switch keyString {
		case "esc":
			m.worktrees.searching = false
		case "enter":
			m.worktrees.filter = m.worktrees.draft
			m.worktrees.searching = false
			m.worktrees.selected, m.worktrees.offset = 0, 0
		default:
			var runes []rune
			if key.Type == tea.KeyRunes {
				runes = key.Runes
			} else if key.Type == tea.KeySpace {
				runes = []rune{' '}
			}
			m.worktrees.draft, m.worktrees.draftCursor, _ = editLine(m.worktrees.draft, m.worktrees.draftCursor, keyString, runes)
			m.worktrees.filter = m.worktrees.draft
			m.worktrees.selected, m.worktrees.offset = 0, 0
		}
		return m, nil
	}
	entries := m.sortedWorktrees()
	switch keyString {
	case "esc", "q":
		m.screen = "history"
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "tab":
		return m.openBranches()
	case "j", "down":
		m.worktrees.selected = min(m.worktrees.selected+1, max(0, len(entries)-1))
	case "k", "up":
		m.worktrees.selected = max(0, m.worktrees.selected-1)
	case "g", "home":
		m.worktrees.selected = 0
	case "G", "end":
		m.worktrees.selected = max(0, len(entries)-1)
	case "/":
		m.worktrees.searching = true
		m.worktrees.draft = m.worktrees.filter
		m.worktrees.draftCursor = len([]rune(m.worktrees.draft))
	case "r":
		m.worktrees.loading = true
		m.worktrees.request++
		return m, m.loadWorktrees()
	case "y":
		if len(entries) > 0 {
			path := entries[min(m.worktrees.selected, len(entries)-1)].Path
			return m, copyCmd(path, path)
		}
	case "enter":
		if len(entries) > 0 {
			return m.switchWorktree(entries[min(m.worktrees.selected, len(entries)-1)].Path)
		}
	case "n":
		return m.promptNewWorktree("", "", false)
	case "d", "D":
		if len(entries) > 0 {
			return m.confirmRemoveWorktree(entries[min(m.worktrees.selected, len(entries)-1)], keyString == "D")
		}
	case "p":
		if m.anyPrunable() {
			return m.runWorktreeAction("Pruned worktrees", func(r git.Repo) (string, error) {
				return r.Action("worktree", "prune")
			})
		}
	}
	return m, nil
}

func (m model) anyPrunable() bool {
	for _, entry := range m.worktrees.entries {
		if entry.Prunable {
			return true
		}
	}
	return false
}

func (m model) promptNewWorktree(branch, start string, existing bool) (tea.Model, tea.Cmd) {
	if branch == "" {
		branch = "new-branch"
	}
	entries := m.worktrees.entries
	if len(entries) == 0 {
		entries, _ = m.repo.Worktrees()
		m.worktrees.entries = entries
	}
	path := git.WorktreePathFor(entries, m.repo.Root, branch)
	m.worktrees.newPath, m.worktrees.newBranch, m.worktrees.newExisting = path, branch, existing
	m.worktrees.newStart = start
	m.input, m.inputCursor = path+"  "+branch, len([]rune(path+"  "+branch))
	m.modal = "worktree-new"
	return m, nil
}

func (m model) confirmRemoveWorktree(entry git.WorktreeEntry, force bool) (tea.Model, tea.Cmd) {
	current, _ := os.Getwd()
	if entry.Path == current {
		m.flash = "Cannot remove the current worktree"
		return m, nil
	}
	m.worktrees.confirm, m.worktrees.confirmForce = entry, force
	m.modal = "worktree-delete"
	return m, nil
}

func (m model) runWorktreeAction(message string, action func(git.Repo) (string, error)) (tea.Model, tea.Cmd) {
	repo := m.repo
	return m, func() tea.Msg {
		out, err := action(repo)
		if err != nil {
			return worktreeActionDone{message: out, err: err}
		}
		return worktreeActionDone{message: message}
	}
}

func (m model) createWorktree() (tea.Model, tea.Cmd) {
	fields := strings.Fields(m.input)
	if len(fields) < 2 {
		m.flash = "Enter a path and a branch"
		return m, nil
	}
	path, branch := fields[0], fields[1]
	existing := m.worktrees.newExisting
	start := m.worktrees.newStart
	if start == "" {
		start = "HEAD"
	}
	return m.runWorktreeAction("Created worktree "+path, func(r git.Repo) (string, error) {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("%s already exists", path)
		}
		var out string
		var err error
		if existing {
			out, err = r.Action("worktree", "add", path, branch)
		} else {
			out, err = r.Action("worktree", "add", "-b", branch, path, start)
		}
		if err == nil {
			_ = copyToClipboard(path)
		}
		return out, err
	})
}

func (m model) removeWorktree(entry git.WorktreeEntry, force bool) (tea.Model, tea.Cmd) {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, entry.Path)
	return m.runWorktreeAction("Removed worktree "+git.WorktreeDisplayPath(m.worktrees.entries, entry.Path), func(r git.Repo) (string, error) {
		return r.Action(args...)
	})
}

// switchWorktree relaunches the pane inside another worktree, keeping session
// toggles but dropping state tied to the old root.
func (m model) switchWorktree(path string) (tea.Model, tea.Cmd) {
	current, _ := os.Getwd()
	if path == current {
		m.flash = "Already in this worktree"
		return m, nil
	}
	if err := os.Chdir(path); err != nil {
		m.flash = err.Error()
		return m, nil
	}
	repo, err := git.OpenRepository()
	if err != nil {
		m.flash = err.Error()
		return m, nil
	}
	next := model{
		repo: repo, width: m.width, height: m.height, wrap: m.wrap,
		lightTheme: m.lightTheme, themeAuto: m.themeAuto, themeNotify: m.themeNotify,
		loading: true, startup: true, viewed: newViewedStore(repo.Root),
	}
	return next, next.Init()
}

func (m model) reloadWorktrees() (model, tea.Cmd) {
	m.worktrees.loading = true
	m.worktrees.request++
	return m, m.loadWorktrees()
}

func worktreeEntryFor(entries []git.WorktreeEntry, path string) *git.WorktreeEntry {
	for i := range entries {
		if entries[i].Path == path {
			return &entries[i]
		}
	}
	return nil
}

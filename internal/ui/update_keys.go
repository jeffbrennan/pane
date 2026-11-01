package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	if m.modal != "" {
		return m.handleModal(msg)
	}
	if msg.String() == "L" && m.pending == "" && (m.screen == "" || m.screen == "commit" || m.screen == "base" || m.screen == "sequence") {
		return m.openFileHistory()
	}
	if msg.String() == "F" && m.pending == "" && !(m.screen == "history" && m.history.list.filtering) {
		if !m.busy {
			return m.startFetch()
		}
		return m, nil
	}
	if msg.String() == "W" && m.pending == "" && (m.screen == "" || m.screen == "commit" || m.screen == "sequence" || m.screen == "base") {
		return m.toggleWhitespace()
	}
	if msg.String() == "B" && m.pending == "" && (m.screen == "" || m.screen == "commit" || m.screen == "sequence" || m.screen == "base") {
		m.shortPaths = !m.shortPaths
		m.flash = "Full paths"
		if m.shortPaths {
			m.flash = "Short paths"
		}
		if m.screen == "commit" || m.screen == "base" {
			m.rebuildCommit()
		} else {
			m.rebuild()
		}
		return m, nil
	}
	if msg.String() == "T" && m.pending == "" && !(m.screen == "history" && m.history.list.filtering) {
		m.lightTheme = !m.lightTheme
		m.themeAuto = false
		m.flash = "Dark colors"
		if m.lightTheme {
			m.flash = "Light colors"
		}
		return m, nil
	}
	if m.screen != "" {
		if m.screen == "stashes" {
			return m.handleStashKey(msg)
		}
		if m.screen == "branches" {
			return m.handleBranchKey(msg)
		}
		if m.screen == "worktrees" {
			return m.handleWorktreeKey(msg)
		}
		if m.screen == "hotspots" {
			return m.handleHotspotKey(msg)
		}
		if m.screen == "sequence" {
			return m.handleSequenceKey(msg)
		}
		updated, cmd := m.handleHistoryKey(msg)
		next := updated.(model)
		next.revealHistorySelection()
		next, more := next.loadHistoryBackground()
		if more == nil {
			return next, cmd
		}
		if cmd == nil {
			return next, more
		}
		return next, tea.Batch(cmd, more)
	}
	if m.pending == "" && (msg.String() == "h" || msg.String() == "l") && m.onDiffFile() {
		return m.changeContext(msg.String())
	}
	if m.handleDiffNavigation(msg.String()) || m.handleDiffTool(msg.String()) {
		return m, nil
	}
	switch msg.String() {
	case "o":
		return m.openEditor()
	case "y":
		if len(m.rows) > 0 {
			path := m.rows[m.pos].path
			return m, copyCmd(path, path)
		}
	case "Y":
		return m, m.copyDiffLocation("")
	case "esc", "q":
		if m.pickerReturn {
			m.restorePicker()
			return m, nil
		}
		if msg.String() == "q" {
			return m, tea.Quit
		}
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.pickerReturn, m.pickerBackToHistory = false, false
		m.staged = !m.staged
		m.jumps = nil
		m.jumpIndex = 0
		m.pos = 0
		m.viewTop = 0
		m.rows = nil
		m.rebuild()
	case "w":
		m.wrap = !m.wrap
		m.horizontal = 0
		m.flash = wrapFlash(m.wrap)
		m.rebuild()
	case "r":
		m.refreshCache = nil
		m.generation++
		return m.refresh()
	case "d":
		return m.startDiscard()
	case "?":
		m.modal = "help"
		m.help.reset()
	case "H":
		return m.openHistory()
	case "S":
		if !m.busy {
			return m.startStashPush()
		}
	case " ", "s":
		return m.stageSelection(msg.String() == "s")
	case "c":
		if !m.busy {
			if len(m.snapshot.Staged) == 0 && len(m.snapshot.Unstaged) == 0 {
				m.message = "Nothing to commit"
				return m, nil
			}
			m.modal = "commit"
			m.input = ""
			m.inputCursor = 0
			m.inputBody = ""
			m.amendHash = ""
			m.message = ""
			m.recentCommits = nil
			m.recentCommitLoading = true
			m.recentCommitError = false
			m.recentCommitRequest++
			repo, request := m.repo, m.recentCommitRequest
			return m, func() tea.Msg {
				commits, err := repo.HistoryWithDate(0, 5, "%cI")
				return recentCommitsLoaded{commits: commits, err: err, request: request}
			}
		}
	case "p":
		if !m.busy {
			return m.runAction("push")
		}
	case "P":
		if !m.busy {
			return m.runAction("pull")
		}
	}
	return m, nil
}

// handleDiffTool handles the search and file picker keys shared by the
// working-tree and commit diff views.
func (m *model) handleDiffTool(key string) bool {
	switch key {
	case "/":
		m.startSearch()
	case "n":
		m.searchJump(m.searchDirection())
	case "N":
		m.searchJump(-m.searchDirection())
	case "f":
		m.openPicker()
	case "e":
		m.toggleDeletedFile()
	default:
		return false
	}
	return true
}

func (m model) handleModal(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case "branch-new":
		switch key.String() {
		case "enter":
			m.modal = ""
			return m.createBranch(m.input, m.branch.newFrom)
		case "esc", "ctrl+c":
			m.modal = ""
		default:
			var inserted []rune
			if key.Type == tea.KeyRunes {
				inserted = key.Runes
			} else if key.Type == tea.KeySpace {
				inserted = []rune{' '}
			}
			m.input, m.inputCursor, _ = editLine(m.input, m.inputCursor, key.String(), inserted)
		}
		return m, nil
	case "worktree-new":
		switch key.String() {
		case "enter":
			m.modal = ""
			return m.createWorktree()
		case "esc", "ctrl+c":
			m.modal = ""
		default:
			var inserted []rune
			if key.Type == tea.KeyRunes {
				inserted = key.Runes
			} else if key.Type == tea.KeySpace {
				inserted = []rune{' '}
			}
			m.input, m.inputCursor, _ = editLine(m.input, m.inputCursor, key.String(), inserted)
		}
		return m, nil
	case "branch-delete":
		switch key.String() {
		case "y":
			entry, force := m.branch.confirm, m.branch.confirmForce
			m.modal = ""
			return m.deleteBranch(entry, force)
		case "n", "esc", "q", "ctrl+c":
			m.modal = ""
		}
		return m, nil
	case "worktree-delete":
		switch key.String() {
		case "y":
			entry, force := m.worktrees.confirm, m.worktrees.confirmForce
			m.modal = ""
			return m.removeWorktree(entry, force)
		case "n", "esc", "q", "ctrl+c":
			m.modal = ""
		}
		return m, nil
	}
	if m.modal == "stash-confirm" {
		switch key.String() {
		case "y":
			return m.startStashAction(m.stash.operation, m.stash.target)
		case "n", "esc", "q", "ctrl+c":
			m.modal = ""
		}
		return m, nil
	}
	if m.modal == "discard" {
		switch key.String() {
		case "y":
			request, repo := m.discard, m.repo
			m.modal = ""
			m.busy = true
			m.generation++
			return m, func() tea.Msg {
				return completed{message: "Discarded changes", err: repo.Discard(request)}
			}
		case "n", "esc", "q", "ctrl+c":
			m.modal = ""
		}
		return m, nil
	}
	if m.modal == "files" || m.modal == "search" {
		runes := key.Runes
		if key.Type == tea.KeySpace {
			runes = []rune{' '}
		} else if key.Type != tea.KeyRunes {
			runes = nil
		}
		opened := false
		m = m.inDiff(func(view *model) {
			if view.modal == "files" {
				opened = view.handlePickerKey(key.String(), runes)
			} else {
				view.handleSearchKey(key.String(), runes)
			}
		})
		if opened {
			m.history.streamEndPending = false
			m.pickerReturn = true
			m.pickerBackToHistory = false
		} else if m.modal == "" && m.pickerBackToHistory {
			if m.screen == "commit" || m.screen == "base" {
				return m.returnToHistory()
			}
			if m.screen == "sequence" {
				return m.leaveSequence()
			}
			return m.openHistory()
		}
		return m, nil
	}
	if m.modal == "help" {
		entries := m.helpLines()
		pages := max(1, len(entries)-helpVisibleRows(m.height)+1)
		if m.handleListKey(&m.help, key.String(), listRunes(key), pages, helpVisibleRows(m.height), nil) {
			return m, nil
		}
		switch key.String() {
		case "esc", "?", "q":
			m.modal = ""
		}
		return m, nil
	}
	if m.modal == "error" {
		switch key.String() {
		case "j", "down":
			m.errorPos = min(m.errorPos+1, m.errorMaxPos())
		case "k", "up":
			m.errorPos = max(0, m.errorPos-1)
		case "esc", "enter", "q":
			m.modal = ""
		}
		return m, nil
	}
	if key.String() == "esc" || key.String() == "ctrl+c" {
		m.modal = ""
		return m, nil
	}
	if m.modal == "commit" {
		if key.String() == "ctrl+g" {
			return m.openCommitEditor()
		}
		switch key.String() {
		case "enter":
			if strings.TrimSpace(m.input) == "" {
				m.message = "Commit message cannot be empty"
				return m, nil
			}
			return m.submitCommit(m.input + m.inputBody)
		default:
			var inserted []rune
			if key.Type == tea.KeyRunes {
				text := strings.ReplaceAll(string(key.Runes), "\r\n", "\n")
				if key.Paste {
					if subject, body, ok := strings.Cut(text, "\n"); ok {
						text = subject
						m.inputBody += "\n" + body
					}
				}
				inserted = []rune(text)
			} else if key.Type == tea.KeySpace {
				inserted = []rune{' '}
			}
			var handled bool
			m.input, m.inputCursor, handled = editLine(m.input, m.inputCursor, key.String(), inserted)
			if handled {
				m.message = ""
			}
		}
		return m, nil
	}
	return m, nil
}

func (m model) runAction(args ...string) (tea.Model, tea.Cmd) {
	m.busy = true
	m.activeAction = args[0]
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		output, err := repo.Action(args...)
		return completed{message: output, action: args[0], err: err}
	}
}

func (m model) currentChanges() []git.Change {
	if m.staged {
		return m.snapshot.Staged
	}
	return m.snapshot.Unstaged
}

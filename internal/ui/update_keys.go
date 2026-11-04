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
	switch m.screen {
	case "":
		return m.handleChangesKey(msg)
	case "stashes":
		return m.handleStashKey(msg)
	case "branches":
		return m.handleBranchKey(msg)
	case "worktrees":
		return m.handleWorktreeKey(msg)
	case "hotspots":
		return m.handleHotspotKey(msg)
	case "sequence":
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

// diffScreen reports whether the current screen shows a diff.
func (m model) diffScreen() bool {
	return m.screen == "" || m.screen == "commit" || m.screen == "base" || m.screen == "sequence"
}

// handleSharedAction handles the toggles and actions that work the same way
// on every screen. It reports whether it handled p.
func (m model) handleSharedAction(p keyPress) (tea.Model, tea.Cmd, bool) {
	switch {
	case p.action == actFileHistory && m.diffScreen():
		next, cmd := m.openFileHistory()
		return next, cmd, true
	case p.action == actFetch:
		if !m.busy {
			next, cmd := m.startFetch()
			return next, cmd, true
		}
		return m, nil, true
	case p.action == actWhitespace && m.diffScreen():
		next, cmd := m.toggleWhitespace()
		return next, cmd, true
	case p.action == actShortPaths && m.diffScreen():
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
		return m, nil, true
	case p.action == actTheme:
		m.lightTheme = !m.lightTheme
		m.themeAuto = false
		m.flash = "Dark colors"
		if m.lightTheme {
			m.flash = "Light colors"
		}
		return m, nil, true
	case p.action == actHelp:
		m.modal = "help"
		m.help.reset()
		return m, nil, true
	}
	return m, nil, false
}

// foldKey names the fold direction the context and header toggles take.
func foldKey(a action) string {
	if a == actFold {
		return "h"
	}
	return "l"
}

func (m model) handleChangesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.press(msg, scopeChanges)
	if next, cmd, ok := m.handleSharedAction(p); ok {
		return next, cmd
	}
	if (p.action == actFold || p.action == actUnfold) && m.onDiffFile() {
		return m.changeContext(foldKey(p.action))
	}
	if m.handleDiffNavigation(p) || m.handleDiffTool(p) {
		return m, nil
	}
	switch p.action {
	case actEditor:
		return m.openEditor()
	case actCopy:
		if len(m.rows) > 0 {
			path := m.rows[m.pos].path
			return m, copyCmd(path, path)
		}
	case actCopyLocation:
		return m, m.copyDiffLocation("")
	case actBack, actQuit:
		if m.pickerReturn {
			m.restorePicker()
			return m, nil
		}
		if p.action == actQuit {
			return m, tea.Quit
		}
	case actInterrupt:
		return m, tea.Quit
	case actSwitchView:
		m.pickerReturn, m.pickerBackToHistory = false, false
		m.staged = !m.staged
		m.jumps = nil
		m.jumpIndex = 0
		m.pos = 0
		m.viewTop = 0
		m.rows = nil
		m.rebuild()
	case actWrap:
		m.wrap = !m.wrap
		m.horizontal = 0
		m.flash = wrapFlash(m.wrap)
		m.rebuild()
	case actRefresh:
		m.refreshCache = nil
		m.generation++
		return m.refresh()
	case actDiscard:
		return m.startDiscard()
	case actHistory:
		return m.openHistory()
	case actStash:
		if !m.busy {
			return m.startStashPush()
		}
	case actStageFile, actStageHunk:
		return m.stageSelection(p.action == actStageHunk)
	case actCommit:
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
	case actPush:
		if !m.busy {
			return m.startPush()
		}
	case actPull:
		if !m.busy {
			return m.startPull()
		}
	}
	return m, nil
}

// handleDiffTool handles the search and file picker keys shared by the
// working-tree and commit diff views.
func (m *model) handleDiffTool(p keyPress) bool {
	switch p.action {
	case actSearch:
		m.startSearch()
	case actFiles:
		m.openPicker()
	case actExpandDeleted:
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
	case "pull-issue":
		return m.handlePullIssueKey(key.String())
	case "push-rejected":
		return m.handlePushRejectedKey(key.String())
	case "push-upstream":
		switch key.String() {
		case "enter":
			return m.confirmPush(false)
		case "a":
			return m.confirmPush(true)
		case "esc", "q", "n", "ctrl+c":
			m.modal = ""
		default:
			if selected, ok := m.selectPushRemote(key.String()); ok {
				m = selected
				return m.confirmPush(false)
			}
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
				opened = view.handlePickerKey(key)
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
		if m.keyEditor.open {
			return m.handleKeyEditorKey(key)
		}
		entries := m.helpLines()
		pages := max(1, len(entries)-helpVisibleRows(m.height)+1)
		p := m.listPress(&m.help, key, scopeHelp)
		if m.handleListKey(&m.help, p, pages, helpVisibleRows(m.height), nil) {
			return m, nil
		}
		switch p.action {
		case actBack, actHelp, actQuit, actInterrupt:
			m.modal = ""
		case actEditKeys:
			m.openKeyEditor()
		}
		return m, nil
	}
	if m.modal == "error" {
		p := m.press(key, scopeShared)
		switch p.action {
		case actDown:
			m.errorPos = min(m.errorPos+1, m.errorMaxPos())
		case actUp:
			m.errorPos = max(0, m.errorPos-1)
		case actOpen:
			m.modal = ""
			if blockedByChanges(m.errorText) {
				return m.showChanges()
			}
		case actBack, actQuit:
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

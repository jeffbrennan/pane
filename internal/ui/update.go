package ui

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	view, ok := next.(model)
	if !ok {
		return next, cmd
	}
	if view.screen == "commit" && view.history.streamEndPending {
		return view, cmd
	}
	if load := view.loadVisibleSyntax(); load != nil {
		return view, tea.Batch(cmd, load)
	}
	view.warmCursorSyntax()
	if view.tickStopped && !view.readOnlyDiff() {
		view.tickStopped = false
		return view, tea.Batch(cmd, nextTick())
	}
	return view, cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case syntaxDemandLoaded:
		m.applySyntaxDemandLoaded(msg)
		return m, nil
	case tea.WindowSizeMsg:
		widthChanged := m.width != msg.Width
		oldContentWidth := m.diffContentWidth()
		m.width, m.height = msg.Width, msg.Height
		if widthChanged {
			m.resizeRequest++
			if max(len(m.rows), len(m.history.rows)) >= 50000 {
				request := m.resizeRequest
				m.resizeContentWidth = oldContentWidth
				cmd := tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return resizeRebuild{request: request} })
				m.revealHistorySelection()
				if m.modal == "files" {
					m = m.inDiff(func(view *model) { view.revealPickerSelection() })
				}
				m, background := m.loadHistoryBackground()
				return m, tea.Batch(cmd, background)
			}
			m.rebuild()
			m.rebuildCommit()
		} else {
			m = m.inDiff(func(view *model) {
				view.clamp()
				view.viewTop = view.viewportStart(max(1, view.height-2))
			})
		}
		m.revealHistorySelection()
		if m.modal == "files" {
			m = m.inDiff(func(view *model) { view.revealPickerSelection() })
		}
		return m.loadHistoryBackground()
	case resizeRebuild:
		if msg.request != m.resizeRequest {
			return m, nil
		}
		m.resizeRequest++
		m.reflowWidth(m.resizeContentWidth)
		return m, nil
	case tick:
		var cmd tea.Cmd
		m.ticks++
		interval := refreshTicks
		if m.blurred {
			interval = m.blurredRefreshInterval()
		} else if m.screen == "history" {
			interval = blurredRefreshTicks
		}
		if (m.screen == "" || m.screen == "base" || (m.screen == "history" && m.history.path == "")) && m.ticks%interval == 0 {
			m, cmd = m.refresh()
		}
		if m.readOnlyDiff() {
			// Read-only diffs do not refresh, so no periodic redraw is needed.
			m.tickStopped = true
			return m, tea.Batch(cmd, m.queryTheme())
		}
		return m, tea.Batch(cmd, nextTick(), m.queryThemeOnTick())
	case tea.BlurMsg:
		m.blurred = true
	case tea.FocusMsg:
		m.blurred, m.ticks = false, 0
		m.blurredInterval = blurredRefreshMin
		m, cmd := m.refresh()
		return m, tea.Batch(cmd, m.queryTheme())
	case terminalBackgroundChanged:
		if m.themeAuto {
			m.lightTheme = bool(msg)
		}
	case terminalThemeNotifications:
		if m.themeAuto {
			m.themeNotify = bool(msg)
		}
	case loadedUnchanged:
		m.loading = false
		if msg.generation != m.generation {
			return m.refresh()
		}
		m.backOffBlurredRefresh()
		m.refreshCache = msg.cache
		m.whitespaceRefreshing = false
		m.restoreStagingCursor()
		if m.screen == "base" {
			return m.refreshBase(false)
		}
	case loaded:
		m.loading = false
		if msg.generation != m.generation {
			return m.refresh()
		}
		if msg.err != nil {
			m.message = msg.err.Error()
			m.prefetch.close()
			m.prefetch = nil
			break
		}
		m.refreshCache = msg.cache
		m.whitespaceRefreshing = false
		headChanged := m.snapshot.Branch.Available && msg.snapshot.Branch.Available && m.snapshot.Branch.Head != msg.snapshot.Branch.Head
		changed := !m.snapshot.Equal(msg.snapshot)
		splice := changed && m.screen == "" && m.rows != nil
		if splice {
			m.previousChanges = append([]git.Change(nil), m.currentChanges()...)
		}
		m.snapshot = msg.snapshot
		if changed {
			m.resetBlurredRefresh()
			if splice {
				m.refreshRows()
				m.previousChanges = nil
			} else {
				m.rebuild()
			}
		} else {
			m.backOffBlurredRefresh()
		}
		m.restoreStagingCursor()
		if headChanged && m.screen == "commit" {
			m.history.stale = true
		}
		if headChanged && m.screen == "history" {
			if m.history.loading {
				m.history.stale = true
			} else {
				return m.reloadHistory()
			}
		}
		if m.startup {
			m.startup = false
			if len(msg.snapshot.Unstaged) == 0 && len(msg.snapshot.Staged) == 0 && m.screen == "" && m.modal == "" {
				m.startsInHistory = true
				return m.openHistory()
			}
		}
		m.prefetch.close()
		m.prefetch = nil
		if msg.cache != nil && msg.cache.Plain {
			// Highlight the files that started plain right away.
			return m.refresh()
		}
		if m.screen == "base" {
			return m.refreshBase(changed)
		}
	case baseLoaded:
		if m.screen != "base" || msg.request != m.base.request {
			break
		}
		m.base.loading = false
		if m.base.dirty {
			m.base.dirty = false
			return m.refreshBase(true)
		}
		if msg.err != nil {
			m.flash = "Base review: " + msg.err.Error()
			break
		}
		if msg.changed {
			m.base.hash, m.base.initialized = msg.hash, true
			m.history.changes, m.history.rows = nil, nil
			m.history.viewedFiles, m.history.totalFiles = 0, 0
			m.contextFiles = nil
			m.history.streamPaths, m.history.streamLoaded, m.history.streamGutter = nil, 0, 0
			m.history.streamError, m.history.streamEndPending = "", false
			m.history.streamActive = true
			m.picker.pendingPath = ""
			m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
			repo := m.startCommitLoad()
			return m, startBaseStream(repo, msg.hash, m.base.request, m.height)
		}
	case pushProbed:
		return m.finishPushProbe(msg)
	case completed:
		m.busy = false
		m.activeAction = ""
		if m.commitEditorPath != "" && msg.commitMessage != "" {
			if msg.err != nil {
				msg.err = fmt.Errorf("%w\nMessage retained at %s", msg.err, m.commitEditorPath)
			} else {
				os.Remove(m.commitEditorPath)
			}
			m.commitEditorPath = ""
		}
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
		} else if msg.commitMessage != "" {
			m.message = ""
			var refreshCmd tea.Cmd
			m, refreshCmd = m.refresh()
			historyModel, historyCmd := m.openHistory()
			return historyModel, tea.Batch(refreshCmd, historyCmd)
		} else if msg.action == "push" {
			m.message = ""
			m.modal = ""
			if msg.message != "" {
				m.flash = msg.message
			}
			var refreshCmd tea.Cmd
			m, refreshCmd = m.refresh()
			if m.screen == "history" {
				return m, refreshCmd
			}
			historyModel, historyCmd := m.openHistory()
			return historyModel, tea.Batch(refreshCmd, historyCmd)
		} else if msg.action == "pull" {
			m.message = ""
			var refreshCmd tea.Cmd
			m, refreshCmd = m.refresh()
			if m.screen == "history" {
				var historyCmd tea.Cmd
				m, historyCmd = m.reloadHistory()
				return m, tea.Batch(refreshCmd, historyCmd)
			}
			historyModel, historyCmd := m.openHistory()
			return historyModel, tea.Batch(refreshCmd, historyCmd)
		} else if msg.action == "fetch" {
			m.flash = msg.message
			return m.refresh()
		} else if msg.action == "stash-push" {
			m.flash = msg.message
			var refreshCmd tea.Cmd
			m, refreshCmd = m.refresh()
			if msg.message == "Nothing to stash" {
				return m, refreshCmd
			}
			historyModel, historyCmd := m.openHistory()
			return historyModel, tea.Batch(refreshCmd, historyCmd)
		} else if msg.action == "stash-pop" || msg.action == "stash-drop" {
			m.flash = msg.message
			var refreshCmd tea.Cmd
			m, refreshCmd = m.refresh()
			stashModel, stashCmd := m.openStashes()
			return stashModel, tea.Batch(refreshCmd, stashCmd)
		} else if msg.stageCursor != nil {
			m.flash = msg.message
			m.stageCursor = msg.stageCursor
		} else {
			m.message = msg.message
		}
		return m.refresh()
	case recentCommitsLoaded:
		if m.modal != "commit" || msg.request != m.recentCommitRequest {
			break
		}
		m.recentCommitLoading = false
		m.recentCommitError = msg.err != nil
		m.recentCommits = msg.commits
		if msg.err == nil && m.recentCommits == nil {
			m.recentCommits = []git.CommitInfo{}
		}
	case worktreeHeadsLoaded:
		if msg.request == m.worktreeHeadRequest {
			m.worktreeHeads = msg.heads
		}
	case historyLoaded:
		if (m.screen != "history" && m.screen != "commit") || msg.request != m.history.request || (msg.skip > 0 && (!m.history.fetching || msg.skip != len(m.history.commits))) {
			break
		}
		if msg.skip > 0 {
			m.history.fetching = false
			m.history.more = msg.err == nil
		} else {
			m.history.loading = false
			m.history.stashCount = msg.stashCount
		}
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		if msg.skip > 0 {
			m.history.commits = append(m.history.commits[:len(m.history.commits):len(m.history.commits)], msg.commits...)
		} else {
			for i, c := range msg.commits {
				if cached, ok := m.history.statsCache[c.Hash]; ok {
					msg.commits[i].Added = cached.Added
					msg.commits[i].Deleted = cached.Deleted
					msg.commits[i].Binary = cached.Binary
					msg.commits[i].StatsLoaded = true
				}
			}
			m.history.commits = msg.commits
			m.cacheHistoryStats()
		}
		m.history.more = len(msg.commits) == msg.limit
		m.history.list.selected = min(m.history.list.selected, max(0, len(m.filteredHistory())-1))
		if m.screen == "commit" {
			if direction := m.history.pendingStep; direction != 0 {
				m.history.pendingStep = 0
				return m.stepCommit(direction)
			}
			return m, nil
		}
		m.revealHistorySelection()
		if m.history.stale {
			return m.reloadHistory()
		}
		return m.loadHistoryBackground()
	case historyStatsLoaded:
		if m.screen != "history" || msg.request != m.history.request || !m.history.statsFetching {
			break
		}
		m.history.statsFetching = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		stats := make(map[string]git.CommitInfo, len(msg.commits))
		for _, c := range msg.commits {
			stats[c.Hash] = c
		}
		for _, hash := range msg.hashes {
			if _, ok := stats[hash]; !ok {
				m.errorText = "Git did not return statistics for commit " + hash
				m.errorPos = 0
				m.modal = "error"
				return m, nil
			}
		}
		for i, c := range m.history.commits {
			if s, ok := stats[c.Hash]; ok {
				m.history.commits[i].Added = s.Added
				m.history.commits[i].Deleted = s.Deleted
				m.history.commits[i].Binary = s.Binary
				m.history.commits[i].StatsLoaded = true
			}
		}
		m.cacheHistoryStats()
		return m.loadHistoryStats()
	case mergeCountsLoaded:
		if msg.request != m.history.request || !m.history.mergeCountsFetching {
			break
		}
		m.history.mergeCountsFetching = false
		if m.history.mergeCounts == nil {
			m.history.mergeCounts = make(map[string]int)
		}
		for _, hash := range msg.hashes {
			// Missing counts are stored as zero so they are not retried.
			m.history.mergeCounts[hash] = msg.counts[hash]
		}
		return m.loadMergeCounts()
	case stashesLoaded:
		if m.screen != "stashes" || msg.request != m.stash.request {
			break
		}
		m.stash.loading = false
		if msg.err != nil {
			m.errorText, m.errorPos, m.modal = msg.err.Error(), 0, "error"
			break
		}
		m.stash.entries = msg.entries
		m.stash.list.selected = min(m.stash.list.selected, max(0, len(msg.entries)-1))
	case hotspotSequenceLoaded:
		if m.screen != "hotspots" || msg.request != m.hotspots.request {
			break
		}
		if msg.err != nil {
			m.flash = "Hotspots: " + msg.err.Error()
			break
		}
		if len(msg.commits) == 0 {
			m.flash = "No commits in the window"
			break
		}
		next, err := newSequenceModel(m.repo, msg.commits)
		if err != nil {
			m.flash = err.Error()
			break
		}
		next.width, next.height, next.wrap = m.width, m.height, m.wrap
		next.viewed = m.viewed
		return next, next.Init()
	case hotspotsStarted:
		if m.screen != "hotspots" || msg.request != m.hotspots.request || msg.err != nil {
			if msg.plan.stream != nil {
				msg.plan.stream.Close()
			}
			if msg.err != nil {
				m.flash = "Hotspots: " + msg.err.Error()
				m.hotspots.loading = false
			}
			break
		}
		m.hotspots.head, m.hotspots.cachePath = msg.plan.head, msg.plan.cachePath
		m.hotspots.cutoff, m.hotspots.commits = msg.plan.cutoff, msg.plan.commits
		m.hotspots.files, m.hotspots.authors = msg.plan.files, msg.plan.authors
		m.hotspots.dataVersion++
		m.hotspots.stream = msg.plan.stream
		m.hotspots.streamLoaded = len(msg.plan.commits)
		m.hotspots.loading = true
		return m, m.waitHotspotPage(msg.plan.stream, msg.request, hotspotFirstPageSize)
	case hotspotPageLoaded:
		if m.screen != "hotspots" || msg.request != m.hotspots.request {
			break
		}
		if msg.err != nil {
			m.hotspots.stream = nil
			m.hotspots.loading = false
			m.flash = "Hotspots: " + msg.err.Error()
			break
		}
		m.hotspots.commits = append(m.hotspots.commits, msg.commits...)
		m.hotspots.streamLoaded = len(m.hotspots.commits)
		m.hotspots.files, m.hotspots.authors, m.hotspots.dirs, m.hotspots.hidden = aggregateHotspots(m.hotspots.commits, builtinPathMarks(m.hotspots.commits), m.hotspots.cutoff, time.Now())
		m.hotspots.dataVersion++
		m.hotspots.loading = false
		if msg.done {
			m.hotspots.stream = nil
			return m, m.finishHotspots(msg.request)
		}
		return m, m.waitHotspotPage(msg.stream, msg.request, hotspotPageSize)
	case hotspotsLoaded:
		if m.screen != "hotspots" || msg.request != m.hotspots.request {
			break
		}
		m.hotspots.loading = false
		m.hotspots.stream = nil
		if msg.err != nil {
			m.flash = "Hotspots: " + msg.err.Error()
			break
		}
		m.hotspots.files, m.hotspots.authors = msg.files, msg.authors
		m.hotspots.dirs, m.hotspots.hidden = msg.dirs, msg.hidden
		m.hotspots.dataVersion++
		if len(msg.items) > 0 {
			m.hotspots.items = &hotItemsCache{items: msg.items, filled: true, dataVersion: m.hotspots.dataVersion, builtAt: time.Now()}
		}
		m.hotspots.list.selected, m.hotspots.list.offset = 0, 0
	case blameLoaded:
		return m.applyBlame(msg)
	case branchesLoaded:
		if m.screen != "branches" || msg.request != m.branch.request {
			break
		}
		m.branch.loading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		m.branch.entries = msg.entries
		m.branch.list.selected = min(m.branch.list.selected, max(0, len(msg.entries)-1))
	case branchActionDone:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		m.flash = msg.message
		if msg.stay {
			return m.reloadBranches()
		}
		next, historyCmd := m.openHistory()
		refreshed, refreshCmd := next.(model).refresh()
		return refreshed, tea.Batch(historyCmd, refreshCmd)
	case worktreesLoaded:
		if m.screen != "worktrees" || msg.request != m.worktrees.request {
			break
		}
		m.worktrees.loading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		m.worktrees.entries = msg.entries
		m.worktrees.status = make(map[string]worktreeStatus, len(msg.entries))
		m.worktrees.list.selected = min(m.worktrees.list.selected, max(0, len(msg.entries)-1))
		cmds := make([]tea.Cmd, 0, len(msg.entries))
		for _, entry := range msg.entries {
			cmds = append(cmds, loadWorktreeStatus(m.repo, entry.Path, m.worktrees.request))
		}
		return m, tea.Batch(cmds...)
	case worktreeStatusLoaded:
		if m.screen != "worktrees" || msg.request != m.worktrees.request || msg.err != nil {
			break
		}
		m.worktrees.status[msg.path] = msg.status
	case worktreeActionDone:
		if msg.err != nil {
			m.errorText = msg.err.Error() + "\n\nPress D on the worktree to force removal."
			m.errorPos = 0
			m.modal = "error"
			break
		}
		m.flash = msg.message
		return m.reloadWorktrees()
	case stashDiffLoaded:
		if m.screen != "commit" || !m.stash.viewing || msg.request != m.history.request {
			break
		}
		m.history.loading = false
		if msg.err != nil {
			m.screen = "stashes"
			m.errorText, m.errorPos, m.modal = msg.err.Error(), 0, "error"
			break
		}
		m.history.changes = msg.changes
		if msg.meta.Hash != "" {
			m.history.meta = msg.meta
		}
		m.rebuildCommit()
	case commitLoadingDelay:
		if m.screen == "commit" && m.history.loading && msg.request == m.history.request {
			m.history.commitLoadingVisible = true
		}
	case commitStreamLoaded:
		request := m.history.request
		if m.screen == "base" {
			request = m.base.request
		}
		if (m.screen != "commit" && m.screen != "base") || !m.history.streamActive || msg.request != request {
			break
		}
		chunk := msg.chunk
		if chunk.Meta.Hash != "" {
			m.history.meta = chunk.Meta
		}
		m.history.streamPaths, m.history.streamLoaded = chunk.Paths, chunk.Loaded
		m.history.commitLoadingVisible = false
		m.appendCommitChanges(chunk.Changes, chunk.Done)
		if m.picker.pendingPath != "" {
			for i, r := range m.history.rows {
				if r.kind == 'f' && (r.path == m.picker.pendingPath || r.oldPath == m.picker.pendingPath) {
					m.history.pos = i
					m.history.viewTop = i
					m.picker.pendingPath = ""
					break
				}
			}
		}
		if m.history.pendingPosition != nil {
			m.restoreCommitPosition(chunk.Done)
		}
		if m.history.streamEndPending && len(m.history.rows) > 0 {
			m.history.pos = len(m.history.rows) - 1
			m.history.viewTop = max(0, m.history.pos-max(1, m.height-2)+1)
		}
		if m.modal == "search" && m.search.draft != "" {
			m = m.inDiff(func(view *model) { view.updateSearchDraft() })
		}
		if chunk.Done {
			m.history.streamActive, m.history.loading, m.history.streamEndPending = false, false, false
			m.picker.cache = nil
			if len(m.history.rows) > 500000 {
				// The streamed build peaks above what the rows retain; return
				// that capacity to the OS once the diff is complete.
				debug.FreeOSMemory()
			}
			if chunk.Err != nil {
				m.history.streamError = "Commit load: " + chunk.Err.Error()
			}
			if m.picker.pendingPath != "" {
				m.flash = "File not found: " + m.picker.pendingPath
				m.picker.pendingPath = ""
			}
			break
		}
		return m, waitCommitStream(msg.stream, msg.request)
	case sequenceChunkLoaded:
		if m.screen != "sequence" || msg.request != m.sequence.request || msg.index < 0 || msg.index >= len(m.sequence.states) {
			break
		}
		index := msg.index
		chunk := msg.chunk
		if chunk.Meta.Hash != "" {
			m.sequence.commits[index] = chunk.Meta
		}
		if len(chunk.Changes) > 0 {
			m.sequence.changes[index] = append(m.sequence.changes[index], chunk.Changes...)
			if index < len(m.sequence.rowCache) {
				m.sequence.rowCache[index] = nil
			}
		}
		if chunk.Done {
			errText := ""
			if chunk.Err != nil {
				errText = chunk.Err.Error()
				if strings.Contains(errText, "bad revision") || strings.Contains(errText, "unknown revision") || strings.Contains(errText, "not a valid object") {
					errText = fmt.Sprintf("invalid commit %q: %s", m.sequence.commits[index].Hash, errText)
				}
			}
			if chunk.Err != nil && len(m.sequence.changes[index]) == 0 {
				m.sequence.states[index] = sequenceFailed
				m.sequence.errors[index] = errText
			} else {
				m.sequence.states[index] = sequenceReady
				if errText != "" {
					m.sequence.errors[index] = errText
				}
			}
			m.rebuildSequence()
			return m, m.nextSequenceLoad()
		}
		if m.sequence.states[index] != sequenceReady {
			m.sequence.states[index] = sequenceReady
			m.sequence.loading = true
		}
		m.rebuildSequence()
		return m, waitSequenceChunk(msg.stream, index, msg.request)
	case sequenceLoaded:
		if m.screen != "sequence" || msg.request != m.sequence.request || msg.index < 0 || msg.index >= len(m.sequence.states) || m.sequence.states[msg.index] != sequenceLoading {
			break
		}
		if msg.err != nil {
			m.sequence.states[msg.index] = sequenceFailed
			m.sequence.errors[msg.index] = msg.err.Error()
		} else {
			m.sequence.states[msg.index] = sequenceReady
			m.sequence.commits[msg.index] = msg.commit
			m.sequence.changes[msg.index] = msg.changes
		}
		m.rebuildSequence()
		if query := m.activeSearch(); query != "" && (len(m.rows) == 0 || !rowMatches(m.rows[m.pos], query)) {
			if m.modal == "search" {
				m.updateSearchDraft()
			} else if len(m.rows) > 0 {
				if index, _ := m.findMatch(query, m.pos-m.searchDirection(), m.searchDirection()); index >= 0 {
					m.showMatch(index)
					m.flash = ""
				}
			}
		}
		cmd := m.nextSequenceLoad()
		return m, cmd
	case flashed:
		m.flash = msg.text
	case editorClosed:
		if msg.err != nil {
			m.flash = "Editor: " + msg.err.Error()
		}
		return m.refresh()
	case commitEditorClosed:
		return m.finishCommitEditor(msg)
	case contextLoaded:
		return m.applyContext(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

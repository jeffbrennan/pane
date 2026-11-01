package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jeffbrennan/pane/internal/syntax"
)

type historyState struct {
	commitContext                                       context.Context
	commitCancel                                        context.CancelFunc
	streamPaths                                         []string
	streamError                                         string
	streamLoaded, streamGutter                          int
	streamActive, streamEndPending                      bool
	stream                                              *HistoryStream
	commits                                             []CommitInfo
	statsCache                                          map[string]CommitInfo
	changes                                             []Change
	rows                                                []row
	gutterWidth                                         int
	selected, offset, pos, horizontal, viewTop, request int
	stashCount                                          int
	filter, draft                                       string
	path                                                string
	since                                               string
	draftCursor                                         int
	searching, loading, stale, more, fetching           bool
	commitLoadingVisible                                bool
	statsFetching                                       bool
	directCommit                                        bool
	current                                             CommitInfo
	meta                                                CommitInfo
	moved                                               *movedIndex
	headerCollapsed                                     bool
	positions                                           map[string]commitPosition
	pendingPosition                                     *commitPosition
	pendingStep                                         int
	visualActive                                        bool
	visualAnchor                                        int
	expandedDeleted                                     map[string]bool
	jumps                                               []jumpLocation
	jumpIndex                                           int
}

func (m *model) startCommitLoad() Repo {
	if m.history.commitCancel != nil {
		m.history.commitCancel()
	}
	m.history.commitContext, m.history.commitCancel = context.WithCancel(context.Background())
	return m.commitRepository()
}

func (m model) commitRepository() Repo {
	repo := m.repo
	repo.Context = m.history.commitContext
	return repo
}

func (m *model) cancelCommitLoad() {
	if m.history.commitCancel != nil {
		m.history.commitCancel()
		m.history.commitCancel = nil
		m.history.commitContext = nil
	}
	m.history.streamActive = false
}

func (m *model) appendCommitChanges(changes []Change, done bool) {
	if len(changes) == 0 {
		return
	}
	seeded := *m
	if m.screen == "base" {
		seeded.contextCommit, seeded.contextBase = m.base.hash, true
		seeded.viewIdentity = "base:" + m.base.hash + ":" + m.base.revision
	} else {
		seeded.contextCommit, seeded.contextBase = m.history.current.Hash, false
		seeded.viewIdentity = "commit:" + m.history.current.Hash
	}
	seeded.staged = false
	seeded.collapseGenerated(changes)
	seeded.seedViewedCollapse(changes)
	m.collapsedFiles, m.generatedHandled = seeded.collapsedFiles, seeded.generatedHandled
	if m.screen == "commit" {
		if m.history.moved == nil {
			m.history.moved = newMovedIndex()
		}
		m.history.moved.addChanges(changes)
	}
	width := m.history.streamGutter
	if width == 0 && !done {
		width = 4
		// On very large commits a late wide gutter can force a second build
		// of hundreds of thousands of wrapped rows.
		if len(m.history.streamPaths) >= 5000 {
			width = 5
		}
	}
	for _, c := range changes {
		width = max(width, lineNumberWidth(strings.Split(c.Diff, "\n")))
	}
	previousWidth := m.history.streamGutter
	m.history.streamGutter = width
	m.history.changes = append(m.history.changes, changes...)
	if len(m.history.rows) == 0 || width > previousWidth {
		m.rebuildCommit()
		return
	}
	view := m.commitModel()
	changes = view.applySyntaxDemand(contextKey{commit: view.contextCommit, staged: view.staged, base: view.contextBase}, changes)
	if len(view.contextFiles) == 0 && len(view.collapsedFiles) == 0 {
		parts := make([][]row, len(changes))
		parallelEach(len(changes), func(i int) {
			parts[i] = changeRows(changes[i], true, view.diffContentWidth(), view.wrap, view.expandedDeleted[changes[i].Path], width, view.shortPaths)
		})
		total := 0
		for _, part := range parts {
			total += len(part)
		}
		if cap(m.history.rows) >= 100000 && len(m.history.rows)+total > cap(m.history.rows) {
			runtime.GC()
		}
		rows := m.history.rows
		required := len(rows) + total
		if required > cap(rows) && len(m.history.streamPaths) >= 5000 && m.history.streamLoaded >= 500 {
			estimate := int(min(int64(2000000), int64(required)*int64(len(m.history.streamPaths))/int64(m.history.streamLoaded)))
			estimate = min(estimate, required*4)
			if estimate > required+required/4 {
				grown := make([]row, len(rows), estimate)
				copy(grown, rows)
				rows = grown
			}
		}
		rows = slices.Grow(rows, total)
		header := rows[len(rows)-1].headerRow()
		if rows[len(rows)-1].kind == 'f' {
			header = len(rows)
		}
		for _, part := range parts {
			for i := range part {
				if part[i].kind == 'f' {
					header = len(rows) + i + 1
				} else {
					part[i].setHeaderRow(header)
				}
			}
			rows = append(rows, part...)
		}
		m.history.rows = rows
		return
	}
	part, _ := buildRowsDisplay(changes, view.diffContentWidth(), view.wrap, view.expandedDeleted, width, view.shortPaths)
	view.applyViewed(part, changes)
	part = view.insertNoteRows(part)
	part = view.localContextRows(part, changes)
	part = view.foldContextFiles(part)
	if len(part) == 0 {
		return
	}
	rows := m.history.rows
	last := rows[len(rows)-1]
	header := last.headerRow()
	if last.kind == 'f' {
		header = len(rows)
	}
	separator := row{path: changes[0].Path, lineIndex: -2, kind: 's'}
	separator.setHeaderRow(header)
	rows = append(rows, separator)
	offset := len(rows)
	for i := range part {
		if header := part[i].headerRow(); header > 0 {
			part[i].setHeaderRow(header + offset)
		}
	}
	m.history.rows = append(rows, part...)
}

type historyLoaded struct {
	commits     []CommitInfo
	err         error
	request     int
	skip, limit int
	stashCount  int
}
type commitLoaded struct {
	changes []Change
	err     error
	request int
}

type commitSyntaxLoaded struct {
	changes []Change
	request int
}

type commitLoadingDelay struct{ request int }

type commitStreamLoaded struct {
	chunk   CommitChunk
	stream  <-chan CommitChunk
	request int
}

func startCommitStream(repo Repo, hash string, request, height int) tea.Cmd {
	return startStream(repo, hash, request, height, repo.StreamCommit)
}

func startBaseStream(repo Repo, hash string, request, height int) tea.Cmd {
	return startStream(repo, hash, request, height, repo.StreamBase)
}

func startStream(repo Repo, hash string, request, height int, open func(context.Context, string, int) <-chan CommitChunk) tea.Cmd {
	return func() tea.Msg {
		ctx := repo.Context
		if ctx == nil {
			ctx = context.Background()
		}
		stream := open(ctx, hash, max(height, startupSyntaxLines))
		return waitCommitStream(stream, request)()
	}
}

func waitCommitStream(stream <-chan CommitChunk, request int) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-stream
		if !ok {
			chunk = CommitChunk{Done: true, Err: fmt.Errorf("commit stream ended unexpectedly")}
		}
		return commitStreamLoaded{chunk: chunk, stream: stream, request: request}
	}
}

func commitLoadingTimer(request int) tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return commitLoadingDelay{request: request}
	})
}

// loadCommitSyntax highlights the changes that commitChangesFirstScreen left
// plain.
func loadCommitSyntax(repo Repo, hash string, request int, changes []Change) tea.Cmd {
	changes = append([]Change(nil), changes...)
	var indexes []int
	var plain []Change
	for i, c := range changes {
		if c.SyntaxDiff == "" && !c.SyntaxSkipped && syntax.Lexable(c.Path) {
			indexes = append(indexes, i)
			plain = append(plain, c)
		}
	}
	if len(plain) == 0 {
		return nil
	}
	return func() tea.Msg {
		repo.LoadSyntaxDiffs(plain, contextKey{commit: hash}.gitContext())
		warmSyntax(plain)
		for i, c := range plain {
			changes[indexes[i]] = c
		}
		return commitSyntaxLoaded{changes: changes, request: request}
	}
}

type historyStatsLoaded struct {
	commits []CommitInfo
	hashes  []string
	err     error
	request int
}

const (
	historyStatsLookahead = 5
	historyStatsCacheSize = 20
)

func (m *model) cacheHistoryStats() {
	cache := make(map[string]CommitInfo)
	for _, c := range m.history.commits[:min(len(m.history.commits), historyStatsCacheSize)] {
		if c.StatsLoaded {
			cache[c.Hash] = c
		}
	}
	m.history.statsCache = cache
}

func (m model) openHistory() (tea.Model, tea.Cmd) {
	m.screen = "history"
	m.history.path = ""
	m.fileHistoryReturn = nil
	m.pickerReturn, m.pickerBackToHistory = false, false
	m.history.commits = nil
	m.history.filter = ""
	m.history.searching = false
	m.history.selected, m.history.offset = 0, 0
	m.pending = ""
	m, cmd := m.reloadHistory()
	return m, tea.Batch(cmd, m.loadWorktreeHeads())
}

func (m model) reloadHistory() (model, tea.Cmd) {
	m.history.stream.Close()
	m.history.loading = true
	m.history.stale = false
	m.history.fetching = false
	m.history.statsFetching = false
	m.history.request++
	if prefetch := m.prefetch; prefetch != nil && m.history.path == "" {
		m.prefetch = nil
		m.history.stream = prefetch.stream
		return m, prefetch.loadHistory(m.history.request)
	}
	m.history.stream = NewHistoryStream(m.repo, m.history.path, m.history.since)
	return m, m.fetchHistory(0, max(historyPage, len(m.history.commits)))
}

type worktreeHeadsLoaded struct {
	heads   map[string]string
	request int
}

// loadWorktreeHeads decorates history commits that are checked out elsewhere.
func (m model) loadWorktreeHeads() tea.Cmd {
	m.worktreeHeadRequest++
	repo, request := m.repo, m.worktreeHeadRequest
	return func() tea.Msg {
		entries, err := repo.Worktrees()
		if err != nil {
			return worktreeHeadsLoaded{request: request}
		}
		heads := make(map[string]string, len(entries))
		root := repo.Root
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		for _, entry := range entries {
			if entry.Head == "" || entry.Path == root {
				continue
			}
			name := entry.Branch
			if name == "" {
				name = shortHash(entry.Head)
			}
			heads[entry.Head] = name
		}
		return worktreeHeadsLoaded{heads: heads, request: request}
	}
}

func (m model) loadHistoryStats() (model, tea.Cmd) {
	if m.screen != "history" || m.history.loading || m.history.statsFetching {
		return m, nil
	}
	matches := m.filteredHistory()
	start, visible := m.historyViewport(len(matches))
	available := max(0, m.height-m.historyHeaderHeight()-1)
	if available >= 4 && (available-1)%3 != 0 {
		visible++
	}
	var hashes []string
	for _, c := range matches[start:min(len(matches), start+visible+historyStatsLookahead)] {
		if !c.StatsLoaded && c.Hash != "" {
			hashes = append(hashes, c.Hash)
		}
	}
	if len(hashes) == 0 {
		return m, nil
	}
	m.history.statsFetching = true
	repo, request := m.repo, m.history.request
	return m, func() tea.Msg {
		commits, err := repo.HistoryStats(hashes)
		return historyStatsLoaded{commits: commits, hashes: hashes, err: err, request: request}
	}
}

func (m model) loadHistoryBackground() (model, tea.Cmd) {
	var pages, stats tea.Cmd
	m, pages = m.loadMoreHistory()
	m, stats = m.loadHistoryStats()
	if pages == nil {
		return m, stats
	}
	if stats == nil {
		return m, pages
	}
	return m, tea.Batch(pages, stats)
}

func (m model) fetchHistory(skip, limit int) tea.Cmd {
	repo, request, path := m.repo, m.history.request, m.history.path
	requestStream := m.history.stream
	return func() tea.Msg {
		stream := requestStream
		if stream == nil {
			stream = NewHistoryStream(repo, path)
			defer stream.Close()
			if skip > 0 {
				if _, err := stream.NextPage(skip); err != nil {
					return historyLoaded{err: err, request: request, skip: skip, limit: limit}
				}
			}
		}
		commits, count, err := historyPageWithStashes(repo, stream, limit, skip == 0)
		return historyLoaded{commits: commits, err: err, request: request, skip: skip, limit: limit, stashCount: count}
	}
}

// historyPageWithStashes reads the next page from stream with stored line
// counts filled in, counting stashes alongside when stashes is set.
func historyPageWithStashes(repo Repo, stream *HistoryStream, limit int, stashes bool) ([]CommitInfo, int, error) {
	count := make(chan int, 1)
	if stashes {
		go func() {
			entries, err := repo.Stashes()
			if err != nil {
				count <- 0
				return
			}
			count <- len(entries)
		}()
	} else {
		count <- 0
	}
	commits, err := stream.NextPage(limit)
	repo.Stats.Fill(commits)
	return commits, <-count, err
}

// loadMoreHistory fetches the next page when the selection nears the end of
// the loaded commits, or continuously while a search is active.
func (m model) loadMoreHistory() (model, tea.Cmd) {
	if m.screen != "history" || m.history.loading || m.history.fetching || !m.history.more {
		return m, nil
	}
	if m.historyQuery() == "" && m.history.selected < len(m.history.commits)-historyPage/4 {
		return m, nil
	}
	m.history.fetching = true
	limit := historyPage
	if m.historyQuery() != "" {
		limit = 5 * historyPage
	}
	return m, m.fetchHistory(len(m.history.commits), limit)
}

func (m model) historyQuery() string {
	if m.history.searching {
		return m.history.draft
	}
	return m.history.filter
}

func (m model) filteredHistory() []CommitInfo {
	query := strings.ToLower(m.historyQuery())
	if query == "" {
		return m.history.commits
	}
	var matches []CommitInfo
	for _, c := range m.history.commits {
		search := c.Search
		if search == "" {
			search = strings.ToLower(c.Hash + "\n" + c.Author + "\n" + c.Email + "\n" + c.Message + "\n" + c.Subject)
		}
		if strings.Contains(search, query) {
			matches = append(matches, c)
		}
	}
	return matches
}

func (m model) handleHistoryKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == "commit" || m.screen == "base" {
		return m.handleCommitKey(key)
	}
	if m.history.searching {
		switch key.String() {
		case "down":
			m.history.selected = min(m.history.selected+1, max(0, len(m.filteredHistory())-1))
			return m, nil
		case "up":
			m.history.selected = max(0, m.history.selected-1)
			return m, nil
		case "esc":
			m.history.searching = false
		case "enter":
			if len(m.filteredHistory()) == 0 || m.history.loading {
				return m, nil
			}
			m.history.filter = m.history.draft
			m.history.searching = false
			return m.handleHistoryKey(key)
		default:
			var runes []rune
			if key.Type == tea.KeyRunes {
				runes = key.Runes
			} else if key.Type == tea.KeySpace {
				runes = []rune{' '}
			}
			before := m.history.draft
			m.history.draft, m.history.draftCursor, _ = editLine(before, m.history.draftCursor, key.String(), runes)
			if before == m.history.draft {
				return m, nil
			}
		}
		m.history.selected, m.history.offset = 0, 0
		return m, nil
	}
	matches := m.filteredHistory()
	keyString := key.String()
	if len(keyString) == 1 && keyString[0] >= '0' && keyString[0] <= '9' && m.pending == "" {
		m.count = min(1000000, m.count*10+int(keyString[0]-'0'))
		return m, nil
	}
	hadCount := m.count > 0
	count := max(1, m.count)
	if m.pending != "" {
		pending := m.pending
		m.pending = ""
		m.count = 0
		if pending == "g" && key.String() == "g" {
			m.history.selected = 0
			if hadCount {
				m.history.selected = min(count-1, max(0, len(matches)-1))
			}
			return m, nil
		}
		if pending == "g" && (keyString == "H" || keyString == "M" || keyString == "L") {
			start, visible := m.historyViewport(len(matches))
			end := min(max(0, len(matches)-1), start+visible-1)
			m.history.selected = start
			if keyString == "M" {
				m.history.selected = (start + end) / 2
			}
			if keyString == "L" {
				m.history.selected = end
			}
			return m, nil
		}
		if key.String() == "esc" {
			return m, nil
		}
	}
	if keyString != "g" {
		m.count = 0
	}
	page := max(1, (m.historyAvailable()-1)/3)
	switch key.String() {
	case "esc", "q":
		if m.history.visualActive {
			m.history.visualActive = false
			return m, nil
		}
		if m.history.path != "" {
			return m.leaveFileHistory()
		}
		m.screen = ""
		m.history.stream.Close()
		m.history.stream = nil
		m.history.request++
		return m.refresh()
	case "ctrl+c":
		m.closeHistoryStreams()
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "p":
		if !m.busy {
			return m.runAction("push")
		}
	case "P":
		if !m.busy {
			return m.runAction("pull")
		}
	case "S":
		if m.history.path != "" {
			m.flash = "Return to history to open stashes"
			break
		}
		if !m.busy {
			return m.openStashes()
		}
	case "r":
		var refreshCmd, historyCmd tea.Cmd
		m, refreshCmd = m.refresh()
		m, historyCmd = m.reloadHistory()
		return m, tea.Batch(refreshCmd, historyCmd)
	case "R":
		if len(matches) == 0 || m.history.loading {
			break
		}
		return m.openBaseFrom(matches[min(m.history.selected, len(matches)-1)].Hash)
	case "b":
		if m.history.path != "" {
			m.flash = "Return to history to browse branches"
			break
		}
		return m.openBranches()
	case "X":
		return m.openHotspots(hotSinceDefault(), "")
	case "n":
		if len(matches) == 0 || m.history.loading {
			break
		}
		return m.promptNewBranch(matches[min(m.history.selected, len(matches)-1)].Hash)
	case "/":
		m.history.searching = true
		m.history.draft = m.history.filter
		m.history.draftCursor = len([]rune(m.history.draft))
	case "y":
		if len(matches) > 0 {
			hash := matches[min(m.history.selected, len(matches)-1)].Hash
			return m, copyCmd(hash, shortHash(hash))
		}
	case "Y":
		if len(matches) > 0 {
			commit := matches[min(m.history.selected, len(matches)-1)]
			if commit.FilePath != "" {
				return m, copyLocationCmd(m.repo, commit.Hash, commit.FilePath, 0)
			}
			return m, copyCmd(commit.Hash, shortHash(commit.Hash))
		}
	case "u":
		if m.history.path != "" {
			m.flash = "Return to history to edit the latest commit"
			break
		}
		if m.busy || m.history.loading || len(matches) == 0 || len(m.history.commits) == 0 {
			break
		}
		selected := matches[min(m.history.selected, len(matches)-1)]
		if selected.Hash != m.history.commits[0].Hash {
			m.flash = "Only the latest commit message can be edited"
			break
		}
		m.modal, m.amendHash, m.message, m.flash = "commit", selected.Hash, "", ""
		m.input, m.inputBody, _ = strings.Cut(strings.TrimRight(selected.Message, "\n"), "\n")
		if m.inputBody != "" {
			m.inputBody = "\n" + m.inputBody
		}
		m.inputCursor = len([]rune(m.input))
		m.inputNormal, m.inputPending = false, ""
		m.recentCommits, m.recentCommitLoading, m.recentCommitError = nil, false, false
		m.recentCommitRequest++
		return m, nil
	case "g":
		m.pending = "g"
	case "G", "end":
		m.history.selected = max(0, len(matches)-1)
		if hadCount && keyString == "G" {
			m.history.selected = min(count-1, max(0, len(matches)-1))
		}
	case "home":
		m.history.selected = 0
	case "ctrl+d":
		m.history.selected = min(m.history.selected+max(1, page/2)*count, max(0, len(matches)-1))
	case "ctrl+u":
		m.history.selected = max(0, m.history.selected-max(1, page/2)*count)
	case "ctrl+f", "pgdown":
		m.history.selected = min(m.history.selected+page*count, max(0, len(matches)-1))
	case "ctrl+b", "pgup":
		m.history.selected = max(0, m.history.selected-page*count)
	case "j", "down":
		m.history.selected = min(m.history.selected+count, max(0, len(matches)-1))
	case "k", "up":
		m.history.selected = max(0, m.history.selected-count)
	case "}":
		m.history.selected = min(m.history.selected+5*count, max(0, len(matches)-1))
	case "{":
		m.history.selected = max(0, m.history.selected-5*count)
	case "enter", " ":
		if len(matches) == 0 || m.history.loading {
			break
		}
		if key.String() == "enter" && m.history.visualActive {
			lo, hi := m.history.visualAnchor, min(m.history.selected, len(matches)-1)
			if lo > hi {
				lo, hi = hi, lo
			}
			lo, hi = max(0, lo), min(hi, len(matches)-1)
			m.history.visualActive = false
			next, err := newSequenceModel(m.repo, append([]CommitInfo(nil), matches[lo:hi+1]...))
			if err != nil {
				m.flash = err.Error()
				break
			}
			next.width, next.height, next.wrap = m.width, m.height, m.wrap
			next.viewed = m.viewed
			return next, next.Init()
		}
		return m.openCommit(matches[min(m.history.selected, len(matches)-1)])
	case "V":
		m.history.visualActive = !m.history.visualActive
		m.history.visualAnchor = m.history.selected

	}
	return m, nil
}

// openCommit replaces the current view with a commit's read-only diff.
func (m model) openCommit(commit CommitInfo) (tea.Model, tea.Cmd) {
	m.saveCommitPosition()
	m.history.current = commit
	if m.history.fetching {
		m.history.stream.Close()
		m.history.stale = true
	}
	m.history.request++
	repo := m.startCommitLoad()
	m.history.loading = true
	m.history.commitLoadingVisible = false
	m.history.fetching = false
	m.history.statsFetching = false
	m.history.changes, m.history.rows = nil, nil
	m.history.streamActive = true
	m.history.streamPaths, m.history.streamLoaded, m.history.streamGutter = nil, 0, 0
	m.history.streamError = ""
	m.history.streamEndPending = false
	m.history.meta = CommitInfo{}
	m.history.moved = nil
	if position, ok := m.history.positions[commit.Hash]; ok && commit.FilePath == "" {
		m.history.pendingPosition = &position
	} else {
		m.history.pendingPosition = nil
	}
	m.picker.pendingPath = commit.FilePath
	m.history.expandedDeleted = nil
	m.history.jumps = nil
	m.history.jumpIndex = 0
	m.pickerReturn, m.pickerBackToHistory = false, false
	m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
	m.screen = "commit"
	hash, request := commit.Hash, m.history.request
	return m, tea.Batch(startCommitStream(repo, hash, request, m.height), commitLoadingTimer(request))
}

// inDiff applies f to the diff currently on screen: the working tree, or the
// commit being viewed, whose diff state lives in m.history.
func (m model) inDiff(f func(*model)) model {
	if m.screen != "commit" && m.screen != "base" {
		f(&m)
		return m
	}
	view := m.commitModel()
	f(&view)
	m.history.rows, m.history.pos, m.history.horizontal, m.history.viewTop = view.rows, view.pos, view.horizontal, view.viewTop
	m.history.gutterWidth = view.gutterWidth
	m.collapsedFiles, m.generatedHandled = view.collapsedFiles, view.generatedHandled
	m.history.expandedDeleted = view.expandedDeleted
	m.pending, m.count, m.modal, m.picker, m.search, m.flash = view.pending, view.count, view.modal, view.picker, view.search, view.flash
	m.history.jumps, m.history.jumpIndex = view.jumps, view.jumpIndex
	return m
}

func (m model) handleCommitKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyString := key.String()
	pending := m.pending
	followEnd := m.history.streamActive && m.pending == "" && m.count == 0 && (keyString == "G" || keyString == "end")
	if m.history.streamActive && (keyString == "J" || (pending == "]" && keyString == "c")) {
		followEnd = m.streamJumpBeyondEnd(keyString == "J", max(1, m.count))
	}
	if keyString != "]" && !(pending == "]" && keyString == "c") {
		m.history.streamEndPending = false
	}
	if m.pending == "g" && key.String() == "b" {
		m.pending = ""
		return m.startBlame()
	}
	if m.pending == "" && (key.String() == "h" || key.String() == "l") && m.commitModel().onCommitHeader() {
		m.history.headerCollapsed = key.String() == "h"
		m.rebuildCommit()
		return m, nil
	}
	if m.pending == "" && (key.String() == "h" || key.String() == "l") && m.commitModel().onDiffFile() {
		return m.changeContext(key.String())
	}
	handled := false
	m = m.inDiff(func(view *model) {
		handled = view.handleDiffNavigation(key.String()) || view.handleDiffTool(key.String())
	})
	if handled {
		if followEnd {
			m.history.streamEndPending = true
			if len(m.history.rows) > 0 {
				m.history.pos = len(m.history.rows) - 1
				m.history.viewTop = max(0, m.history.pos-max(1, m.height-2)+1)
			}
		}
		return m, nil
	}
	view := m.commitModel()
	switch key.String() {
	case ")", "(":
		direction := 1
		if key.String() == "(" {
			direction = -1
		}
		return m.stepCommit(direction)
	case "v":
		return m.toggleViewedCommit()
	case "M":
		if cmd := m.copyNotes(); cmd != nil {
			return m, cmd
		}
		m.flash = "No notes in this view"
		return m, nil
	case "o":
		opened, cmd := view.openEditor()
		m.flash = opened.flash
		return m, cmd
	case "y":
		if m.screen == "base" {
			return m, copyCmd(m.base.hash, shortHash(m.base.hash))
		}
		return m, copyCmd(m.history.current.Hash, shortHash(m.history.current.Hash))
	case "Y":
		hash := m.history.current.Hash
		if m.screen == "base" || m.stash.viewing {
			hash = ""
		}
		return m, view.copyDiffLocation(hash)
	}
	if key.String() == "esc" || key.String() == "q" {
		if m.pickerReturn {
			m.restorePicker()
			return m, nil
		}
		return m.returnToHistory()
	}
	if key.String() == "ctrl+c" {
		m.closeHistoryStreams()
		return m, tea.Quit
	}
	if key.String() == "?" {
		m.modal = "help"
		m.helpPos = 0
		return m, nil
	}
	switch key.String() {
	case "w":
		view.wrap = !view.wrap
		view.horizontal = 0
		view.rebuild()
		m.flash = wrapFlash(view.wrap)
	}
	if m.wrap != view.wrap {
		m.wrap = view.wrap
		m.rebuild()
	}
	m.history.rows, m.history.pos, m.history.horizontal, m.history.viewTop = view.rows, view.pos, view.horizontal, view.viewTop
	m.history.gutterWidth = view.gutterWidth
	m.collapsedFiles, m.generatedHandled = view.collapsedFiles, view.generatedHandled
	return m, nil
}

func (m model) streamJumpBeyondEnd(files bool, count int) bool {
	rows := m.history.rows
	if len(rows) == 0 {
		return true
	}
	i := max(0, min(m.history.pos, len(rows)-1))
	if files {
		path := rows[i].path
		for i++; i < len(rows); i++ {
			if rows[i].kind == 'f' && rows[i].path != path {
				path = rows[i].path
				count--
				if count == 0 {
					return false
				}
			}
		}
		return true
	}
	for count > 0 {
		if changedRow(rows[i]) {
			path := rows[i].path
			for i < len(rows) && changedRow(rows[i]) && rows[i].path == path {
				i++
			}
		} else {
			i++
		}
		for i < len(rows) && !changedRow(rows[i]) {
			i++
		}
		if i >= len(rows) {
			return true
		}
		count--
	}
	return false
}

func (m model) returnToHistory() (tea.Model, tea.Cmd) {
	m.cancelCommitLoad()
	m.releaseLargeCommitRows()
	if m.stash.viewing {
		return m.openStashes()
	}
	if m.history.directCommit {
		m.history.directCommit = false
		next, historyCmd := m.openHistory()
		refreshed, refreshCmd := next.(model).refresh()
		return refreshed, tea.Batch(historyCmd, refreshCmd)
	}
	m.screen = "history"
	m.pickerReturn, m.pickerBackToHistory = false, false
	m.history.request++
	m.history.loading = false
	m.history.fetching = false
	m, refreshCmd := m.refresh()
	if m.history.stale {
		next, historyCmd := m.reloadHistory()
		return next, tea.Batch(historyCmd, refreshCmd)
	}
	next, historyCmd := m.loadHistoryBackground()
	return next, tea.Batch(historyCmd, refreshCmd)
}

// releaseLargeCommitRows returns a finished huge commit's rows and patch to the
// OS so a forgotten pane does not hold them while showing history.
func (m *model) releaseLargeCommitRows() {
	// Diagnostic switch for the C4 measurement; not a user setting.
	if os.Getenv("PANE_KEEP_COMMIT_ROWS") == "1" {
		return
	}
	if len(m.history.rows) > 500000 {
		m.history.rows, m.history.changes = nil, nil
		debug.FreeOSMemory()
	}
}

func (m model) commitModel() model {
	view := m
	view.screen = ""
	view.relativeLines = true
	view.contextCommit = m.history.current.Hash
	view.viewIdentity = "commit:" + m.history.current.Hash
	view.gutterFloor = m.history.streamGutter
	view.gutterWidth = m.history.gutterWidth
	if m.screen == "base" {
		view.contextCommit, view.contextBase = m.base.hash, true
		view.viewIdentity = "base:" + m.base.hash + ":" + m.base.revision
	}
	view.jumps, view.jumpIndex = m.history.jumps, m.history.jumpIndex
	view.snapshot = Snapshot{Unstaged: m.history.changes}
	view.expandedDeleted = m.history.expandedDeleted
	view.staged = false
	view.rows, view.pos, view.horizontal, view.viewTop = m.history.rows, m.history.pos, m.history.horizontal, m.history.viewTop
	view.viewTitle = m.accent() + shortHash(m.history.current.Hash) + styleReset + "  " + clean(m.history.current.Subject)
	if index, total := m.commitListPosition(); index > 0 && total > 1 {
		view.viewTitle += styleDim + fmt.Sprintf("  %d/%d", index, total) + styleReset
	}
	view.viewBadge = styleDim + "read-only" + styleReset
	if m.screen == "commit" && m.history.streamActive && len(m.history.streamPaths) > 0 {
		view.viewBadge = styleDim + fmt.Sprintf("read-only · loading %d/%d files", m.history.streamLoaded, len(m.history.streamPaths)) + styleReset
		view.searchProgress = "loading"
	}
	if m.screen == "base" {
		view.viewTitle = clean(m.base.revision) + "…working tree"
		view.viewBadge = styleDim + shortHash(m.base.hash) + " · read-only" + styleReset
		if m.history.streamActive && len(m.history.streamPaths) > 0 {
			view.viewBadge = styleDim + shortHash(m.base.hash) + fmt.Sprintf(" · loading %d/%d files", m.history.streamLoaded, len(m.history.streamPaths)) + styleReset
			view.searchProgress = "loading"
		}
	}
	view.viewHints = keyHints("esc", "back", "?", "help")
	if viewed, total := viewedCountRows(m.history.rows); viewed > 0 {
		view.viewBadge += styleDim + fmt.Sprintf(" · %d/%d viewed", viewed, total) + styleReset
	}
	view.message = ""
	if m.history.loading && m.history.commitLoadingVisible {
		view.message = "Loading commit…"
	}
	if m.picker.pendingPath != "" {
		view.message = "Loading " + m.picker.pendingPath + "…"
	}
	if m.history.streamError != "" {
		view.message = m.history.streamError
	}
	return view
}

func (m *model) rebuildCommit() {
	if m.screen != "commit" && m.screen != "base" {
		return
	}
	if m.screen == "commit" && m.history.moved == nil && len(m.history.changes) > 0 {
		m.history.moved = newMovedIndex()
		m.history.moved.addChanges(m.history.changes)
	}
	view := m.commitModel()
	view.rebuild()
	m.history.rows, m.history.pos, m.history.viewTop = view.rows, view.pos, view.viewTop
	m.history.gutterWidth = view.gutterWidth
	m.collapsedFiles, m.generatedHandled = view.collapsedFiles, view.generatedHandled
}

func (m model) commitView() string { return m.commitModel().View() }

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func (m model) historyView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	lines := m.historyHeader()
	available := max(0, m.height-len(lines)-1)
	matches := m.filteredHistory()
	selected := min(m.history.selected, max(0, len(matches)-1))
	spacing := 0
	if available >= 4 {
		spacing = 1
	}
	offset, _ := m.historyViewport(len(matches))
	visualLo, visualHi := -1, -1
	if m.history.visualActive {
		visualLo = max(0, min(m.history.visualAnchor, selected))
		visualHi = min(len(matches)-1, max(m.history.visualAnchor, selected))
	}
	var content []string
	if spacing > 0 && len(matches) > 0 {
		content = append(content, "")
	}
	now := time.Now()
	for i := offset; i < len(matches); i++ {
		c := matches[i]
		subject := styleDim + "  " + fit(clean(c.Subject), max(0, m.width-2)) + styleReset
		if i == selected {
			subject = styleBold + m.accent() + "▸ " + styleReset + styleBold + fit(clean(c.Subject), max(0, m.width-2)) + styleReset
		} else if i >= visualLo && i <= visualHi {
			subject = styleBold + m.accent() + "│ " + styleReset + styleBold + fit(clean(c.Subject), max(0, m.width-2)) + styleReset
		}
		date := historyCommitTime(c.Date, now)
		meta := historyMetadata(c, date, m.width, i == selected, m.accent(), m.worktreeHeads)
		content = append(content, subject, meta, "")
		if len(content) >= available {
			break
		}
	}
	if len(content) == 0 {
		text := " No matching commits"
		if m.history.loading {
			text = " Loading history…"
		} else if len(m.history.commits) == 0 {
			text = " No commits"
		}
		content = append(content, fit(text, m.width))
	}
	for i := 0; i < available; i++ {
		if i < len(content) {
			lines = append(lines, content[i])
		} else {
			lines = append(lines, "")
		}
	}
	status, hints := "", keyHints("↵", "open", "/", "search", "?", "help")
	if m.history.visualActive {
		lo, hi := visualLo, visualHi
		count := max(0, hi-lo+1)
		status = fmt.Sprintf("%d commit(s) selected", count)
		hints = keyHints("↵", "review", "esc", "cancel")
	}
	if m.history.searching {
		status, hints = "/"+renderInput(m.history.draft, m.history.draftCursor, max(1, m.width-1)), keyHints("↵", "open", "esc", "cancel")
	} else if m.history.filter != "" {
		status, hints = "/"+clean(m.history.filter), keyHints("↵", "open", "/", "edit", "esc", "back")
	}
	if m.flash != "" {
		status = clean(m.flash)
	}
	if m.modal == "branch-new" {
		status, hints = "New branch from "+shortHash(m.branch.newFrom)+": "+renderInput(m.input, m.inputCursor, max(1, m.width-32)), keyHints("↵", "create", "esc", "cancel")
	}
	if m.history.fetching && m.historyQuery() != "" {
		status += styleDim + "  searching older commits…" + styleReset
	}
	if m.modal == "help" || m.modal == "commit" {
		status, hints = "", ""
		if m.modal == "commit" && (m.width < 24 || m.height < 9) {
			status = prompt("Commit message: ", m.input)
		}
	}
	lines = append(lines, m.statusBar(status, hints))
	if m.modal == "help" {
		clearBody(lines)
		m.showHelpDialog(lines)
	} else if m.modal == "commit" {
		clearBody(lines)
		if m.width >= 24 && m.height >= 9 {
			m.showCommitDialog(lines)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) historyViewport(count int) (int, int) {
	available := max(0, m.height-m.historyHeaderHeight()-1)
	spacing := 0
	if available >= 4 {
		spacing = 1
	}
	visible := max(1, (available-spacing)/3)
	selected := min(m.history.selected, max(0, count-1))
	offset := m.history.offset
	if selected < offset {
		offset = selected
	}
	if selected >= offset+visible {
		offset = selected - visible + 1
	}
	return min(offset, count), visible
}

func (m *model) revealHistorySelection() {
	if m.screen == "history" {
		m.history.offset, _ = m.historyViewport(len(m.filteredHistory()))
	}
}

func historyMetadata(c CommitInfo, date string, width int, selected bool, accent string, worktreeHeads map[string]string) string {
	type segment struct{ text, style string }
	green, red, cyan := "\x1b[32m", "\x1b[31m", accent
	if !selected {
		green, red, cyan = "\x1b[2;32m", "\x1b[2;31m", styleDim+accent
	}
	segments := []segment{{"  ", ""}}
	if c.StatsLoaded {
		segments = append(segments, segment{fmt.Sprintf("+%d", c.Added), green}, segment{fmt.Sprintf(" -%d", c.Deleted), red})
	} else {
		segments = append(segments, segment{"…", "\x1b[2m"})
	}
	if c.Binary > 0 {
		segments = append(segments, segment{fmt.Sprintf("  %d binary", c.Binary), "\x1b[2m"})
	}
	segments = append(segments,
		segment{"  " + clean(date), "\x1b[2m"},
		segment{"  " + clean(c.Author), "\x1b[2m"},
		segment{"  " + shortHash(c.Hash), cyan},
	)
	if name := worktreeHeads[c.Hash]; name != "" {
		segments = append(segments, segment{"  ⌂ " + clean(name), "\x1b[2m"})
	}
	var b strings.Builder
	remaining := width
	for _, part := range segments {
		if remaining <= 0 {
			break
		}
		text := fit(part.text, remaining)
		if part.style != "" {
			b.WriteString(part.style)
		}
		b.WriteString(text)
		if part.style != "" {
			b.WriteString("\x1b[0m")
		}
		remaining -= stringWidth(text)
	}
	return b.String()
}

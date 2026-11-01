package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/mattn/go-runewidth"
)

type loaded struct {
	snapshot   snapshot
	err        error
	generation int
	cache      *refreshCache
}
type loadedUnchanged struct {
	generation int
	cache      *refreshCache
}
type tick struct{}
type resizeRebuild struct{ request int }
type completed struct {
	message       string
	commitMessage string
	action        string
	err           error
	stageCursor   *stagingCursor
}

type recentCommitsLoaded struct {
	commits []commitInfo
	err     error
	request int
}

type model struct {
	repo                  repository
	refreshCache          *refreshCache
	snapshot              snapshot
	rows                  []row
	width, height         int
	gutterFloor           int
	gutterWidth           int
	sequenceGutters       bool
	resizeRequest         int
	pos, horizontal       int
	viewTop               int
	pending               string
	count                 int
	jumps                 []jumpLocation
	jumpIndex             int
	staged, wrap          bool
	shortPaths            bool
	relativeLines         bool
	lightTheme            bool
	themeAuto             bool
	themeQuery            func()
	ticks                 int
	blurred               bool
	whitespaceRefreshing  bool
	loading, busy         bool
	activeAction          string
	startup               bool
	generation            int
	modal, input, message string
	inputCursor           int
	inputBody             string
	inputNormal           bool
	inputPending          string
	amendHash             string
	commitEditorPath      string
	errorText             string
	errorPos              int
	helpPos               int
	screen, viewTitle     string
	viewBadge, viewHints  string
	history               historyState
	picker                pickerState
	pickerReturn          bool
	pickerBackToHistory   bool
	search                searchState
	searchProgress        string
	flash                 string
	expandedDeleted       map[string]bool
	contextFiles          map[contextKey]contextFile
	collapsedFiles        map[contextKey]bool
	contextRequest        int
	contextCommit         string
	syntaxDemand          *syntaxDemand
	recentCommits         []commitInfo
	recentCommitRequest   int
	recentCommitLoading   bool
	recentCommitError     bool
	discard               discardRequest
	stageCursor           *stagingCursor
	sequence              sequenceState
	stash                 stashState
	base                  baseState
	contextBase           bool
	fileHistoryReturn     *model
	prefetch              *startupPrefetch
}

func main() {
	repo, err := openRepository()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	piped := !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd())
	var prefetch *startupPrefetch
	if !piped && len(os.Args) == 1 {
		prefetch = startPrefetch(repo)
	}
	var m model
	if piped && len(os.Args) == 1 {
		m, err = initialSequence(repo, os.Stdin)
	} else {
		m, err = initialModel(repo, os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m.prefetch = prefetch
	theme, colorFGBG := strings.ToLower(strings.TrimSpace(os.Getenv("PANE_THEME"))), os.Getenv("COLORFGBG")
	m.lightTheme = lightThemeFromEnv(theme, colorFGBG)
	options := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()}
	var tracking themeTracking
	if theme != "light" && theme != "dark" {
		tracking = startThemeTracking()
		if tracking.input != nil {
			// Init queries the background without blocking startup; terminals
			// that never answer would otherwise delay every launch.
			m.themeAuto, m.themeQuery = true, tracking.query
			options = append(options, tea.WithInput(tracking.input))
			defer tracking.close()
		} else if light, ok := terminalLightBackground(); ok {
			m.lightTheme = light
		}
	}
	if piped && tracking.input == nil {
		options = append(options, tea.WithInputTTY())
	}
	program := tea.NewProgram(m, options...)
	if tracking.setHandler != nil {
		tracking.setHandler(func(light bool) { go program.Send(terminalBackgroundChanged(light)) })
	}
	stopProfile := startCPUProfile(os.Getenv("PANE_CPUPROFILE"))
	stopTrace := startTrace(os.Getenv("PANE_TRACE"))
	final, err := program.Run()
	stopTrace()
	stopProfile()
	if path := os.Getenv("PANE_MEMPROFILE"); path != "" {
		if profileErr := writeHeapProfile(path); profileErr != nil {
			fmt.Fprintln(os.Stderr, profileErr)
		}
	}
	if result, ok := final.(model); ok {
		result.closeHistoryStreams()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func startTrace(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := trace.Start(file); err != nil {
		file.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return func() {
		trace.Stop()
		file.Close()
	}
}

func writeHeapProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	runtime.GC()
	return pprof.WriteHeapProfile(file)
}

// startCPUProfile records a CPU profile to path, for building default.pgo
// from real sessions. It returns a function that stops recording.
func startCPUProfile(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return func() {
		pprof.StopCPUProfile()
		file.Close()
	}
}

func (m model) closeHistoryStreams() {
	m.cancelCommitLoad()
	m.prefetch.close()
	m.history.stream.close()
	if m.fileHistoryReturn != nil {
		m.fileHistoryReturn.history.stream.close()
	}
}

func initialModel(repo repository, args []string) (model, error) {
	if len(args) == 2 && args[0] == "--" {
		return initialFileHistory(repo, args[1])
	}
	if len(args) == 3 && args[0] == "log" && args[1] == "--" {
		return initialFileHistory(repo, args[2])
	}
	if len(args) > 2 || (len(args) == 2 && args[0] != "--base" && args[0] != "-b") || (len(args) == 1 && (args[0] == "--base" || args[0] == "-b")) {
		return model{}, fmt.Errorf("%s", fileHistoryUsage())
	}
	m := model{repo: repo, wrap: true, loading: true, startup: true}
	if len(args) == 0 {
		return m, nil
	}
	base := ""
	if len(args) == 2 {
		base = args[1]
		if base == "" || strings.HasPrefix(base, "-") {
			return model{}, fmt.Errorf("invalid base %q", base)
		}
	} else if strings.HasSuffix(args[0], "...") && !strings.Contains(strings.TrimSuffix(args[0], "..."), "..") {
		base = strings.TrimSuffix(args[0], "...")
		if base == "" {
			return model{}, fmt.Errorf("base revision cannot be empty")
		}
	}
	if base != "" {
		hash, err := repo.mergeBase(base)
		if err != nil {
			return model{}, err
		}
		m.screen, m.base = "base", baseState{revision: base, hash: hash}
		m.history.directCommit = true
		return m, nil
	}
	if strings.Contains(args[0], "..") {
		out, err := repo.command("rev-list", "--max-count=1001", "--end-of-options", args[0], "--")
		if err != nil {
			return model{}, fmt.Errorf("invalid commit range %q: %w", args[0], gitError(out, err))
		}
		if len(out) == 0 {
			return model{}, fmt.Errorf("commit range %q contains no commits", args[0])
		}
		return initialSequence(repo, strings.NewReader(string(out)))
	}
	commit, err := repo.resolveCommit(args[0])
	if err != nil {
		return model{}, err
	}
	m.screen = "commit"
	m.history.current = commit
	m.history.loading = true
	m.history.request++
	m.history.directCommit = true
	m.history.streamActive = true
	m.startCommitLoad()
	return m, nil
}

func (m model) Init() tea.Cmd {
	initial := load(m.repo, m.generation, m.refreshCache)
	if m.prefetch != nil {
		initial = m.prefetch.loadStatus()
	}
	commands := []tea.Cmd{initial, nextTick(), m.queryTheme()}
	if m.screen == "history" && m.history.path != "" && m.history.loading {
		commands = append(commands, m.fetchHistory(0, historyPage))
	}
	if m.screen == "commit" && m.history.loading && m.history.directCommit {
		repo, hash, request := m.commitRepository(), m.history.current.hash, m.history.request
		commands = append(commands, startCommitStream(repo, hash, request, m.height))
		commands = append(commands, commitLoadingTimer(request))
	}
	if m.screen == "sequence" && m.sequence.loading {
		for i, state := range m.sequence.states {
			if state == sequenceLoading {
				commands = append(commands, loadSequenceCommit(m.repo, m.sequence.commits[i].hash, i, m.sequence.request))
			}
		}
	}
	return tea.Batch(commands...)
}

// queryTheme asks the terminal for its background color; the answer arrives
// as terminalBackgroundChanged.
func (m model) queryTheme() tea.Cmd {
	if !m.themeAuto || m.themeQuery == nil || editorRunning.Load() {
		return nil
	}
	query := m.themeQuery
	return func() tea.Msg {
		if !editorRunning.Load() {
			query()
		}
		return nil
	}
}

func (m model) queryThemeOnTick() tea.Cmd {
	if m.blurred || m.ticks%10 != 0 {
		return nil
	}
	return m.queryTheme()
}

func load(repo repository, generation int, cache *refreshCache) tea.Cmd {
	return loadWith(repo, generation, cache, false)
}

func loadWith(repo repository, generation int, cache *refreshCache, deferOffscreen bool) tea.Cmd {
	return func() tea.Msg {
		next, unchanged, err := repo.loadChanges(cache, deferOffscreen)
		if err != nil {
			return loaded{err: err, generation: generation}
		}
		if unchanged {
			return loadedUnchanged{generation: generation, cache: next}
		}
		return loaded{snapshot: next.snapshot, generation: generation, cache: next}
	}
}

// Refreshes run every refreshTicks ticks while the terminal is focused, and
// every blurredRefreshTicks while it is not, so a pane left in a background
// window polls Git less often. Regaining focus refreshes immediately.
const (
	refreshTicks        = 2
	blurredRefreshTicks = 6
)

func nextTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tick{} })
}

func (m model) refresh() (model, tea.Cmd) {
	if m.loading || m.busy {
		return m, nil
	}
	m.loading = true
	return m, load(m.repo, m.generation, m.refreshCache)
}

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
	return view, cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case syntaxDemandLoaded:
		m.applySyntaxDemandLoaded(msg)
		return m, nil
	case tea.WindowSizeMsg:
		widthChanged := m.width != msg.Width
		m.width, m.height = msg.Width, msg.Height
		if widthChanged {
			m.resizeRequest++
			if max(len(m.rows), len(m.history.rows)) >= 50000 {
				request := m.resizeRequest
				cmd := tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return resizeRebuild{request} })
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
		m.rebuild()
		m.rebuildCommit()
		return m, nil
	case tick:
		var cmd tea.Cmd
		m.ticks++
		interval := refreshTicks
		if m.blurred || m.screen == "history" {
			interval = blurredRefreshTicks
		}
		if (m.screen == "" || m.screen == "base" || (m.screen == "history" && m.history.path == "")) && m.ticks%interval == 0 {
			m, cmd = m.refresh()
		}
		return m, tea.Batch(cmd, nextTick(), m.queryThemeOnTick())
	case tea.BlurMsg:
		m.blurred = true
	case tea.FocusMsg:
		m.blurred, m.ticks = false, 0
		m, cmd := m.refresh()
		return m, tea.Batch(cmd, m.queryTheme())
	case terminalBackgroundChanged:
		if m.themeAuto {
			m.lightTheme = bool(msg)
		}
	case loadedUnchanged:
		m.loading = false
		if msg.generation != m.generation {
			return m.refresh()
		}
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
		headChanged := m.snapshot.branch.available && msg.snapshot.branch.available && m.snapshot.branch.head != msg.snapshot.branch.head
		changed := !m.snapshot.equal(msg.snapshot)
		if changed {
			m.snapshot = msg.snapshot
			m.rebuild()
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
			if len(msg.snapshot.unstaged) == 0 && len(msg.snapshot.staged) == 0 && m.screen == "" && m.modal == "" {
				return m.openHistory()
			}
		}
		m.prefetch.close()
		m.prefetch = nil
		if msg.cache != nil && msg.cache.plain {
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
			m.history.changes = msg.changes
			m.contextFiles = nil
			m.rebuildCommit()
		}
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
			m.recentCommits = []commitInfo{}
		}
	case historyLoaded:
		if m.screen != "history" || msg.request != m.history.request || (msg.skip > 0 && (!m.history.fetching || msg.skip != len(m.history.commits))) {
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
				if cached, ok := m.history.statsCache[c.hash]; ok {
					msg.commits[i].added = cached.added
					msg.commits[i].deleted = cached.deleted
					msg.commits[i].binary = cached.binary
					msg.commits[i].statsLoaded = true
				}
			}
			m.history.commits = msg.commits
			m.cacheHistoryStats()
		}
		m.history.more = len(msg.commits) == msg.limit
		m.history.selected = min(m.history.selected, max(0, len(m.filteredHistory())-1))
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
		stats := make(map[string]commitInfo, len(msg.commits))
		for _, c := range msg.commits {
			stats[c.hash] = c
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
			if s, ok := stats[c.hash]; ok {
				m.history.commits[i].added = s.added
				m.history.commits[i].deleted = s.deleted
				m.history.commits[i].binary = s.binary
				m.history.commits[i].statsLoaded = true
			}
		}
		m.cacheHistoryStats()
		return m.loadHistoryStats()
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
		m.stash.selected = min(m.stash.selected, max(0, len(msg.entries)-1))
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
		m.rebuildCommit()
	case commitLoadingDelay:
		if m.screen == "commit" && m.history.loading && msg.request == m.history.request {
			m.history.commitLoadingVisible = true
		}
	case commitStreamLoaded:
		if m.screen != "commit" || !m.history.streamActive || msg.request != m.history.request {
			break
		}
		chunk := msg.chunk
		m.history.streamPaths, m.history.streamLoaded = chunk.paths, chunk.loaded
		m.history.commitLoadingVisible = false
		m.appendCommitChanges(chunk.changes, chunk.done)
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
		if m.history.streamEndPending && len(m.history.rows) > 0 {
			m.history.pos = len(m.history.rows) - 1
			m.history.viewTop = max(0, m.history.pos-max(1, m.height-2)+1)
		}
		if m.modal == "search" && m.search.draft != "" {
			m = m.inDiff(func(view *model) { view.updateSearchDraft() })
		}
		if chunk.done {
			m.history.streamActive, m.history.loading, m.history.streamEndPending = false, false, false
			m.picker.cache = nil
			if chunk.err != nil {
				m.history.streamError = "Commit load: " + chunk.err.Error()
			}
			if m.picker.pendingPath != "" {
				m.flash = "File not found: " + m.picker.pendingPath
				m.picker.pendingPath = ""
			}
			break
		}
		return m, waitCommitStream(msg.stream, msg.request)
	case commitLoaded:
		if m.screen != "commit" || msg.request != m.history.request {
			break
		}
		m.history.loading = false
		m.history.streamActive = false
		m.history.streamPaths, m.history.streamGutter, m.history.streamError = nil, 0, ""
		m.history.commitLoadingVisible = false
		if msg.err != nil {
			m.screen = "history"
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			if m.history.stale {
				return m.reloadHistory()
			}
			break
		}
		m.history.changes = msg.changes
		m.history.rows = nil
		m.history.pos = 0
		m.rebuildCommit()
		m.jumpToHistoryFile()
		return m, loadCommitSyntax(m.commitRepository(), m.history.current.hash, m.history.request, m.history.changes)
	case commitSyntaxLoaded:
		if m.screen != "commit" || msg.request != m.history.request {
			break
		}
		m.history.changes = msg.changes
		m.rebuildCommit()
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
		m.flash = ""
		if m.modal != "" {
			return m.handleModal(msg)
		}
		if msg.String() == "L" && m.pending == "" && (m.screen == "" || m.screen == "commit" || m.screen == "base" || m.screen == "sequence") {
			return m.openFileHistory()
		}
		if msg.String() == "F" && m.pending == "" && !(m.screen == "history" && m.history.searching) {
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
		if msg.String() == "T" && m.pending == "" && !(m.screen == "history" && m.history.searching) {
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
			m.helpPos = 0
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
				if len(m.snapshot.staged) == 0 && len(m.snapshot.unstaged) == 0 {
					m.message = "Nothing to commit"
					return m, nil
				}
				m.modal = "commit"
				m.input = ""
				m.inputCursor = 0
				m.inputBody = ""
				m.inputNormal = false
				m.inputPending = ""
				m.amendHash = ""
				m.message = ""
				m.recentCommits = nil
				m.recentCommitLoading = true
				m.recentCommitError = false
				m.recentCommitRequest++
				repo, request := m.repo, m.recentCommitRequest
				return m, func() tea.Msg {
					commits, err := repo.historyWithDate(0, 5, "%cI")
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
				return completed{message: "Discarded changes", err: repo.discard(request)}
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
		switch key.String() {
		case "esc", "?", "q":
			m.modal = ""
		case "j", "down":
			m.helpPos = min(m.helpPos+1, max(0, len(shortcutLines(m.width, ""))-helpVisibleRows(m.height)))
		case "k", "up":
			m.helpPos = max(0, m.helpPos-1)
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
	if m.modal == "commit" && key.String() == "esc" && !m.inputNormal {
		m.inputNormal = true
		m.inputCursor = max(0, m.inputCursor-1)
		return m, nil
	}
	if m.modal == "commit" && key.String() == "esc" && m.inputPending != "" {
		m.inputPending = ""
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
		if m.inputNormal && key.String() != "enter" {
			m.handleCommitNormal(key)
			return m, nil
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
		output, err := repo.action(args...)
		return completed{message: output, action: args[0], err: err}
	}
}

func (m *model) rebuild() {
	if m.screen == "sequence" {
		m.rebuildSequence()
		return
	}
	var anchor row
	if len(m.rows) > 0 && m.pos < len(m.rows) {
		anchor = m.rows[m.pos]
	}
	changes := m.snapshot.unstaged
	if m.staged {
		changes = m.snapshot.staged
	}
	changes = m.applySyntaxDemand(contextKey{commit: m.contextCommit, staged: m.staged, base: m.contextBase}, changes)
	m.rows, m.gutterWidth = buildRowsDisplay(changes, m.diffContentWidth(), m.wrap, m.expandedDeleted, m.gutterFloor, m.shortPaths)
	m.rows = m.localContextRows(m.rows, changes)
	m.rows = m.foldContextFiles(m.rows)
	m.pos = 0
	if anchor.path != "" {
		best := -1
		for i, r := range m.rows {
			if r.path != anchor.path {
				continue
			}
			if r.line < anchor.line || (r.line == anchor.line && r.offset <= anchor.offset) {
				best = i
				continue
			}
			if best < 0 {
				best = i
			}
			break
		}
		if best >= 0 {
			m.pos = best
		}
	}
	m.clamp()
	m.viewTop = m.viewportStart(max(1, m.height-2))
}

func (m *model) toggleDeletedFile() {
	if len(m.rows) == 0 || m.pos >= len(m.rows) {
		return
	}
	path := m.rows[m.pos].path
	changes := m.snapshot.unstaged
	if m.staged {
		changes = m.snapshot.staged
	}
	for _, c := range changes {
		if c.path != path || !strings.Contains(c.diff, "\ndeleted file mode ") || strings.Contains(c.diff, "\nnew file mode ") {
			continue
		}
		if m.expandedDeleted == nil {
			m.expandedDeleted = make(map[string]bool)
		}
		m.expandedDeleted[path] = !m.expandedDeleted[path]
		m.rebuild()
		return
	}
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

func wrapLine(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	var parts []string
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w > 0 && w+rw > width {
			parts = append(parts, b.String())
			b.Reset()
			w = 0
		}
		b.WriteRune(r)
		w += rw
	}
	return append(parts, b.String())
}

func (m *model) clamp() { m.pos = max(0, min(m.pos, len(m.rows)-1)) }

// lineStart returns the first row of the wrapped line containing row i.
func (m model) lineStart(i int) int {
	for i > 0 && i < len(m.rows) && m.rows[i].offset > 0 {
		i--
	}
	return i
}

// lineRows converts a count of whole lines from the active line into a row
// delta, so j and k step over wrapped continuation rows.
func (m model) lineRows(n int) int {
	if len(m.rows) == 0 {
		return 0
	}
	pos := m.lineStart(m.pos)
	for ; n > 0; n-- {
		next := pos + 1
		for next < len(m.rows) && m.rows[next].offset > 0 {
			next++
		}
		if next >= len(m.rows) {
			break
		}
		pos = next
	}
	for ; n < 0 && pos > 0; n++ {
		pos = m.lineStart(pos - 1)
	}
	return pos - m.pos
}

// move moves the active row by n rows, landing on the start of a line.
func (m *model) move(n int) {
	visible := max(1, m.height-2)
	top := m.viewportStart(visible)
	wasVisible := m.pos >= top && m.pos < top+visible
	from := m.pos
	m.pos += n
	m.clamp()
	if start := m.lineStart(m.pos); start > from || n <= 0 {
		m.pos = start
	} else if next := m.pos + m.lineRows(1); next > m.pos {
		// A downward move that would snap back to where it started skips to
		// the next line instead.
		m.pos = next
	}
	m.reveal()
	if wasVisible {
		if n > 0 {
			m.viewTop = max(m.viewTop, top)
		} else if n < 0 {
			m.viewTop = min(m.viewTop, top)
		}
	}
}

func (m *model) paragraphJump(direction int) {
	if len(m.rows) == 0 {
		return
	}
	i := m.pos + direction
	if paragraphBoundary(m.rows[m.pos]) {
		for i >= 0 && i < len(m.rows) && paragraphBoundary(m.rows[i]) {
			i += direction
		}
	}
	for i >= 0 && i < len(m.rows) && !paragraphBoundary(m.rows[i]) {
		i += direction
	}
	m.pos = max(0, min(i, len(m.rows)-1))
	m.reveal()
}

func paragraphBoundary(r row) bool {
	return r.kind == 'f' || (r.kind == '@' && strings.TrimSpace(r.text) == "···") || strings.TrimSpace(r.text) == ""
}

func diffMargin(width int) int {
	if width < 3 {
		return 1
	}
	return 2
}

func (m model) viewportStart(available int) int {
	available = max(1, available)
	top := max(0, min(m.viewTop, max(0, len(m.rows)-available)))
	if m.pos < top {
		top = m.pos
	}
	if m.pos >= top+available {
		top = m.pos - available + 1
	}
	return max(0, min(top, max(0, len(m.rows)-available)))
}

func (m *model) jump(direction int) {
	if len(m.rows) == 0 {
		return
	}
	path := m.rows[m.pos].path
	for i := m.pos + direction; i >= 0 && i < len(m.rows); i += direction {
		if m.rows[i].kind == 'f' && (m.rows[i].path != path || (m.screen == "sequence" && m.rows[i].commitIndex != m.rows[m.pos].commitIndex)) {
			m.pos = i
			m.positionViewport("t")
			return
		}
	}
	m.reveal()
}

func (m model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.modal == "error" || m.modal == "discard" || m.modal == "stash-confirm" {
		return m.confirmationView()
	}
	if m.screen == "history" {
		return m.historyView()
	}
	if m.screen == "stashes" {
		return m.stashView()
	}
	if m.screen == "commit" || m.screen == "base" {
		return m.commitView()
	}
	if m.screen == "sequence" {
		return m.sequenceView()
	}
	title := " " + m.viewTitle
	if m.viewTitle == "" {
		title = " " + m.stageTabs()
	}
	var lines []string
	lines = append(lines, m.titleBar(title, m.viewBadge))
	available := max(0, m.height-2)
	start, visible, pinned := m.diffViewport(available)
	if pinned != nil {
		// Overlay the top row so pinning and unpinning never shifts the rows below it.
		if m.pos == start {
			pinned = nil
			visible++
		} else {
			start++
		}
	}
	if pinned != nil {
		lines = append(lines, m.renderNumberedDiffRow(*pinned, -1))
	}
	for i := 0; i < visible; i++ {
		index := start + i
		if index >= len(m.rows) {
			if index == 0 {
				lines = append(lines, fit(m.emptyText(), m.width))
			} else if index == len(m.rows) && m.contextCommit != "" && m.history.streamActive {
				lines = append(lines, styleDim+"  Loading…"+styleReset)
			} else {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, m.renderNumberedDiffRow(m.rows[index], index))
	}
	status := clean(strings.ReplaceAll(m.message, "\n", " | "))
	if m.flash != "" {
		status = clean(strings.ReplaceAll(m.flash, "\n", " | "))
	}
	if m.repo.ignoreSpace && m.modal == "" {
		badge := styleDim + "ignoring whitespace" + styleReset
		if status != "" {
			status += "  "
		}
		status += badge
	}
	hints := m.viewHints
	if hints == "" {
		hints = keyHints("?", "help")
	}
	switch m.modal {
	case "search":
		prefix := "/"
		if m.search.draftDirection < 0 {
			prefix = "?"
		}
		status, hints = prefix+renderInput(m.search.draft, m.search.cursor, max(1, m.width-1)), keyHints("↵", "search", "esc", "cancel")
	case "commit":
		status, hints = "", ""
		if m.width < 24 || m.height < 9 {
			mode := "INSERT"
			if m.inputNormal {
				mode = "NORMAL"
			}
			status = prompt("Commit message ["+mode+"]: ", m.input)
		}
	case "help", "files":
		status, hints = "", ""
	}
	if m.searchProgress != "" && m.activeSearch() != "" && (m.modal == "" || m.modal == "search") {
		if m.modal == "" && m.flash == "" {
			status = "/" + clean(m.search.query)
		}
		status += styleDim + "  · " + m.searchProgress + styleReset
	}
	lines = append(lines, m.statusBar(status, hints))
	if m.modal == "commit" || m.modal == "help" || m.modal == "files" {
		clearBody(lines)
	}
	if m.modal == "commit" && m.width >= 24 && m.height >= 9 {
		m.showCommitDialog(lines)
	}
	if m.modal == "help" {
		m.showHelpDialog(lines)
	}
	if m.modal == "files" {
		m.showFilesDialog(lines)
	}
	return strings.Join(lines, "\n")
}

// confirmationView shows an error or discard confirmation over the current
// screen's title and status bars.
func (m model) confirmationView() string {
	under := m
	under.modal = ""
	lines := strings.Split(under.View(), "\n")
	small := m.width < 24 || m.height < 8
	if m.modal == "discard" && small {
		return m.discardView()
	}
	if m.modal == "stash-confirm" && small {
		return strings.Join([]string{fit(" "+strings.ToUpper(m.stash.operation)+" "+m.stash.target.ref+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "error" && small {
		content := m.errorLines(m.width)
		start := min(m.errorPos, max(0, len(content)-1))
		lines := []string{fit(" Git error", m.width)}
		for i := 0; i < max(0, m.height-2); i++ {
			if start+i < len(content) {
				lines = append(lines, content[start+i])
			} else {
				lines = append(lines, "")
			}
		}
		lines = append(lines, fit("j/k scroll  enter/esc dismiss", m.width))
		return strings.Join(lines, "\n")
	}
	clearBody(lines)
	lines[len(lines)-1] = ""
	if m.modal == "error" {
		m.showErrorDialog(lines)
	} else if m.modal == "stash-confirm" {
		m.showStashConfirm(lines)
	} else {
		m.showDiscardDialog(lines)
	}
	return strings.Join(lines, "\n")
}

// stageTabs shows the unstaged and staged views with the active one
// emphasized, so tab's effect is visible before pressing it.
func (m model) stageTabs() string {
	tab := func(name string, count int, active bool) string {
		if active {
			return styleBold + name + " " + fmt.Sprint(count) + styleReset
		}
		return styleDim + name + " " + fmt.Sprint(count) + styleReset
	}
	return tab("unstaged", len(m.snapshot.unstaged), !m.staged) + styleDim + " · " + styleReset + tab("staged", len(m.snapshot.staged), m.staged)
}

func (m model) emptyText() string {
	if m.relativeLines && m.history.loading {
		if m.history.commitLoadingVisible {
			return " Loading commit…"
		}
		return ""
	}
	if m.viewTitle != "" {
		return " No changes"
	}
	if m.loading && m.startup {
		return ""
	}
	if m.staged {
		if n := len(m.snapshot.unstaged); n > 0 {
			return fmt.Sprintf(" No staged changes · tab shows %d unstaged", n)
		}
		return " No staged changes"
	}
	if n := len(m.snapshot.staged); n > 0 {
		return fmt.Sprintf(" No unstaged changes · tab shows %d staged", n)
	}
	return " No changes · H opens history"
}

func wrapFlash(wrap bool) string {
	if wrap {
		return "Line wrapping on"
	}
	return "Line wrapping off"
}

func (m model) relativeGutterWidth() int {
	if !m.relativeLines && m.screen != "sequence" {
		return 0
	}
	return max(3, len(fmt.Sprint(max(0, m.height-2))))
}

func (m model) diffContentWidth() int {
	extra := max(0, m.relativeGutterWidth()-3)
	return max(1, m.width-diffMargin(m.width)-extra)
}

// lineDistance counts whole lines between row index and the active row,
// matching how j and k move.
func (m model) lineDistance(index int) int {
	active := m.lineStart(m.pos)
	from, to := min(index, active), max(index, active)
	distance := 0
	for i := from + 1; i <= to; i++ {
		if m.rows[i].offset == 0 {
			distance++
		}
	}
	return distance
}

func (m model) renderNumberedDiffRow(r row, index int) string {
	gutterWidth := m.rowGutterWidth(r)
	gutter := rowGutter(r, gutterWidth)
	width := m.relativeGutterWidth()
	if width > 0 {
		if gutter != "" {
			width = max(width, len(gutter)-3)
		} else {
			// Line up headers and separators with the line numbers.
			width = max(width, gutterWidth)
		}
		number := strings.Repeat(" ", width)
		if index >= 0 && r.offset == 0 {
			value := m.lineDistance(index)
			if index == m.pos && r.sourceLine > 0 {
				value = r.sourceLine
			}
			number = fmt.Sprintf("%*d", width, value)
		}
		if gutter != "" {
			gutter = number + gutter[len(gutter)-3:]
		} else {
			gutter = number + " "
		}
	}
	return m.renderDiffRowWithGutter(r, index >= 0 && index == m.pos, gutter)
}

func (m model) renderDiffRow(r row, active bool) string {
	return m.renderDiffRowWithGutter(r, active, rowGutter(r, m.rowGutterWidth(r)))
}

func (m model) rowGutterWidth(r row) int {
	if m.screen == "sequence" || m.sequenceGutters {
		if r.commitIndex >= 0 && r.commitIndex < len(m.sequence.gutterWidths) {
			return m.sequence.gutterWidths[r.commitIndex]
		}
		return 0
	}
	return m.gutterWidth
}

func rowGutter(r row, width int) string {
	if width == 0 {
		return ""
	}
	kind := r.kind
	if kind == 'd' {
		kind = '-'
	}
	if kind != '+' && kind != '-' && kind != ' ' {
		return ""
	}
	if r.offset > 0 {
		return strings.Repeat(" ", width+3)
	}
	return fmt.Sprintf("%*d %c ", width, r.sourceLine, kind)
}

func (m model) renderDiffRowWithGutter(r row, active bool, gutter string) string {
	margin := diffMargin(m.width)
	text := r.text
	syntax, inline := r.syntaxSpans(), r.inlineSpans()
	contentWidth := max(0, m.width-margin)
	if r.kind == 'f' || r.kind == 'F' {
		text = fit(text, contentWidth)
	} else if !m.wrap {
		text = sliceWidth(text, m.horizontal, max(0, contentWidth-stringWidth(gutter)))
	} else {
		text = fit(text, max(0, contentWidth-stringWidth(gutter)))
	}
	text = fit(gutter+text, contentWidth)
	query := m.activeSearch()
	color := ""
	switch r.kind {
	case '+':
		color = "\x1b[32m"
	case '-':
		color = "\x1b[31m"
	case '@':
		color = "\x1b[36m"
		if r.submodule && (strings.HasPrefix(r.text, "  > ") || strings.HasPrefix(r.text, "  < ")) {
			color = styleDim
		}
	case 'f', 'F':
		text += strings.Repeat(" ", max(0, contentWidth-stringWidth(text)))
		color = renameHeaderDark
		if m.lightTheme {
			color = renameHeaderLight
		}
	}
	if (r.kind == 'f' || r.kind == 'F') && len(inline) > 0 && strings.HasPrefix(text, gutter) {
		highlight := "\x1b[48;2;100;38;43m"
		if r.renameAdded {
			highlight = "\x1b[48;2;42;83;52m"
		}
		if m.lightTheme {
			highlight = "\x1b[48;2;239;182;182m"
			if r.renameAdded {
				highlight = "\x1b[48;2;175;220;181m"
			}
		}
		text = color + gutter + renderSyntaxWithHighlight(text[len(gutter):], nil, inline, 0, query, color, m.lightTheme, highlight) + "\x1b[0m"
		color = ""
	} else if (syntax != nil || r.kind == '+' || r.kind == '-' || r.kind == 'd') && strings.HasPrefix(text, gutter) {
		visible := text[len(gutter):]
		start := 0
		if !m.wrap {
			start = sliceStart(r.text, m.horizontal, max(0, contentWidth-stringWidth(gutter)))
		}
		background := ""
		switch r.kind {
		case '+':
			background = darkAddedBackground
			if m.lightTheme {
				background = "\x1b[48;2;221;242;224m"
			}
		case '-', 'd':
			background = darkRemovedBackground
			if m.lightTheme {
				background = "\x1b[48;2;250;225;225m"
			}
		}
		padding := ""
		if background != "" {
			padding = strings.Repeat(" ", max(0, contentWidth-stringWidth(gutter+visible)))
		}
		text = background + gutter + renderSyntax(visible, syntax, inline, start, query, background, m.lightTheme) + padding + "\x1b[0m"
		color = ""
	} else if query != "" && strings.HasPrefix(text, gutter) {
		text = gutter + highlightMatches(text[len(gutter):], query)
	}
	if color != "" {
		text = color + text + "\x1b[0m"
	}
	prefix := strings.Repeat(" ", margin)
	if active {
		prefix = styleBold + m.accent() + "▸" + strings.Repeat(" ", margin-1) + styleReset
	}
	return prefix + text
}

func fit(s string, width int) string {
	if printableASCII(s) {
		return s[:max(0, min(len(s), width))]
	}
	return runewidth.Truncate(s, width, "")
}

// printableASCII reports whether every byte of s is a printable ASCII
// character, each of which is one cell wide. Most diff text is, and skipping
// runewidth's grapheme segmentation for it dominates rendering cost.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func stringWidth(s string) int {
	if printableASCII(s) {
		return len(s)
	}
	return runewidth.StringWidth(s)
}

func sliceWidth(s string, start, width int) string {
	var b strings.Builder
	pos := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if pos >= start && pos+w <= start+width {
			b.WriteRune(r)
		}
		pos += w
		if pos >= start+width {
			break
		}
	}
	return b.String()
}

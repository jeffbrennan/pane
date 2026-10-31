package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/mattn/go-runewidth"
)

type row struct {
	path         string
	line         int
	offset       int
	target       int
	sourceLine   int
	gutter       string
	text         string
	kind         byte
	deletedLines int
	headerIndex  int
	syntax       []syntaxSpan
	inline       []syntaxSpan
	commitIndex  int
	numberWidth  int
}

type loaded struct {
	snapshot   snapshot
	err        error
	generation int
}
type tick struct{}
type completed struct {
	message       string
	commitMessage string
	action        string
	err           error
}

type recentCommitsLoaded struct {
	commits []commitInfo
	err     error
	request int
}

type model struct {
	repo                  repository
	snapshot              snapshot
	rows                  []row
	width, height         int
	pos, horizontal       int
	viewTop               int
	pending               string
	count                 int
	jumps                 []jumpLocation
	jumpIndex             int
	staged, wrap          bool
	relativeLines         bool
	lightTheme            bool
	themeAuto             bool
	themeQuery            func()
	refreshTick           bool
	loading, busy         bool
	activeAction          string
	startup               bool
	generation            int
	modal, input, message string
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
	flash                 string
	expandedDeleted       map[string]bool
	contextFiles          map[contextKey]contextFile
	collapsedFiles        map[contextKey]bool
	contextRequest        int
	contextCommit         string
	recentCommits         []commitInfo
	recentCommitRequest   int
	recentCommitLoading   bool
	recentCommitError     bool
	discard               discardRequest
	sequence              sequenceState
}

func main() {
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: pane [commit-or-range]")
		os.Exit(1)
	}
	repo, err := openRepository()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	piped := !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd())
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
	theme, colorFGBG := strings.ToLower(strings.TrimSpace(os.Getenv("PANE_THEME"))), os.Getenv("COLORFGBG")
	m.lightTheme = lightThemeFromEnv(theme, colorFGBG)
	if theme != "light" && theme != "dark" {
		if light, ok := terminalLightBackground(); ok {
			m.lightTheme = light
		}
	}
	options := []tea.ProgramOption{tea.WithAltScreen()}
	var tracking themeTracking
	if theme != "light" && theme != "dark" {
		tracking = startThemeTracking()
		if tracking.input != nil {
			m.themeAuto, m.themeQuery = true, tracking.query
			options = append(options, tea.WithInput(tracking.input))
			defer tracking.close()
		}
	}
	if piped && tracking.input == nil {
		options = append(options, tea.WithInputTTY())
	}
	program := tea.NewProgram(m, options...)
	if tracking.setHandler != nil {
		tracking.setHandler(func(light bool) { go program.Send(terminalBackgroundChanged(light)) })
	}
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initialModel(repo repository, args []string) (model, error) {
	if len(args) > 1 {
		return model{}, fmt.Errorf("usage: pane [commit-or-range]")
	}
	m := model{repo: repo, wrap: true, loading: true, startup: true}
	if len(args) == 0 {
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
	return m, nil
}

func (m model) Init() tea.Cmd {
	commands := []tea.Cmd{load(m.repo, m.generation), nextTick()}
	if m.screen == "commit" && m.history.loading && m.history.directCommit {
		repo, hash, request := m.repo, m.history.current.hash, m.history.request
		commands = append(commands, func() tea.Msg {
			changes, err := repo.commitChangesWithoutSyntax(hash)
			return commitLoaded{changes, err, request}
		})
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

func load(repo repository, generation int) tea.Cmd {
	return func() tea.Msg {
		s, err := repo.load()
		if err == nil {
			warmSyntax(s.unstaged)
			warmSyntax(s.staged)
		}
		return loaded{s, err, generation}
	}
}

func nextTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tick{} })
}

func (m model) refresh() (model, tea.Cmd) {
	if m.loading || m.busy {
		return m, nil
	}
	m.loading = true
	return m, load(m.repo, m.generation)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()
		m.rebuildCommit()
		m.revealHistorySelection()
		if m.modal == "files" {
			m = m.inDiff(func(view *model) { view.revealPickerSelection() })
		}
		return m.loadHistoryBackground()
	case tick:
		var cmd tea.Cmd
		m.refreshTick = !m.refreshTick
		if !m.refreshTick {
			m, cmd = m.refresh()
		}
		var query tea.Cmd
		if m.themeAuto && m.themeQuery != nil && !editorRunning.Load() {
			query = func() tea.Msg {
				if !editorRunning.Load() {
					m.themeQuery()
				}
				return nil
			}
		}
		return m, tea.Batch(cmd, nextTick(), query)
	case terminalBackgroundChanged:
		if m.themeAuto {
			m.lightTheme = bool(msg)
		}
	case loaded:
		m.loading = false
		if msg.generation != m.generation {
			return m.refresh()
		}
		if msg.err != nil {
			m.message = msg.err.Error()
			break
		}
		headChanged := m.snapshot.branch.available && msg.snapshot.branch.available && m.snapshot.branch.head != msg.snapshot.branch.head
		if !m.snapshot.equal(msg.snapshot) {
			m.snapshot = msg.snapshot
			m.rebuild()
		}
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
	case completed:
		m.busy = false
		m.activeAction = ""
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
	case commitLoadingDelay:
		if m.screen == "commit" && m.history.loading && msg.request == m.history.request {
			m.history.commitLoadingVisible = true
		}
	case commitLoaded:
		if m.screen != "commit" || msg.request != m.history.request {
			break
		}
		m.history.loading = false
		m.history.commitLoadingVisible = false
		if msg.err != nil {
			m.screen = "history"
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		m.history.changes = msg.changes
		m.history.rows = nil
		m.history.pos = 0
		m.rebuildCommit()
		return m, loadCommitSyntax(m.repo, m.history.current.hash, m.history.request, m.history.changes)
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
		cmd := m.nextSequenceLoad()
		return m, cmd
	case flashed:
		m.flash = msg.text
	case editorClosed:
		if msg.err != nil {
			m.flash = "Editor: " + msg.err.Error()
		}
		return m.refresh()
	case contextLoaded:
		return m.applyContext(msg), nil
	case tea.KeyMsg:
		m.flash = ""
		if m.modal != "" {
			return m.handleModal(msg)
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
			return m.refresh()
		case "d":
			return m.startDiscard()
		case "?":
			m.modal = "help"
			m.helpPos = 0
		case "H":
			return m.openHistory()
		case " ":
			if !m.busy && len(m.rows) > 0 {
				path, staged, repo := m.rows[m.pos].path, m.staged, m.repo
				for _, c := range m.snapshot.unstaged {
					if c.path == path && c.conflict {
						m.message = "Resolve conflicts outside pane"
						return m, nil
					}
				}
				m.busy = true
				m.generation++
				return m, func() tea.Msg { return completed{message: "Updated staging", err: repo.stage(path, staged)} }
			}
		case "c":
			if !m.busy {
				if len(m.snapshot.staged) == 0 && len(m.snapshot.unstaged) == 0 {
					m.message = "Nothing to commit"
					return m, nil
				}
				m.modal = "commit"
				m.input = ""
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
			m.pickerReturn = true
			m.pickerBackToHistory = false
		} else if m.modal == "" && m.pickerBackToHistory {
			if m.screen == "commit" {
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
	if key.String() == "esc" || key.String() == "ctrl+c" {
		m.modal = ""
		return m, nil
	}
	if m.modal == "commit" {
		switch key.String() {
		case "enter":
			if strings.TrimSpace(m.input) == "" {
				m.message = "Commit message cannot be empty"
				return m, nil
			}
			message := m.input
			m.modal = ""
			m.busy = true
			m.generation++
			repo := m.repo
			return m, func() tea.Msg {
				output, err := repo.commit(message)
				return completed{message: output, commitMessage: message, err: err}
			}
		case "backspace", "ctrl+h":
			m.message = ""
			runes := []rune(m.input)
			if len(runes) > 0 {
				m.input = string(runes[:len(runes)-1])
			}
		default:
			if key.Type == tea.KeyRunes {
				m.message = ""
				m.input += string(key.Runes)
			} else if key.Type == tea.KeySpace {
				m.message = ""
				m.input += " "
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
	m.rows = buildRowsExpanded(changes, m.diffContentWidth(), m.wrap, m.expandedDeleted)
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
func (m *model) move(n int) {
	top, visible, _ := m.diffViewport(max(1, m.height-2))
	wasVisible := m.pos >= top && m.pos < top+visible
	m.pos += n
	m.clamp()
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
	if m.modal == "error" || m.modal == "discard" {
		return m.confirmationView()
	}
	if m.screen == "history" {
		return m.historyView()
	}
	if m.screen == "commit" {
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
		status, hints = prompt(prefix, m.search.draft), keyHints("↵", "search", "esc", "cancel")
	case "commit":
		status, hints = "", ""
		if m.width < 24 || m.height < 9 {
			status = prompt("Commit message: ", m.input) + "  (↵ commit, esc cancel)"
		}
	case "help", "files":
		status, hints = "", ""
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

func (m model) renderNumberedDiffRow(r row, index int) string {
	width := m.relativeGutterWidth()
	if width > 0 {
		if r.gutter != "" {
			width = max(width, len(r.gutter)-3)
		} else if r.kind == 'f' {
			// Line up the header's number with the line numbers below it.
			width = max(width, r.numberWidth)
		}
		number := strings.Repeat(" ", width)
		if index >= 0 {
			value := max(index-m.pos, m.pos-index)
			if index == m.pos && r.sourceLine > 0 {
				value = r.sourceLine
			}
			number = fmt.Sprintf("%*d", width, value)
		}
		if r.gutter != "" {
			r.gutter = number + r.gutter[len(r.gutter)-3:]
		} else {
			r.gutter = number + " "
		}
	}
	return m.renderDiffRow(r, index >= 0 && index == m.pos)
}

func (m model) renderDiffRow(r row, active bool) string {
	margin := diffMargin(m.width)
	text := r.text
	contentWidth := max(0, m.width-margin)
	if r.kind == 'f' {
		text = fit(text, contentWidth)
	} else if !m.wrap {
		text = sliceWidth(text, m.horizontal, max(0, contentWidth-runewidth.StringWidth(r.gutter)))
	} else {
		text = fit(text, max(0, contentWidth-runewidth.StringWidth(r.gutter)))
	}
	text = fit(r.gutter+text, contentWidth)
	query := m.activeSearch()
	color := ""
	switch r.kind {
	case '+':
		color = "\x1b[32m"
	case '-':
		color = "\x1b[31m"
	case '@':
		color = "\x1b[36m"
	case 'f':
		text += strings.Repeat(" ", max(0, contentWidth-runewidth.StringWidth(text)))
		color = "\x1b[1;37;48;5;236m"
		if m.lightTheme {
			color = "\x1b[1;38;2;45;45;45;48;2;222;222;222m"
		}
	}
	if (r.syntax != nil || r.kind == '+' || r.kind == '-' || r.kind == 'd') && strings.HasPrefix(text, r.gutter) {
		visible := text[len(r.gutter):]
		start := 0
		if !m.wrap {
			start = sliceStart(r.text, m.horizontal, max(0, contentWidth-runewidth.StringWidth(r.gutter)))
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
			padding = strings.Repeat(" ", max(0, contentWidth-runewidth.StringWidth(r.gutter+visible)))
		}
		text = background + r.gutter + renderSyntax(visible, r.syntax, r.inline, start, query, background, m.lightTheme) + padding + "\x1b[0m"
		color = ""
	} else if query != "" && strings.HasPrefix(text, r.gutter) {
		text = r.gutter + highlightMatches(text[len(r.gutter):], query)
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

func fit(s string, width int) string { return runewidth.Truncate(s, width, "") }

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

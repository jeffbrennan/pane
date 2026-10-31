package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type historyState struct {
	commits                                             []commitInfo
	statsCache                                          map[string]commitInfo
	changes                                             []change
	rows                                                []row
	selected, offset, pos, horizontal, viewTop, request int
	filter, draft                                       string
	searching, loading, stale, more, fetching           bool
	statsFetching                                       bool
	directCommit                                        bool
	current                                             commitInfo
	expandedDeleted                                     map[string]bool
	jumps                                               []jumpLocation
	jumpIndex                                           int
}

type historyLoaded struct {
	commits     []commitInfo
	err         error
	request     int
	skip, limit int
}
type commitLoaded struct {
	changes []change
	err     error
	request int
}
type historyStatsLoaded struct {
	commits []commitInfo
	hashes  []string
	err     error
	request int
}

const (
	historyStatsLookahead = 5
	historyStatsCacheSize = 20
)

func (m *model) cacheHistoryStats() {
	cache := make(map[string]commitInfo)
	for _, c := range m.history.commits[:min(len(m.history.commits), historyStatsCacheSize)] {
		if c.statsLoaded {
			cache[c.hash] = c
		}
	}
	m.history.statsCache = cache
}

func (m model) openHistory() (tea.Model, tea.Cmd) {
	m.screen = "history"
	m.pickerReturn, m.pickerBackToHistory = false, false
	m.history.commits = nil
	m.history.filter = ""
	m.history.searching = false
	m.history.selected, m.history.offset = 0, 0
	m.pending = ""
	return m.reloadHistory()
}

func (m model) reloadHistory() (model, tea.Cmd) {
	m.history.loading = true
	m.history.stale = false
	m.history.fetching = false
	m.history.statsFetching = false
	m.history.request++
	return m, m.fetchHistory(0, max(historyPage, len(m.history.commits)))
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
		if !c.statsLoaded && c.hash != "" {
			hashes = append(hashes, c.hash)
		}
	}
	if len(hashes) == 0 {
		return m, nil
	}
	m.history.statsFetching = true
	repo, request := m.repo, m.history.request
	return m, func() tea.Msg {
		commits, err := repo.historyStats(hashes)
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
	repo, request := m.repo, m.history.request
	return func() tea.Msg {
		commits, err := repo.history(skip, limit)
		return historyLoaded{commits: commits, err: err, request: request, skip: skip, limit: limit}
	}
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
	return m, m.fetchHistory(len(m.history.commits), historyPage)
}

func (m model) historyQuery() string {
	if m.history.searching {
		return m.history.draft
	}
	return m.history.filter
}

func (m model) filteredHistory() []commitInfo {
	query := strings.ToLower(m.historyQuery())
	if query == "" {
		return m.history.commits
	}
	var matches []commitInfo
	for _, c := range m.history.commits {
		search := c.search
		if search == "" {
			search = strings.ToLower(c.hash + "\n" + c.author + "\n" + c.email + "\n" + c.message + "\n" + c.subject)
		}
		if strings.Contains(search, query) {
			matches = append(matches, c)
		}
	}
	return matches
}

func (m model) handleHistoryKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == "commit" {
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
		case "backspace", "ctrl+h":
			runes := []rune(m.history.draft)
			if len(runes) > 0 {
				m.history.draft = string(runes[:len(runes)-1])
			}
		default:
			if key.Type == tea.KeyRunes {
				m.history.draft += string(key.Runes)
			} else if key.Type == tea.KeySpace {
				m.history.draft += " "
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
		m.screen = ""
		m.history.request++
	case "ctrl+c":
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
	case "r":
		var refreshCmd, historyCmd tea.Cmd
		m, refreshCmd = m.refresh()
		m, historyCmd = m.reloadHistory()
		return m, tea.Batch(refreshCmd, historyCmd)
	case "/":
		m.history.searching = true
		m.history.draft = m.history.filter
	case "y":
		if len(matches) > 0 {
			hash := matches[min(m.history.selected, len(matches)-1)].hash
			return m, copyCmd(hash, shortHash(hash))
		}
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
		m.history.current = matches[min(m.history.selected, len(matches)-1)]
		m.history.request++
		m.history.loading = true
		m.history.fetching = false
		m.history.statsFetching = false
		m.history.changes, m.history.rows = nil, nil
		m.history.expandedDeleted = nil
		m.history.jumps = nil
		m.history.jumpIndex = 0
		m.pickerReturn, m.pickerBackToHistory = false, false
		m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
		m.screen = "commit"
		repo, hash, request := m.repo, m.history.current.hash, m.history.request
		return m, func() tea.Msg { changes, err := repo.commitChanges(hash); return commitLoaded{changes, err, request} }
	}
	return m, nil
}

// inDiff applies f to the diff currently on screen: the working tree, or the
// commit being viewed, whose diff state lives in m.history.
func (m model) inDiff(f func(*model)) model {
	if m.screen != "commit" {
		f(&m)
		return m
	}
	view := m.commitModel()
	f(&view)
	m.history.rows, m.history.pos, m.history.horizontal, m.history.viewTop = view.rows, view.pos, view.horizontal, view.viewTop
	m.history.expandedDeleted = view.expandedDeleted
	m.pending, m.count, m.modal, m.picker, m.search, m.flash = view.pending, view.count, view.modal, view.picker, view.search, view.flash
	m.history.jumps, m.history.jumpIndex = view.jumps, view.jumpIndex
	return m
}

func (m model) handleCommitKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pending == "" && (key.String() == "h" || key.String() == "l") && m.commitModel().onDiffFile() {
		return m.changeContext(key.String())
	}
	handled := false
	m = m.inDiff(func(view *model) {
		handled = view.handleDiffNavigation(key.String()) || view.handleDiffTool(key.String())
	})
	if handled {
		return m, nil
	}
	view := m.commitModel()
	switch key.String() {
	case "o":
		opened, cmd := view.openEditor()
		m.flash = opened.flash
		return m, cmd
	case "y":
		return m, copyCmd(m.history.current.hash, shortHash(m.history.current.hash))
	}
	if key.String() == "esc" || key.String() == "q" {
		if m.pickerReturn {
			m.restorePicker()
			return m, nil
		}
		return m.returnToHistory()
	}
	if key.String() == "ctrl+c" {
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
	}
	if m.wrap != view.wrap {
		m.wrap = view.wrap
		m.rebuild()
	}
	m.history.rows, m.history.pos, m.history.horizontal, m.history.viewTop = view.rows, view.pos, view.horizontal, view.viewTop
	return m, nil
}

func (m model) returnToHistory() (tea.Model, tea.Cmd) {
	if m.history.directCommit {
		m.history.directCommit = false
		return m.openHistory()
	}
	m.screen = "history"
	m.pickerReturn, m.pickerBackToHistory = false, false
	m.history.request++
	m.history.loading = false
	m.history.fetching = false
	if m.history.stale {
		return m.reloadHistory()
	}
	return m.loadHistoryBackground()
}

func (m model) commitModel() model {
	view := m
	view.screen = ""
	view.contextCommit = m.history.current.hash
	view.jumps, view.jumpIndex = m.history.jumps, m.history.jumpIndex
	view.snapshot = snapshot{unstaged: m.history.changes}
	view.expandedDeleted = m.history.expandedDeleted
	view.staged = false
	view.rows, view.pos, view.horizontal, view.viewTop = m.history.rows, m.history.pos, m.history.horizontal, m.history.viewTop
	view.viewTitle = " pane  COMMIT " + shortHash(m.history.current.hash) + "  " + clean(m.history.current.subject)
	view.message = "Read-only · esc/q back"
	if m.history.loading {
		view.message = "Loading commit…"
	}
	return view
}

func (m *model) rebuildCommit() {
	if m.screen != "commit" {
		return
	}
	view := m.commitModel()
	view.rebuild()
	m.history.rows, m.history.pos, m.history.viewTop = view.rows, view.pos, view.viewTop
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
	var content []string
	if spacing > 0 && len(matches) > 0 {
		content = append(content, "")
	}
	now := time.Now()
	for i := offset; i < len(matches); i++ {
		c := matches[i]
		marker := "  "
		style := "\x1b[2m"
		if i == selected {
			marker = "▸ "
			style = "\x1b[1m"
		}
		date := historyCommitTime(c.date, now)
		subject := fit(marker+clean(c.subject), m.width)
		meta := historyMetadata(c, date, m.width, i == selected)
		content = append(content, style+subject+"\x1b[0m", meta, "")
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
	status := ""
	if m.history.searching {
		status = "/" + clean(m.history.draft) + "_"
	} else if m.history.filter != "" {
		status = "/" + clean(m.history.filter)
	}
	if m.flash != "" {
		status = m.flash
	}
	if m.history.fetching && m.historyQuery() != "" {
		status += "  searching older commits…"
	}
	lines = append(lines, fit(status, m.width))
	if m.modal == "help" {
		for i := 1; i < len(lines)-1; i++ {
			lines[i] = ""
		}
		showHelpDialog(lines, m.width, m.helpPos)
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

func historyMetadata(c commitInfo, date string, width int, selected bool) string {
	type segment struct{ text, style string }
	green, red, cyan := "\x1b[32m", "\x1b[31m", "\x1b[36m"
	if !selected {
		green, red, cyan = "\x1b[2;32m", "\x1b[2;31m", "\x1b[2;36m"
	}
	segments := []segment{{"  ", ""}}
	if c.statsLoaded {
		segments = append(segments, segment{fmt.Sprintf("+%d", c.added), green}, segment{fmt.Sprintf(" -%d", c.deleted), red})
	} else {
		segments = append(segments, segment{"…", "\x1b[2m"})
	}
	if c.binary > 0 {
		segments = append(segments, segment{fmt.Sprintf("  %d binary", c.binary), "\x1b[2m"})
	}
	segments = append(segments,
		segment{"  " + clean(date), "\x1b[2m"},
		segment{"  " + clean(c.author), "\x1b[2m"},
		segment{"  " + shortHash(c.hash), cyan},
	)
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
		remaining -= runewidth.StringWidth(text)
	}
	return b.String()
}

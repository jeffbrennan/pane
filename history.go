package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type historyState struct {
	commits                                    []commitInfo
	changes                                    []change
	rows                                       []row
	selected, offset, pos, horizontal, request int
	filter, draft                              string
	searching, loading                         bool
	current                                    commitInfo
}

type historyLoaded struct {
	commits []commitInfo
	err     error
	request int
}
type commitLoaded struct {
	changes []change
	err     error
	request int
}

func (m model) openHistory() (tea.Model, tea.Cmd) {
	m.screen = "history"
	m.history.request++
	m.history.commits = nil
	m.history.filter = ""
	m.history.selected, m.history.offset = 0, 0
	m.history.loading = true
	repo, request := m.repo, m.history.request
	return m, func() tea.Msg { commits, err := repo.history(); return historyLoaded{commits, err, request} }
}

func (m model) filteredHistory() []commitInfo {
	query := m.history.filter
	if m.history.searching {
		query = m.history.draft
	}
	query = strings.ToLower(query)
	var matches []commitInfo
	for _, c := range m.history.commits {
		if strings.Contains(strings.ToLower(c.hash+"\n"+c.author+"\n"+c.email+"\n"+c.message+"\n"+c.subject), query) {
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
		case "esc":
			m.history.searching = false
		case "enter":
			m.history.filter = m.history.draft
			m.history.searching = false
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
	switch key.String() {
	case "esc", "q":
		m.screen = ""
		m.history.request++
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "/":
		m.history.searching = true
		m.history.draft = m.history.filter
	case "j", "down":
		m.history.selected = min(m.history.selected+1, max(0, len(matches)-1))
	case "k", "up":
		m.history.selected = max(0, m.history.selected-1)
	case "}":
		m.history.selected = min(m.history.selected+1, max(0, len(matches)-1))
	case "{":
		m.history.selected = max(0, m.history.selected-1)
	case "enter":
		if len(matches) == 0 || m.history.loading {
			break
		}
		m.history.current = matches[min(m.history.selected, len(matches)-1)]
		m.history.request++
		m.history.loading = true
		m.history.changes, m.history.rows = nil, nil
		m.history.pos, m.history.horizontal = 0, 0
		m.screen = "commit"
		repo, hash, request := m.repo, m.history.current.hash, m.history.request
		return m, func() tea.Msg { changes, err := repo.commitChanges(hash); return commitLoaded{changes, err, request} }
	}
	return m, nil
}

func (m model) handleCommitKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "esc" || key.String() == "q" {
		m.screen = "history"
		m.history.request++
		m.history.loading = false
		return m, nil
	}
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if key.String() == "?" {
		m.modal = "help"
		m.helpPos = 0
		return m, nil
	}
	view := m.commitModel()
	switch key.String() {
	case "j", "down":
		view.move(1)
	case "k", "up":
		view.move(-1)
	case "}":
		view.paragraphJump(1)
	case "{":
		view.paragraphJump(-1)
	case "J":
		view.jump(1)
	case "K":
		view.jump(-1)
	case "w":
		view.wrap = !view.wrap
		view.horizontal = 0
		view.rebuild()
	case "h":
		if !view.wrap {
			view.horizontal = max(0, view.horizontal-4)
		}
	case "l":
		if !view.wrap {
			view.horizontal += 4
		}
	}
	if m.wrap != view.wrap {
		m.wrap = view.wrap
		m.rebuild()
	}
	m.history.rows, m.history.pos, m.history.horizontal = view.rows, view.pos, view.horizontal
	return m, nil
}

func (m model) commitModel() model {
	view := m
	view.screen = ""
	view.snapshot = snapshot{unstaged: m.history.changes}
	view.staged = false
	view.rows, view.pos, view.horizontal = m.history.rows, m.history.pos, m.history.horizontal
	view.viewTitle = " diffy  COMMIT " + shortHash(m.history.current.hash) + "  " + clean(m.history.current.subject)
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
	m.history.rows, m.history.pos = view.rows, view.pos
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
	lines := []string{fit(" diffy  HISTORY", m.width)}
	available := max(0, m.height-2)
	matches := m.filteredHistory()
	selected := min(m.history.selected, max(0, len(matches)-1))
	spacing := 0
	if available >= 4 {
		spacing = 1
	}
	visible := max(1, (available-spacing)/3)
	offset := m.history.offset
	if selected < offset {
		offset = selected
	}
	if selected >= offset+visible {
		offset = selected - visible + 1
	}
	var content []string
	if spacing > 0 && len(matches) > 0 {
		content = append(content, "")
	}
	for i := offset; i < len(matches); i++ {
		c := matches[i]
		marker := "  "
		style := "\x1b[2m"
		if i == selected {
			marker = "▸ "
			style = "\x1b[1m"
		}
		date := c.date
		if len(date) >= 10 {
			date = date[:10]
		}
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
	lines = append(lines, fit(status, m.width))
	if m.modal == "help" {
		for i := 1; i <= available; i++ {
			lines[i] = ""
		}
		showHelpDialog(lines, m.width, m.helpPos)
	}
	return strings.Join(lines, "\n")
}

func historyMetadata(c commitInfo, date string, width int, selected bool) string {
	type segment struct{ text, style string }
	green, red, cyan := "\x1b[32m", "\x1b[31m", "\x1b[36m"
	if !selected {
		green, red, cyan = "\x1b[2;32m", "\x1b[2;31m", "\x1b[2;36m"
	}
	segments := []segment{
		{"  ", ""},
		{fmt.Sprintf("+%d", c.added), green},
		{fmt.Sprintf(" -%d", c.deleted), red},
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

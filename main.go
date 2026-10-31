package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type row struct {
	path   string
	line   int
	gutter string
	text   string
	kind   byte
}

type loaded struct {
	snapshot   snapshot
	err        error
	generation int
}
type tick struct{}
type completed struct {
	message string
	err     error
}

type model struct {
	repo                  repository
	snapshot              snapshot
	rows                  []row
	width, height         int
	pos, horizontal       int
	staged, wrap          bool
	loading, busy         bool
	generation            int
	modal, input, message string
	errorText             string
	errorPos              int
	helpPos               int
	screen, viewTitle     string
	history               historyState
}

func main() {
	repo, err := openRepository()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := model{repo: repo, wrap: true, loading: true}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (m model) Init() tea.Cmd { return tea.Batch(load(m.repo, m.generation), nextTick()) }

func load(repo repository, generation int) tea.Cmd {
	return func() tea.Msg { s, err := repo.load(); return loaded{s, err, generation} }
}

func nextTick() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return tick{} })
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
	case tick:
		var cmd tea.Cmd
		m, cmd = m.refresh()
		return m, tea.Batch(cmd, nextTick())
	case loaded:
		m.loading = false
		if msg.generation != m.generation {
			return m.refresh()
		}
		if msg.err != nil {
			m.message = msg.err.Error()
			break
		}
		m.snapshot = msg.snapshot
		m.rebuild()
	case completed:
		m.busy = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
		} else {
			m.message = msg.message
		}
		return m.refresh()
	case historyLoaded:
		if m.screen != "history" || msg.request != m.history.request {
			break
		}
		m.history.loading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.errorPos = 0
			m.modal = "error"
			break
		}
		m.history.commits = msg.commits
		m.history.selected = 0
	case commitLoaded:
		if m.screen != "commit" || msg.request != m.history.request {
			break
		}
		m.history.loading = false
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
	case tea.KeyMsg:
		if m.modal != "" {
			return m.handleModal(msg)
		}
		if m.screen != "" {
			return m.handleHistoryKey(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "J":
			m.jump(1)
		case "K":
			m.jump(-1)
		case "tab":
			m.staged = !m.staged
			m.pos = 0
			m.rows = nil
			m.rebuild()
		case "w":
			m.wrap = !m.wrap
			m.horizontal = 0
			m.rebuild()
		case "h":
			if !m.wrap && m.horizontal > 0 {
				m.horizontal -= min(4, m.horizontal)
			}
		case "l":
			if !m.wrap {
				m.horizontal += 4
			}
		case "r":
			return m.refresh()
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
						m.message = "Resolve conflicts outside diffy"
						return m, nil
					}
				}
				m.busy = true
				m.generation++
				return m, func() tea.Msg { return completed{message: "Updated staging", err: repo.stage(path, staged)} }
			}
		case "c":
			if !m.busy {
				m.modal = "commit"
				m.input = ""
				m.message = ""
			}
		case "p":
			if !m.busy {
				m.modal = "push"
			}
		case "P":
			if !m.busy {
				m.modal = "pull"
			}
		}
	}
	return m, nil
}

func (m model) handleModal(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal == "help" {
		switch key.String() {
		case "esc", "?", "q":
			m.modal = ""
		case "j", "down":
			m.helpPos = min(m.helpPos+1, max(0, len(shortcutLines(m.width))-helpVisibleRows(m.height)))
		case "k", "up":
			m.helpPos = max(0, m.helpPos-1)
		}
		return m, nil
	}
	if m.modal == "error" {
		switch key.String() {
		case "j", "down":
			m.errorPos++
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
				return completed{message: output, err: err}
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
	if key.String() == "y" || key.String() == "enter" {
		action := m.modal
		m.modal = ""
		return m.runAction(action)
	}
	if key.String() == "n" {
		m.modal = ""
	}
	return m, nil
}

func (m model) runAction(args ...string) (tea.Model, tea.Cmd) {
	m.busy = true
	m.generation++
	repo := m.repo
	return m, func() tea.Msg { output, err := repo.action(args...); return completed{message: output, err: err} }
}

func (m *model) rebuild() {
	var anchor row
	if len(m.rows) > 0 && m.pos < len(m.rows) {
		anchor = m.rows[m.pos]
	}
	changes := m.snapshot.unstaged
	if m.staged {
		changes = m.snapshot.staged
	}
	m.rows = buildRows(changes, max(1, m.width), m.wrap)
	m.pos = 0
	if anchor.path != "" {
		best := -1
		for i, r := range m.rows {
			if r.path == anchor.path && r.line <= anchor.line {
				best = i
			}
			if r.path == anchor.path && r.line >= anchor.line {
				best = i
				break
			}
		}
		if best >= 0 {
			m.pos = best
		}
	}
	m.clamp()
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

func (m *model) clamp()     { m.pos = max(0, min(m.pos, len(m.rows)-1)) }
func (m *model) move(n int) { m.pos += n; m.clamp() }
func (m *model) jump(direction int) {
	if len(m.rows) == 0 {
		return
	}
	path := m.rows[m.pos].path
	if direction > 0 {
		for i := m.pos + 1; i < len(m.rows); i++ {
			if m.rows[i].path != path {
				m.pos = i
				return
			}
		}
	} else {
		start := m.pos
		for start > 0 && m.rows[start-1].path == path {
			start--
		}
		if start > 0 {
			path = m.rows[start-1].path
			start--
			for start > 0 && m.rows[start-1].path == path {
				start--
			}
			m.pos = start
		}
	}
}

func (m model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.modal == "error" {
		var content []string
		for _, line := range strings.Split(m.errorText, "\n") {
			content = append(content, wrapLine(clean(line), max(1, m.width))...)
		}
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
	if m.screen == "history" {
		return m.historyView()
	}
	if m.screen == "commit" {
		return m.commitView()
	}
	mode := "UNSTAGED"
	if m.staged {
		mode = "STAGED"
	}
	header := " diffy  " + mode
	if m.viewTitle != "" {
		header = m.viewTitle
	}
	if m.busy {
		header += "  working…"
	}
	var lines []string
	lines = append(lines, fit(header, m.width))
	available := max(0, m.height-2)
	for i := 0; i < available; i++ {
		index := m.pos + i
		if index >= len(m.rows) {
			if index == 0 {
				lines = append(lines, fit(" No changes", m.width))
			} else {
				lines = append(lines, "")
			}
			continue
		}
		r := m.rows[index]
		text := r.text
		if !m.wrap {
			text = sliceWidth(text, m.horizontal, max(0, m.width-runewidth.StringWidth(r.gutter)))
		} else {
			text = fit(text, max(0, m.width-runewidth.StringWidth(r.gutter)))
		}
		text = fit(r.gutter+text, m.width)
		color := ""
		switch r.kind {
		case '+':
			color = "\x1b[32m"
		case '-':
			color = "\x1b[31m"
		case '@':
			color = "\x1b[36m"
		case 'f':
			color = "\x1b[1;35m"
		}
		if color != "" {
			text = color + text + "\x1b[0m"
		}
		lines = append(lines, text)
	}
	status := m.message
	if m.modal == "commit" {
		if m.width < 24 || m.height < 9 {
			status = "Commit message: " + m.input + "_  (enter: commit, esc: cancel)"
		} else {
			status = "Nothing staged? All changes will be staged."
		}
	}
	if m.modal == "push" || m.modal == "pull" {
		status = m.modal + "? [y/enter: confirm, n/esc: cancel]"
	}
	if m.modal == "help" {
		status = "j/k scroll  ?/esc close"
	}
	lines = append(lines, fit(clean(strings.ReplaceAll(status, "\n", " | ")), m.width))
	if m.modal == "commit" || m.modal == "help" {
		for i := 1; i <= available; i++ {
			lines[i] = ""
		}
	}
	if m.modal == "commit" && m.width >= 24 && m.height >= 9 {
		showCommitDialog(lines, m.width, m.input, m.message)
	}
	if m.modal == "help" {
		showHelpDialog(lines, m.width, m.helpPos)
	}
	return strings.Join(lines, "\n")
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

package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type sequenceState struct {
	commits         []commitInfo
	changes         [][]change
	states          []sequenceLoadState
	errors          []string
	rowCache        [][]row
	cacheWidth      int
	cacheWrap       bool
	cacheShortPaths bool
	loading         bool
	request         int
	endPending      bool
}

type sequenceLoadState uint8

const (
	sequencePending sequenceLoadState = iota
	sequenceLoading
	sequenceReady
	sequenceFailed
)

type sequenceLoaded struct {
	index   int
	commit  commitInfo
	changes []change
	request int
	err     error
}

func initialSequence(repo repository, input io.Reader) (model, error) {
	scanner := bufio.NewScanner(io.LimitReader(input, 1024*1024+1))
	var commits []commitInfo
	total := 0
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > 1024*1024 {
			return model{}, fmt.Errorf("commit input exceeds 1 MiB")
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		hash := fields[0]
		if len(hash) < 4 || len(hash) > 64 || strings.Trim(hash, "0123456789abcdefABCDEF") != "" {
			return model{}, fmt.Errorf("invalid commit hash %q in input; use git log --oneline", hash)
		}
		if len(commits) >= 1000 {
			return model{}, fmt.Errorf("commit input exceeds 1000 commits")
		}
		commits = append(commits, commitInfo{hash: hash})
	}
	if err := scanner.Err(); err != nil {
		return model{}, fmt.Errorf("reading commit input: %w", err)
	}
	if len(commits) == 0 {
		return model{}, fmt.Errorf("no commits in input; use git log --oneline | pane")
	}
	state := sequenceState{commits: commits, changes: make([][]change, len(commits)), states: make([]sequenceLoadState, len(commits)), errors: make([]string, len(commits)), loading: true, request: 1}
	state.states[0] = sequenceLoading
	state.states[len(commits)-1] = sequenceLoading
	return model{
		repo: repo, wrap: true, loading: true, startup: true, screen: "sequence",
		sequence: state,
	}, nil
}

func loadSequenceCommit(repo repository, revision string, index, request int) tea.Cmd {
	return func() tea.Msg {
		commit, err := repo.resolveCommit(revision)
		if err != nil {
			return sequenceLoaded{index: index, request: request, err: err}
		}
		changes, err := repo.commitChanges(commit.hash)
		warmSyntax(changes)
		return sequenceLoaded{index: index, request: request, commit: commit, changes: changes, err: err}
	}
}

func (m *model) nextSequenceLoad() tea.Cmd {
	for i, state := range m.sequence.states {
		if state == sequencePending {
			m.sequence.states[i] = sequenceLoading
			return loadSequenceCommit(m.repo, m.sequence.commits[i].hash, i, m.sequence.request)
		}
	}
	m.sequence.loading = false
	for _, state := range m.sequence.states {
		if state == sequenceLoading {
			m.sequence.loading = true
			break
		}
	}
	return nil
}

func (m *model) rebuildSequence() {
	var anchor row
	hasAnchor := m.pos >= 0 && m.pos < len(m.rows)
	if hasAnchor {
		anchor = m.rows[m.pos]
	}
	oldPos, oldTop := m.pos, m.viewTop
	width := m.diffContentWidth()
	if len(m.sequence.rowCache) != len(m.sequence.commits) || m.sequence.cacheWidth != width || m.sequence.cacheWrap != m.wrap || m.sequence.cacheShortPaths != m.shortPaths {
		m.sequence.rowCache = make([][]row, len(m.sequence.commits))
		m.sequence.cacheWidth, m.sequence.cacheWrap = width, m.wrap
		m.sequence.cacheShortPaths = m.shortPaths
	}
	var rows []row
	for i := range m.sequence.commits {
		if len(m.sequence.states) > 0 && m.sequence.states[i] == sequenceFailed {
			rows = append(rows, row{kind: '@', text: "  " + clean(m.sequence.errors[i]), commitIndex: i})
			continue
		}
		if i >= len(m.sequence.changes) || (len(m.sequence.states) > 0 && m.sequence.states[i] != sequenceReady) {
			rows = append(rows, row{kind: '@', text: "  Loading commit…", commitIndex: i})
			continue
		}
		files := m.sequence.rowCache[i]
		if files == nil {
			expanded := make(map[string]bool, len(m.sequence.changes[i]))
			for _, c := range m.sequence.changes[i] {
				expanded[c.path] = true
			}
			files = buildRowsDisplay(m.sequence.changes[i], width, m.wrap, expanded, 0, m.shortPaths)
			if len(files) == 0 {
				files = append(files, row{kind: '@', text: "  No file changes"})
			}
			for j := range files {
				files[j].commitIndex = i
			}
			m.sequence.rowCache[i] = files
		}
		rows = append(rows, files...)
	}
	indexRowHeaders(rows)
	m.rows = rows
	m.pos = 0
	if hasAnchor {
		best := -1
		for i, r := range rows {
			if r.commitIndex != anchor.commitIndex || r.path != anchor.path || r.kind != anchor.kind {
				continue
			}
			if r.line == anchor.line && r.offset <= anchor.offset {
				best = i
			}
		}
		if best >= 0 {
			m.pos = best
		} else if anchor.kind == '@' {
			for i, r := range rows {
				if r.commitIndex == anchor.commitIndex {
					m.pos = i
					break
				}
			}
		}
		m.viewTop = max(0, oldTop+m.pos-oldPos)
	}
	if m.sequence.endPending && len(m.sequence.states) > 0 && m.sequence.states[len(m.sequence.states)-1] >= sequenceReady {
		m.pos = max(0, len(rows)-1)
		m.sequence.endPending = false
		m.viewTop = max(0, m.pos-max(1, m.height-2)+1)
	}
	m.clamp()
	m.viewTop = m.viewportStart(max(1, m.height-2))
}

func (m model) sequenceView() string {
	view := m
	view.screen = ""
	view.relativeLines = true
	view.viewTitle = " "
	if len(m.rows) > 0 && len(m.sequence.commits) > 0 {
		start := m.viewportStart(max(1, m.height-2))
		index := m.rows[start].commitIndex
		if index >= 0 && index < len(m.sequence.commits) {
			commit := m.sequence.commits[index]
			view.viewTitle = m.accent() + shortHash(commit.hash) + styleReset + "  " + clean(commit.subject)
			view.viewBadge = styleDim + fmt.Sprintf("%d/%d", index+1, len(m.sequence.commits)) + styleReset
		}
	}
	view.viewHints = keyHints("esc", "back", "?", "help")
	view.message = ""
	if m.sequence.loading {
		view.message = "Loading commits…"
	}
	return view.View()
}

func (m model) handleSequenceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "G" && m.pending == "" && m.count == 0 && len(m.sequence.states) > 0 {
		last := m.sequence.states[len(m.sequence.states)-1]
		m.sequence.endPending = last == sequencePending || last == sequenceLoading
	} else if key.String() != "G" {
		m.sequence.endPending = false
	}
	if m.handleDiffNavigation(key.String()) {
		return m, nil
	}
	switch key.String() {
	case "/":
		m.startSearch()
	case "o":
		return m.openEditor()
	case "y":
		if len(m.rows) > 0 {
			index := m.rows[m.pos].commitIndex
			if index >= 0 && index < len(m.sequence.commits) {
				hash := m.sequence.commits[index].hash
				return m, copyCmd(hash, shortHash(hash))
			}
		}
	case "w":
		m.wrap = !m.wrap
		m.horizontal = 0
		m.flash = wrapFlash(m.wrap)
		m.rebuildSequence()
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "f":
		m.openPicker()
	case "e":
		m.flash = "Not available in multi-commit review"
	case "esc", "q":
		if m.pickerReturn {
			m.restorePicker()
			return m, nil
		}
		return m.leaveSequence()
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) leaveSequence() (tea.Model, tea.Cmd) {
	m.screen = ""
	m.rows = nil
	m.pos, m.viewTop = 0, 0
	m.rebuild()
	return m.openHistory()
}

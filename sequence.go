package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type sequenceState struct {
	commits []commitInfo
	changes [][]change
	loading bool
	request int
}

type sequenceLoaded struct {
	changes [][]change
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
		commit, err := repo.resolveCommit(hash)
		if err != nil {
			return model{}, err
		}
		commits = append(commits, commit)
	}
	if err := scanner.Err(); err != nil {
		return model{}, fmt.Errorf("reading commit input: %w", err)
	}
	if len(commits) == 0 {
		return model{}, fmt.Errorf("no commits in input; use git log --oneline | pane")
	}
	return model{
		repo: repo, wrap: true, loading: true, startup: true, screen: "sequence",
		sequence: sequenceState{commits: commits, loading: true, request: 1},
	}, nil
}

func (m *model) rebuildSequence() {
	var anchor row
	hasAnchor := m.pos >= 0 && m.pos < len(m.rows)
	if hasAnchor {
		anchor = m.rows[m.pos]
	}
	var rows []row
	for i := range m.sequence.commits {
		if i >= len(m.sequence.changes) {
			rows = append(rows, row{kind: '@', text: "  Loading commit…", commitIndex: i})
			continue
		}
		expanded := make(map[string]bool, len(m.sequence.changes[i]))
		for _, c := range m.sequence.changes[i] {
			expanded[c.path] = true
		}
		files := buildRowsExpanded(m.sequence.changes[i], max(1, m.width-diffMargin(m.width)), m.wrap, expanded)
		if len(files) == 0 {
			files = append(files, row{kind: '@', text: "  No file changes"})
		}
		for j := range files {
			files[j].commitIndex = i
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
		}
	}
	m.clamp()
	m.viewTop = m.viewportStart(max(1, m.height-2))
}

func (m model) sequenceView() string {
	view := m
	view.screen = ""
	view.viewTitle = " pane  COMMITS"
	if len(m.rows) > 0 && len(m.sequence.commits) > 0 {
		start := m.viewportStart(max(1, m.height-2))
		index := m.rows[start].commitIndex
		if index >= 0 && index < len(m.sequence.commits) {
			commit := m.sequence.commits[index]
			view.viewTitle = " pane  COMMIT " + shortHash(commit.hash) + "  " + clean(commit.subject)
		}
	}
	view.message = "Read-only · esc/q back"
	if m.sequence.loading {
		view.message = "Loading commits…"
	}
	return view.View()
}

func (m model) handleSequenceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.rebuildSequence()
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "f", "e":
		m.flash = "Not available in multi-commit review"
	case "esc", "q":
		m.screen = ""
		m.rows = nil
		m.pos, m.viewTop = 0, 0
		m.rebuild()
		return m.openHistory()
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

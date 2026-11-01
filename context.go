package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type contextFile struct {
	base, diff string
	request    int
	ranges     []contextRange
}

type contextRange struct{ start, end, anchor int }

type contextLoaded struct {
	key     git.Context
	base    string
	diff    string
	request int
	err     error
}

func (m model) onDiffFile() bool {
	return m.pos >= 0 && m.pos < len(m.rows) && m.rows[m.pos].kind != 's' && m.rows[m.pos].path != ""
}

func (m model) foldContextFiles(rows []row) []row {
	if len(m.collapsedFiles) == 0 {
		// Callers rely on the identity to avoid copying rows during refresh.
		return rows
	}
	folded := make([]row, 0, len(rows))
	collapsed := false
	var commit uint16
	for _, r := range rows {
		if r.commitIndex != commit {
			commit, collapsed = r.commitIndex, false
		}
		if r.kind == 'f' {
			collapsed = m.collapsedFiles[git.Context{Path: r.path, Commit: m.contextCommit, Staged: m.staged, Base: m.contextBase}]
		}
		if !collapsed || r.kind == 'f' || r.kind == 'F' || r.kind == 's' || r.kind == 'm' || r.kind == 'M' || r.kind == 'n' {
			folded = append(folded, r)
		}
	}
	indexRowHeaders(folded)
	return folded
}

func (m model) localContextRows(rows []row, changes []git.Change) []row {
	if len(m.contextFiles) == 0 {
		return rows
	}
	byPath := make(map[string]git.Change, len(changes))
	for _, c := range changes {
		byPath[c.Path] = c
	}
	var result []row
	for start := 0; start < len(rows); {
		if rows[start].kind != 'f' {
			result = append(result, rows[start])
			start++
			continue
		}
		end := start + 1
		for end < len(rows) && rows[end].kind != 'f' && rows[end].kind != 's' {
			end++
		}
		path := rows[start].path
		c := byPath[path]
		file, ok := m.contextFiles[git.Context{Path: path, Commit: m.contextCommit, Staged: m.staged, Base: m.contextBase}]
		if !ok || file.base != c.Diff || file.diff == "" || len(file.ranges) == 0 {
			result = append(result, rows[start:end]...)
			start = end
			continue
		}
		visible := make(map[int]bool)
		for _, r := range rows[start:end] {
			if r.kind == ' ' {
				visible[r.target] = true
			}
		}
		c.Diff = file.diff
		full, _ := buildRowsDisplay([]git.Change{c}, m.diffContentWidth(), m.wrap, m.expandedDeleted, m.gutterWidth, m.shortPaths)
		var gap *row
		for _, r := range full {
			show := r.kind != ' ' || visible[r.target]
			for _, span := range file.ranges {
				show = show || (r.target >= span.start && r.target <= span.end)
			}
			if !show {
				if gap == nil {
					separator := (row{path: path, target: r.target, text: "  ···", kind: '@'}).withLineNumber(r.lineNumber())
					gap = &separator
				}
				continue
			}
			if gap != nil {
				result = append(result, *gap)
				gap = nil
			}
			result = append(result, r)
		}
		if gap != nil {
			result = append(result, *gap)
		}
		start = end
	}
	indexRowHeaders(result)
	return result
}

func (m model) changeContext(key string) (tea.Model, tea.Cmd) {
	m.count = 0
	view := m
	if m.screen == "commit" || m.screen == "base" {
		view = m.commitModel()
	}
	if !view.onDiffFile() {
		return m, nil
	}
	path := view.rows[view.pos].path
	fileKey := git.Context{Path: path, Commit: view.contextCommit, Staged: view.staged, Base: view.contextBase}
	selected := view.rows[view.pos]
	if (selected.kind == 'f' || selected.kind == 'F') && key == "h" {
		delete(m.contextFiles, fileKey)
		if m.collapsedFiles == nil {
			m.collapsedFiles = make(map[git.Context]bool)
		}
		m.collapsedFiles[fileKey] = true
		m = m.inDiff(func(view *model) {
			if view.rows[view.pos].kind != 'f' && view.rows[view.pos].kind != 'F' {
				view.pos = max(0, view.rows[view.pos].headerRow()-1)
			}
		})
		return m.rebuildContext(), nil
	}
	if (selected.kind == 'f' || selected.kind == 'F') && m.collapsedFiles[fileKey] {
		delete(m.collapsedFiles, fileKey)
		return m.rebuildContext(), nil
	}
	if selected.kind == 'f' || selected.kind == 'F' {
		return m, nil
	}
	if key == "h" {
		file, ok := m.contextFiles[fileKey]
		if !ok || len(file.ranges) == 0 {
			return m, nil
		}
		nearest, distance := len(file.ranges)-1, int(^uint(0)>>1)
		for i := len(file.ranges) - 1; i >= 0; i-- {
			d := max(file.ranges[i].start-selected.target, selected.target-file.ranges[i].end, 0)
			if d < distance {
				nearest, distance = i, d
			}
		}
		if distance > 10 {
			return m, nil
		}
		file.ranges = append(file.ranges[:nearest], file.ranges[nearest+1:]...)
		m.contextFiles[fileKey] = file
		return m.rebuildContext(), nil
	}
	changes := view.snapshot.Unstaged
	if view.staged {
		changes = view.snapshot.Staged
	}
	for _, c := range changes {
		if c.Path != path {
			continue
		}
		if strings.Contains(c.Diff, "\ndeleted file mode ") && !strings.Contains(c.Diff, "\nnew file mode ") {
			m = m.inDiff(func(view *model) {
				if view.expandedDeleted == nil {
					view.expandedDeleted = make(map[string]bool)
				}
				view.expandedDeleted[path] = true
			})
			m = m.rebuildContext()
		}
		current, ok := m.contextFiles[fileKey]
		if !ok || current.base != c.Diff {
			current = contextFile{base: c.Diff}
		}
		span := contextRange{start: max(1, selected.target-10), end: selected.target + 10, anchor: selected.target}
		if selected.kind == '@' && strings.TrimSpace(selected.text) == "···" {
			start := selected.target
			if view.pos > 0 && view.rows[view.pos-1].path == path && view.rows[view.pos-1].target > 0 {
				start = view.rows[view.pos-1].target + 1
			}
			span = contextRange{start: start, end: start + 9, anchor: start}
		} else {
			for _, previous := range current.ranges {
				if previous.anchor == selected.target {
					span.start = max(1, min(span.start, previous.start-10))
					span.end = max(span.end, previous.end+10)
				}
			}
		}
		current.ranges = append(current.ranges, span)
		if m.contextFiles == nil {
			m.contextFiles = make(map[git.Context]contextFile)
		}
		if current.diff != "" || current.request != 0 {
			m.contextFiles[fileKey] = current
			return m.rebuildContext(), nil
		}
		m.contextRequest++
		request := m.contextRequest
		current.request = request
		m.contextFiles[fileKey] = current
		m.flash = "Loading file context…"
		repo := m.repo
		return m, func() tea.Msg {
			diff, err := repo.FullContext(fileKey, c.Diff, c.OldPath)
			return contextLoaded{key: fileKey, base: c.Diff, diff: diff, request: request, err: err}
		}
	}
	return m, nil
}

func (m model) applyContext(msg contextLoaded) model {
	file, ok := m.contextFiles[msg.key]
	if !ok || file.request != msg.request || file.base != msg.base {
		return m
	}
	if msg.err != nil || msg.diff == "" {
		delete(m.contextFiles, msg.key)
		m.flash = "Could not load file context"
		if msg.err != nil {
			m.flash += ": " + msg.err.Error()
		}
		return m
	}
	file.diff = msg.diff
	file.request = 0
	m.contextFiles[msg.key] = file
	m.flash = ""
	if (m.screen == "base" && msg.key.Base && m.base.hash == msg.key.Commit) || (m.screen == "commit" && !msg.key.Base && m.history.current.Hash == msg.key.Commit) || (m.screen == "" && msg.key.Commit == "" && m.staged == msg.key.Staged) {
		return m.rebuildContext()
	}
	return m
}

func (m model) rebuildContext() model {
	return m.inDiff(func(view *model) {
		if len(view.rows) == 0 {
			return
		}
		anchor := view.rows[view.pos]
		offset := view.pos - view.viewTop
		gapStart := 0
		if anchor.kind == '@' && strings.TrimSpace(anchor.text) == "···" && view.pos > 0 {
			previous := view.rows[view.pos-1]
			if previous.path == anchor.path {
				gapStart = previous.target + 1
			}
		}
		view.rebuild()
		if gapStart > 0 {
			for i, r := range view.rows {
				if r.path == anchor.path && r.kind == '@' && strings.TrimSpace(r.text) == "···" && r.target >= gapStart && r.target <= anchor.target {
					view.pos = i
					view.viewTop = max(0, i-offset)
					view.reveal()
					return
				}
			}
		}
		for i, r := range view.rows {
			if r.path == anchor.path && r.kind == anchor.kind && r.target == anchor.target && r.text == anchor.text && r.offset == anchor.offset {
				view.pos = i
				view.viewTop = max(0, i-offset)
				view.reveal()
				break
			}
		}
	})
}

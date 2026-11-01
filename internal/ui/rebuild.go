package ui

import (
	"strings"

	"github.com/jeffbrennan/pane/internal/git"
)

func (m *model) rebuild() {
	if m.screen == "sequence" {
		m.rebuildSequence()
		return
	}
	m.rebuildRows(nil)
	m.previousChanges = nil
}

func (m *model) refreshRows() {
	if m.screen == "sequence" {
		m.rebuildSequence()
		return
	}
	m.collapseGenerated(m.currentChanges())
	byPath := make(map[string][]row)
	for start := 0; start < len(m.rows); {
		begin := start
		if m.rows[start].kind == 's' {
			begin++
		}
		if begin >= len(m.rows) {
			break
		}
		path := m.rows[begin].path
		end := begin + 1
		for end < len(m.rows) && m.rows[end].path == path && m.rows[end].kind != 's' {
			end++
		}
		byPath[path] = m.rows[start:end]
		start = end
	}
	m.rebuildRows(func(path string) ([]row, bool) { rows, ok := byPath[path]; return rows, ok })
	m.previousChanges = nil
}

func (m *model) rebuildRows(previous func(path string) ([]row, bool)) {
	var anchor row
	if len(m.rows) > 0 && m.pos < len(m.rows) {
		anchor = m.rows[m.pos]
	}
	changes := m.snapshot.Unstaged
	if m.staged {
		changes = m.snapshot.Staged
	}
	changes = m.applySyntaxDemand(git.Context{Commit: m.contextCommit, Staged: m.staged, Base: m.contextBase}, changes)
	m.collapseGenerated(changes)
	m.seedViewedCollapse(changes)
	var rows []row
	gutterWidth := m.gutterFloor
	if previous != nil {
		gutterWidth = m.displayGutterWidth(changes)
	}
	if previous != nil && gutterWidth == m.gutterWidth {
		rows = spliceRows(changes, *m, previous, gutterWidth)
	} else {
		rows, gutterWidth = buildRowsDisplayReuse(changes, m.diffContentWidth(), m.wrap, m.expandedDeleted, m.gutterFloor, m.shortPaths, m.rows)
	}
	m.gutterWidth = gutterWidth
	m.previousChanges = nil
	m.applyViewed(rows, changes)
	rows = m.localContextRows(rows, changes)
	rows = m.foldContextFiles(rows)
	if m.contextCommit != "" && !m.contextBase {
		rows = m.commitHeaderRows(rows)
	}
	indexRowHeaders(rows)
	m.rows = rows
	m.pos = 0
	if anchor.path != "" {
		best := -1
		for i, r := range rows {
			if r.path != anchor.path {
				continue
			}
			if r.lineNumber() < anchor.lineNumber() || (r.lineNumber() == anchor.lineNumber() && r.offset <= anchor.offset) {
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

func (m *model) reflowWidth(oldContentWidth int) {
	if m.screen == "sequence" {
		m.rebuildSequence()
		return
	}
	if m.screen == "commit" || m.screen == "base" {
		view := m.commitModel()
		view.reflowRows(oldContentWidth)
		m.history.rows, m.history.pos, m.history.viewTop = view.rows, view.pos, view.viewTop
		m.history.gutterWidth = view.gutterWidth
		return
	}
	m.reflowRows(oldContentWidth)
}

// reflowRows rebuilds only files whose wrapping changes at the new width.
func (m *model) reflowRows(oldContentWidth int) {
	var anchor row
	if len(m.rows) > 0 && m.pos < len(m.rows) {
		anchor = m.rows[m.pos]
	}
	changes := m.snapshot.Unstaged
	if m.staged {
		changes = m.snapshot.Staged
	}
	changes = m.applySyntaxDemand(git.Context{Commit: m.contextCommit, Staged: m.staged, Base: m.contextBase}, changes)
	byPath := make(map[string][]row, len(changes))
	for start := 0; start < len(m.rows); {
		begin := start
		if m.rows[start].kind == 's' {
			begin++
		}
		if begin >= len(m.rows) {
			break
		}
		path := m.rows[begin].path
		end := begin + 1
		for end < len(m.rows) && m.rows[end].path == path && m.rows[end].kind != 's' {
			end++
		}
		byPath[path] = m.rows[start:end]
		start = end
	}
	width := m.diffContentWidth()
	plans := make([]filePlan, len(changes))
	parallelEach(len(changes), func(i int) {
		part, ok := byPath[changes[i].Path]
		if oldContentWidth > 0 && ok && !fileNeedsReflow(part, width, m.gutterWidth, m.wrap) {
			plans[i].part = part
			return
		}
		if oldContentWidth > 0 && ok {
			if splices, usable := planReflow(part, width, m.gutterWidth, m.wrap); usable {
				plans[i].part, plans[i].splices = part, splices
				return
			}
		}
		plans[i].rebuild = true
	})
	type rebuild struct {
		index  int
		change git.Change
	}
	var rebuilds []rebuild
	for i := range changes {
		if plans[i].rebuild {
			rebuilds = append(rebuilds, rebuild{i, changes[i]})
		}
	}
	parallelEach(len(rebuilds), func(i int) {
		r := rebuilds[i]
		plans[r.index].part = changeRows(r.change, r.index > 0, width, m.wrap, m.expandedDeleted[r.change.Path], m.gutterWidth, m.shortPaths)
	})
	rows := assembleRowPlans(plans, m.rows)
	if len(rows) > 0 && rows[0].kind == 's' {
		rows = rows[1:]
	}
	rows = m.localContextRows(rows, changes)
	rows = m.foldContextFiles(rows)
	indexRowHeaders(rows)
	m.rows = rows
	m.pos = 0
	if anchor.path != "" {
		best := -1
		for i, r := range rows {
			if r.path != anchor.path {
				continue
			}
			if r.lineNumber() < anchor.lineNumber() || (r.lineNumber() == anchor.lineNumber() && r.offset <= anchor.offset) {
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

func fileNeedsReflow(rows []row, contentWidth, gutterWidth int, wrap bool) bool {
	// Every UTF-8 sequence is at least as wide in bytes as it is in cells, so a
	// row shorter than the limit cannot overflow and needs no width pass.
	limit := contentWidth - gutterWidth - 3
	for _, r := range rows {
		if r.offset > 0 {
			return true
		}
		switch r.kind {
		case 'f', 'F':
			if len(r.text) <= contentWidth-3 {
				continue
			}
			if stringWidth(r.text)+3 > contentWidth {
				return true
			}
		case ' ', '+', '-', 'd':
			if !wrap {
				continue
			}
			if len(r.text) <= limit {
				continue
			}
			if stringWidth(r.text) > limit {
				return true
			}
		}
	}
	return false
}

// fileRows returns a file's rows plus the spacer that preceded it.
func fileRows(rows []row, path string) ([]row, bool) {
	for start := 0; start < len(rows); start++ {
		begin := start
		if rows[start].kind == 's' {
			if start+1 >= len(rows) || rows[start+1].path != path {
				continue
			}
			start++
		}
		if rows[start].path != path || (rows[start].kind != 'f' && rows[start].kind != 'F') {
			continue
		}
		end := start + 1
		for end < len(rows) && rows[end].path == path && rows[end].kind != 's' {
			end++
		}
		return rows[begin:end], true
	}
	return nil, false
}

func (m model) displayGutterWidth(changes []git.Change) int {
	widths := make([]int, len(changes))
	parallelEach(len(changes), func(i int) {
		widths[i] = lineNumberWidth(strings.Split(changes[i].Diff, "\n"))
	})
	width := m.gutterFloor
	for _, candidate := range widths {
		width = max(width, candidate)
	}
	return width
}

// spliceRows builds rows only for files whose diff changed and keeps the
// previous rows for the rest. Context and collapse are view state, not change
// state, so those views retain rows when their display diff is unchanged.
func spliceRows(changes []git.Change, m model, previous func(path string) ([]row, bool), gutterWidth int) []row {
	view := m
	if m.screen == "commit" || m.screen == "base" {
		view = m.commitModel()
	}
	oldChanges := view.previousChanges
	keep := func(i int) bool {
		if oldChanges == nil || i >= len(oldChanges) || changes[i].OldPath != oldChanges[i].OldPath || changes[i].Path != oldChanges[i].Path {
			return false
		}
		fileKey := git.Context{Path: changes[i].Path, Commit: view.contextCommit, Staged: view.staged, Base: view.contextBase}
		if view.collapsedFiles[fileKey] {
			return false
		}
		if current, ok := view.contextFiles[fileKey]; ok && current.base == changes[i].Diff && current.diff != "" && len(current.ranges) > 0 {
			return false
		}
		return changes[i].Diff == oldChanges[i].Diff
	}
	parts := make([][]row, len(changes))
	type build struct{ index int }
	var builds []build
	for i := range changes {
		if keep(i) {
			parts[i], _ = previous(changes[i].Path)
		} else {
			builds = append(builds, build{i})
		}
	}
	parallelEach(len(builds), func(i int) {
		index := builds[i].index
		parts[index] = changeRows(changes[index], index > 0, view.diffContentWidth(), view.wrap, view.expandedDeleted[changes[index].Path], gutterWidth, view.shortPaths)
	})
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	next := make([]row, 0, total)
	for _, part := range parts {
		next = append(next, part...)
	}
	// The first file of a diff never has a leading spacer.
	if len(next) > 0 && next[0].kind == 's' {
		next = next[1:]
	}
	return next
}

func (m *model) toggleDeletedFile() {
	if len(m.rows) == 0 || m.pos >= len(m.rows) {
		return
	}
	path := m.rows[m.pos].path
	changes := m.snapshot.Unstaged
	if m.staged {
		changes = m.snapshot.Staged
	}
	for _, c := range changes {
		if c.Path != path || !strings.Contains(c.Diff, "\ndeleted file mode ") || strings.Contains(c.Diff, "\nnew file mode ") {
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

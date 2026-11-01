package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// syntaxDemand highlights files that the syntax budget left plain once they
// scroll into view. Results are kept here rather than in the changes, which
// refreshes replace, and apply while their file's diff is unchanged. Model
// copies share it.
type syntaxDemand struct {
	loaded  map[contextKey]demandSyntax
	order   []contextKey
	pending map[contextKey]bool
}

type demandSyntax struct{ diff, syntaxDiff string }

// syntaxDemandSize bounds the full-context diffs kept. A file evicted while
// on screen keeps its highlighted rows; it loads again if a rebuild drops them.
const syntaxDemandSize = 128

type syntaxDemandLoaded struct {
	results []demandResult
}

type demandResult struct {
	key              contextKey
	commitIndex      int
	diff, syntaxDiff string
	cancelled        bool
}

func (d *syntaxDemand) store(key contextKey, entry demandSyntax) {
	if _, ok := d.loaded[key]; !ok {
		d.order = append(d.order, key)
	}
	d.loaded[key] = entry
	for len(d.order) > syntaxDemandSize {
		delete(d.loaded, d.order[0])
		d.order = d.order[1:]
	}
}

// applySyntaxDemand returns changes with loaded syntax filled in for the
// skipped files of the view identified by key. It copies changes only when
// one is filled.
func (m model) applySyntaxDemand(key contextKey, changes []change) []change {
	if m.syntaxDemand == nil || len(m.syntaxDemand.loaded) == 0 {
		return changes
	}
	copied := false
	for i, c := range changes {
		if !c.syntaxSkipped || c.syntaxDiff != "" {
			continue
		}
		key.path = c.path
		entry, ok := m.syntaxDemand.loaded[key]
		if !ok || entry.diff != c.diff || entry.syntaxDiff == "" {
			continue
		}
		if !copied {
			changes = append([]change(nil), changes...)
			copied = true
		}
		changes[i].syntaxDiff = entry.syntaxDiff
	}
	return changes
}

// diffSyntaxView returns the rows and viewport top of the screen's diff, and
// whether the screen shows one.
func (m model) diffSyntaxView() ([]row, int, bool) {
	switch m.screen {
	case "commit", "base":
		return m.history.rows, m.history.viewTop, true
	case "", "sequence":
		return m.rows, m.viewTop, true
	}
	return nil, 0, false
}

// syntaxContext returns the context key, without a path, and the changes
// behind rows of commitIndex.
func (m model) syntaxContext(commitIndex int) (contextKey, []change) {
	switch m.screen {
	case "commit":
		return contextKey{commit: m.history.current.hash}, m.history.changes
	case "base":
		return contextKey{commit: m.base.hash, base: true}, m.history.changes
	case "sequence":
		if commitIndex < 0 || commitIndex >= len(m.sequence.changes) || commitIndex >= len(m.sequence.commits) {
			return contextKey{}, nil
		}
		return contextKey{commit: m.sequence.commits[commitIndex].hash}, m.sequence.changes[commitIndex]
	}
	if m.staged {
		return contextKey{staged: true}, m.snapshot.staged
	}
	return contextKey{}, m.snapshot.unstaged
}

// syntaxLookahead is how many files past the screen highlight ahead of time,
// so scrolling or J onto them shows color at once.
const syntaxLookahead = 3

type syntaxTarget struct {
	key         contextKey
	commitIndex int
	change      change
}

// loadVisibleSyntax starts highlighting the skipped files on screen and the
// few after it. The screen's files load separately so they show sooner.
func (m *model) loadVisibleSyntax() tea.Cmd {
	rows, top, ok := m.diffSyntaxView()
	if !ok || top < 0 || top >= len(rows) {
		return nil
	}
	screen := rows[top:min(len(rows), top+max(1, m.height))]
	var visible, ahead []syntaxTarget
	seen := make(map[contextKey]bool)
	for _, r := range screen {
		visible = m.addSyntaxTarget(visible, seen, int(r.commitIndex), r.path)
	}
	last := screen[len(screen)-1]
	_, changes := m.syntaxContext(int(last.commitIndex))
	for i, c := range changes {
		if c.path == last.path {
			for _, next := range changes[i+1 : min(len(changes), i+1+syntaxLookahead)] {
				ahead = m.addSyntaxTarget(ahead, seen, int(last.commitIndex), next.path)
			}
			break
		}
	}
	return tea.Batch(m.loadSyntaxTargets(visible), m.loadSyntaxTargets(ahead))
}

// addSyntaxTarget appends path's change if it is skipped and neither loaded
// nor loading, marking it as loading.
func (m *model) addSyntaxTarget(targets []syntaxTarget, seen map[contextKey]bool, commitIndex int, path string) []syntaxTarget {
	key, changes := m.syntaxContext(commitIndex)
	key.path = path
	if path == "" || seen[key] {
		return targets
	}
	seen[key] = true
	for _, c := range changes {
		if c.path != path {
			continue
		}
		if !c.syntaxSkipped || c.syntaxDiff != "" {
			return targets
		}
		if m.syntaxDemand == nil {
			m.syntaxDemand = &syntaxDemand{loaded: make(map[contextKey]demandSyntax), pending: make(map[contextKey]bool)}
		}
		if entry, ok := m.syntaxDemand.loaded[key]; m.syntaxDemand.pending[key] || (ok && entry.diff == c.diff) {
			return targets
		}
		m.syntaxDemand.pending[key] = true
		return append(targets, syntaxTarget{key, commitIndex, c})
	}
	return targets
}

func (m model) loadSyntaxTargets(targets []syntaxTarget) tea.Cmd {
	if len(targets) == 0 {
		return nil
	}
	repo := m.repo
	if m.screen == "commit" {
		repo = m.commitRepository()
	}
	return func() tea.Msg {
		results := make([]demandResult, len(targets))
		parallelEach(len(targets), func(i int) {
			t := targets[i]
			results[i] = demandResult{key: t.key, commitIndex: t.commitIndex, diff: t.change.diff}
			diff, err := repo.fullContext(t.key, t.change.diff, t.change.oldPath)
			if repo.context != nil && repo.context.Err() != nil {
				results[i].cancelled = true
				return
			}
			if err != nil || len(diff) > 2*1024*1024 {
				return
			}
			results[i].syntaxDiff = diff
			c := t.change
			c.syntaxDiff = diff
			warmSyntax([]change{c})
		})
		return syntaxDemandLoaded{results: results}
	}
}

// applySyntaxDemandLoaded keeps loaded results and highlights their rows in
// place; syntax never changes how many rows a file takes, so this avoids a
// rebuild.
func (m *model) applySyntaxDemandLoaded(msg syntaxDemandLoaded) {
	if m.syntaxDemand == nil {
		return
	}
	for _, result := range msg.results {
		delete(m.syntaxDemand.pending, result.key)
		if result.cancelled {
			continue
		}
		m.syntaxDemand.store(result.key, demandSyntax{result.diff, result.syntaxDiff})
		if result.syntaxDiff == "" {
			continue
		}
		key, _ := m.syntaxContext(result.commitIndex)
		key.path = result.key.path
		if key != result.key {
			continue
		}
		highlight := func(rows []row) {
			highlightRows(rows, result.key.path, result.commitIndex, result.syntaxDiff, result.diff)
		}
		switch m.screen {
		case "commit", "base":
			highlight(m.history.rows)
		case "sequence":
			if result.commitIndex < len(m.sequence.rowCache) {
				highlight(m.sequence.rowCache[result.commitIndex])
			}
			highlight(m.rows)
		default:
			highlight(m.rows)
		}
	}
}

// highlightRows sets the syntax spans of path's diff rows, matching each line
// by its source line number and text as changeRows does.
func highlightRows(rows []row, path string, commitIndex int, syntaxDiff, display string) {
	oldSyntax, newSyntax := syntaxLinesFor(path, syntaxDiff, display)
	for i := 0; i < len(rows); {
		r := rows[i]
		diffLine := r.kind == ' ' || r.kind == '+' || r.kind == '-' || r.kind == 'd'
		if r.path != path || int(r.commitIndex) != commitIndex || !diffLine || r.offset != 0 {
			i++
			continue
		}
		// Wrapped continuations follow with the same diff line.
		end := i + 1
		for end < len(rows) && rows[end].path == path && int(rows[end].commitIndex) == commitIndex && rows[end].kind == r.kind && rows[end].line == r.line && rows[end].offset > 0 {
			end++
		}
		var text strings.Builder
		for _, part := range rows[i:end] {
			text.WriteString(part.text)
		}
		lineSyntax, ok := newSyntax[r.sourceLine]
		if r.kind == '-' || r.kind == 'd' {
			lineSyntax, ok = oldSyntax[r.sourceLine]
		}
		if ok && lineSyntax.text == text.String() {
			byteOffset := 0
			for j := i; j < end; j++ {
				rows[j].setSyntaxSpans(clipSyntax(lineSyntax.spans, byteOffset, byteOffset+len(rows[j].text)))
				byteOffset += len(rows[j].text)
			}
		}
		i = end
	}
}

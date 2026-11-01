package ui

import (
	"fmt"
	"github.com/jeffbrennan/pane/internal/syntax"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

type searchState struct {
	query, draft              string
	origin, originTop         int
	direction, draftDirection int
	cursor                    int
	missQuery                 string
	missRows                  []row
}

func searchMatches(text, query string) bool {
	return newSearchMatcher(query).matches(text)
}

func rowMatches(r row, query string) bool {
	return newSearchMatcher(query).rowMatches(&r)
}

type searchMatcher struct {
	needle string
	fold   bool
	ascii  bool
}

func newSearchMatcher(query string) searchMatcher {
	for _, r := range query {
		if unicode.IsUpper(r) {
			return searchMatcher{needle: query}
		}
	}
	needle := strings.ToLower(query)
	ascii := true
	for i := 0; i < len(needle); i++ {
		if needle[i] >= 0x80 {
			ascii = false
			break
		}
	}
	return searchMatcher{needle: needle, fold: true, ascii: ascii}
}

func (s searchMatcher) matches(text string) bool {
	if s.needle == "" {
		return false
	}
	if !s.fold {
		return strings.Contains(text, s.needle)
	}
	// Preserve strings.ToLower's Unicode semantics whenever either operand is non-ASCII.
	if !s.ascii {
		return strings.Contains(strings.ToLower(text), s.needle)
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 0x80 {
			return strings.Contains(strings.ToLower(text), s.needle)
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != s.needle[0] || len(text)-i < len(s.needle) {
			continue
		}
		j := 1
		for ; j < len(s.needle); j++ {
			c := text[i+j]
			if c >= 0x80 {
				return strings.Contains(strings.ToLower(text), s.needle)
			}
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != s.needle[j] {
				break
			}
		}
		if j == len(s.needle) {
			return true
		}
	}
	return false
}

func (s searchMatcher) rowMatches(r *row) bool {
	return s.matches(r.text) || (r.kind == 'f' && (s.matches(r.path) || s.matches(r.oldPath)))
}

func (m model) activeSearch() string {
	if m.modal == "search" {
		return m.search.draft
	}
	return m.search.query
}

func (m *model) startSearch() {
	m.startSearchDirection(1)
}

func (m *model) startSearchDirection(direction int) {
	m.modal = "search"
	m.search.draft = ""
	m.search.missQuery, m.search.missRows = "", nil
	m.search.cursor = 0
	m.search.draftDirection = direction
	m.search.origin, m.search.originTop = m.pos, m.viewTop
}

func (m model) searchDirection() int {
	if m.search.direction < 0 {
		return -1
	}
	return 1
}

// findMatch returns the next row matching query after from in direction,
// wrapping around the diff, and whether the search wrapped.
func (m model) findMatch(query string, from, direction int) (int, bool) {
	if query == "" || len(m.rows) == 0 {
		return -1, false
	}
	matcher := newSearchMatcher(query)
	sequential := len(m.rows)
	if len(m.rows) >= 100000 && (direction == 1 || direction == -1) && runtime.GOMAXPROCS(0) > 1 {
		sequential = 2048
	}
	for step := 1; step <= sequential; step++ {
		i := from + direction*step
		wrapped := i < 0 || i >= len(m.rows)
		i = ((i % len(m.rows)) + len(m.rows)) % len(m.rows)
		if matcher.rowMatches(&m.rows[i]) {
			return i, wrapped
		}
	}
	if sequential < len(m.rows) {
		step := parallelSearch(m.rows, matcher, from, direction, sequential+1)
		if step <= len(m.rows) {
			i := from + direction*step
			return ((i % len(m.rows)) + len(m.rows)) % len(m.rows), i < 0 || i >= len(m.rows)
		}
	}
	return -1, false
}

func parallelSearch(rows []row, matcher searchMatcher, from, direction, first int) int {
	workers := min(runtime.GOMAXPROCS(0), 8)
	remaining := len(rows) - first + 1
	chunk := (remaining + workers - 1) / workers
	var best atomic.Int64
	best.Store(int64(len(rows) + 1))
	var wg sync.WaitGroup
	for start := first; start <= len(rows); start += chunk {
		end := min(len(rows)+1, start+chunk)
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			index := from + direction*start
			index = ((index % len(rows)) + len(rows)) % len(rows)
			for step := start; step < end; step++ {
				if (step-start)%64 == 0 && int64(step) >= best.Load() {
					return
				}
				if matcher.rowMatches(&rows[index]) {
					for old := best.Load(); int64(step) < old; old = best.Load() {
						if best.CompareAndSwap(old, int64(step)) {
							break
						}
					}
					return
				}
				index += direction
				if index < 0 {
					index = len(rows) - 1
				} else if index == len(rows) {
					index = 0
				}
			}
		}(start, end)
	}
	wg.Wait()
	return int(best.Load())
}

func (m *model) showMatch(index int) {
	available := max(1, m.height-2)
	top := m.viewportStart(available)
	m.pos = index
	if index < top || index >= top+available {
		m.positionViewport("z")
	} else {
		m.reveal()
	}
}

func (m *model) updateSearchDraft() {
	m.pos, m.viewTop = min(m.search.origin, max(0, len(m.rows)-1)), m.search.originTop
	if m.search.draft == "" || len(m.rows) == 0 {
		m.search.missQuery, m.search.missRows = "", nil
		return
	}
	if m.search.missQuery != "" && strings.HasPrefix(m.search.draft, m.search.missQuery) &&
		len(m.search.missRows) == len(m.rows) && &m.search.missRows[0] == &m.rows[0] {
		return
	}
	direction := m.search.draftDirection
	if direction == 0 {
		direction = 1
	}
	if index, _ := m.findMatch(m.search.draft, m.pos-direction, direction); index >= 0 {
		m.search.missQuery, m.search.missRows = "", nil
		m.showMatch(index)
	} else {
		m.search.missQuery, m.search.missRows = m.search.draft, m.rows
	}
}

func (m *model) searchJump(direction int) {
	before := m.location()
	query := m.search.query
	if query == "" {
		m.flash = "No previous search"
		return
	}
	index, wrapped := -1, false
	if len(m.rows) > 0 {
		index, wrapped = m.findMatch(query, m.pos, direction)
	}
	if index < 0 {
		m.flash = "Pattern not found: " + query
		return
	}
	m.showMatch(index)
	m.recordJump(before)
	current, total := 0, 0
	matcher := newSearchMatcher(query)
	for i := range m.rows {
		if matcher.rowMatches(&m.rows[i]) {
			total++
			if i <= index {
				current = total
			}
		}
	}
	prefix := "/"
	if m.searchDirection() < 0 {
		prefix = "?"
	}
	m.flash = fmt.Sprintf("%s%s  [%d/%d]", prefix, query, current, total)
	if wrapped {
		m.flash += "  wrapped"
	}
}

func (m *model) handleSearchKey(key string, runes []rune) {
	switch key {
	case "esc", "ctrl+c":
		m.modal = ""
		m.search.missQuery, m.search.missRows = "", nil
		m.pos, m.viewTop = min(m.search.origin, max(0, len(m.rows)-1)), m.search.originTop
	case "enter":
		m.modal = ""
		m.search.missQuery, m.search.missRows = "", nil
		if m.search.draft != "" {
			m.search.query = m.search.draft
			m.search.direction = m.search.draftDirection
		}
		m.pos, m.viewTop = min(m.search.origin, max(0, len(m.rows)-1)), m.search.originTop
		m.searchJump(m.searchDirection())
	case "backspace", "ctrl+h":
		if m.search.draft == "" {
			m.modal = ""
			m.pos, m.viewTop = min(m.search.origin, max(0, len(m.rows)-1)), m.search.originTop
			return
		}
		m.search.draft, m.search.cursor, _ = editLine(m.search.draft, m.search.cursor, key, runes)
		m.updateSearchDraft()
	default:
		var handled bool
		m.search.draft, m.search.cursor, handled = editLine(m.search.draft, m.search.cursor, key, runes)
		if handled {
			m.updateSearchDraft()
		}
	}
}

// highlightMatches reverses the video of each query match in text, which
// must already be fitted to its display width.
func highlightMatches(text, query string) string {
	if query == "" {
		return text
	}
	folded, needle := syntax.Fold(text, query), syntax.Fold(query, query)
	if len(folded) != len(text) {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(folded, needle)
		if i < 0 {
			break
		}
		b.WriteString(text[:i])
		b.WriteString("\x1b[7m" + text[i:i+len(needle)] + "\x1b[27m")
		text, folded = text[i+len(needle):], folded[i+len(needle):]
	}
	b.WriteString(text)
	return b.String()
}

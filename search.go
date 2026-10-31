package main

import (
	"fmt"
	"strings"
	"unicode"
)

type searchState struct {
	query, draft              string
	origin, originTop         int
	direction, draftDirection int
	cursor                    int
}

// searchFold lowercases s unless the query contains an uppercase letter
// (smartcase), so "foo" matches "Foo" but "Foo" does not match "foo".
func searchFold(s, query string) string {
	for _, r := range query {
		if unicode.IsUpper(r) {
			return s
		}
	}
	return strings.ToLower(s)
}

func searchMatches(text, query string) bool {
	return newSearchMatcher(query).matches(text)
}

func rowMatches(r row, query string) bool {
	return newSearchMatcher(query).rowMatches(r)
}

type searchMatcher struct {
	needle string
	fold   bool
}

func newSearchMatcher(query string) searchMatcher {
	for _, r := range query {
		if unicode.IsUpper(r) {
			return searchMatcher{needle: query}
		}
	}
	return searchMatcher{needle: strings.ToLower(query), fold: true}
}

func (s searchMatcher) matches(text string) bool {
	if s.needle == "" {
		return false
	}
	if !s.fold {
		return strings.Contains(text, s.needle)
	}
	// Preserve strings.ToLower's Unicode semantics whenever either operand is non-ASCII.
	for i := 0; i < len(s.needle); i++ {
		if s.needle[i] >= 0x80 {
			return strings.Contains(strings.ToLower(text), s.needle)
		}
	}
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			return strings.Contains(strings.ToLower(text), s.needle)
		}
	}
	for i := 0; i+len(s.needle) <= len(text); i++ {
		j := 0
		for ; j < len(s.needle); j++ {
			c := text[i+j]
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

func (s searchMatcher) rowMatches(r row) bool {
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
	matcher := newSearchMatcher(query)
	for step := 1; step <= len(m.rows); step++ {
		i := from + direction*step
		wrapped := i < 0 || i >= len(m.rows)
		i = ((i % len(m.rows)) + len(m.rows)) % len(m.rows)
		if matcher.rowMatches(m.rows[i]) {
			return i, wrapped
		}
	}
	return -1, false
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
		return
	}
	direction := m.search.draftDirection
	if direction == 0 {
		direction = 1
	}
	if index, _ := m.findMatch(m.search.draft, m.pos-direction, direction); index >= 0 {
		m.showMatch(index)
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
	for i, r := range m.rows {
		if matcher.rowMatches(r) {
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
		m.pos, m.viewTop = min(m.search.origin, max(0, len(m.rows)-1)), m.search.originTop
	case "enter":
		m.modal = ""
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
	folded, needle := searchFold(text, query), searchFold(query, query)
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

package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// listState is the shared cursor, viewport and filter state of every list
// screen. Keeping the motions and filter editing here is what makes the same
// key do the same thing on every list.
type listState struct {
	selected, offset int
	filter, draft    string
	draftCursor      int
	filtering        bool
}

// reset puts the cursor back at the top after the visible set changes.
func (l *listState) reset() { l.selected, l.offset = 0, 0 }

// clearFilter drops the filter and the cursor position with it.
func (l *listState) clearFilter() {
	l.filter, l.draft = "", ""
	l.filtering = false
	l.reset()
}

// listViewport returns the first visible row and the number of rows for a
// list with n entries on a screen with page rows.
func listViewport(l listState, n, page int) (int, int) {
	available := max(1, page)
	selected := min(l.selected, max(0, n-1))
	offset := min(max(0, l.offset), selected)
	if selected >= offset+available {
		offset = selected - available + 1
	}
	return max(0, offset), available
}

// listRunes returns the text a key inserts.
func listRunes(key tea.KeyMsg) []rune {
	switch key.Type {
	case tea.KeyRunes:
		return key.Runes
	case tea.KeySpace:
		return []rune{' '}
	}
	return nil
}

// handleListKey applies the shared list grammar: counts, gg, G, half and full
// page motions, group jumps and filter editing. n is the number of visible
// entries and groups maps an index to the start of its group for { and };
// nil groups step five entries at a time. It reports whether it consumed the
// key, so the screen can handle its own actions.
func (m *model) handleListKey(l *listState, key string, runes []rune, n, page int, groups func(int) int) bool {
	keyString := key
	if l.filtering {
		last := max(0, n-1)
		switch keyString {
		case "esc":
			// The first escape drops the filter; the next one leaves the screen.
			l.clearFilter()
		case "enter":
			// The screen's primary action takes the key once filtering ends.
			l.filtering = false
			return false
		case "down", "ctrl+n":
			l.selected = min(l.selected+1, last)
		case "up", "ctrl+p":
			l.selected = max(0, l.selected-1)
		default:
			before := l.draft
			l.draft, l.draftCursor, _ = editLine(l.draft, l.draftCursor, keyString, runes)
			if before != l.draft {
				l.filter = l.draft
				l.reset()
			}
		}
		return true
	}
	if len(keyString) == 1 && keyString[0] >= '0' && keyString[0] <= '9' && m.pending == "" {
		m.count = min(1000000, m.count*10+int(keyString[0]-'0'))
		return true
	}
	hadCount := m.count > 0
	count := max(1, m.count)
	last := max(0, n-1)
	if m.pending != "" {
		pending := m.pending
		m.pending = ""
		m.count = 0
		switch {
		case keyString == "esc":
			return true
		case pending == "g" && keyString == "g":
			l.selected = 0
			if hadCount {
				l.selected = min(count-1, last)
			}
			return true
		case pending == "g" && (keyString == "H" || keyString == "M" || keyString == "L"):
			top, visible := listViewport(*l, n, page)
			end := min(last, top+visible-1)
			l.selected = top
			if keyString == "M" {
				l.selected = (top + end) / 2
			} else if keyString == "L" {
				l.selected = end
			}
			return true
		}
	}
	if keyString != "g" {
		m.count = 0
	}
	switch keyString {
	case "g":
		m.pending = "g"
	case "j", "down":
		l.selected = min(l.selected+count, last)
	case "k", "up":
		l.selected = max(0, l.selected-count)
	case "G", "end":
		if hadCount && keyString == "G" {
			l.selected = min(count-1, last)
		} else {
			l.selected = last
		}
	case "home":
		l.selected = 0
	case "ctrl+n":
		l.selected = min(l.selected+count, last)
	case "ctrl+p":
		l.selected = max(0, l.selected-count)
	case "ctrl+d":
		l.selected = min(l.selected+max(1, page/2)*count, last)
	case "ctrl+u":
		l.selected = max(0, l.selected-max(1, page/2)*count)
	case "ctrl+f", "pgdown":
		l.selected = min(l.selected+max(1, page)*count, last)
	case "ctrl+b", "pgup":
		l.selected = max(0, l.selected-max(1, page)*count)
	case "}":
		l.selected = listGroupJump(l.selected, n, count, groups, 1)
	case "{":
		l.selected = listGroupJump(l.selected, n, count, groups, -1)
	case "/":
		l.filtering = true
		l.draft = l.filter
		l.draftCursor = len([]rune(l.draft))
	case "esc":
		if l.filter != "" {
			l.clearFilter()
			return true
		}
		return false
	default:
		return false
	}
	l.selected = min(max(0, l.selected), last)
	return true
}

func listGroupJump(selected, n, count int, groups func(int) int, direction int) int {
	if groups == nil {
		return max(0, min(selected+direction*5*count, max(0, n-1)))
	}
	index := selected
	for ; count > 0; count-- {
		start := groups(index)
		if direction > 0 {
			next := start + 1
			for next < n && groups(next) == start {
				next++
			}
			if next >= n {
				break
			}
			index = groups(next)
		} else {
			if start == 0 {
				break
			}
			index = groups(start - 1)
		}
	}
	return max(0, min(index, max(0, n-1)))
}

// listPrefixWidth is the width of the relative number and selection marker
// in front of every list entry.
func listPrefixWidth(count int) int {
	return len(fmt.Sprintf("%d", max(1, count-1))) + 3
}

// listNameWidth sizes the name column of a table list to its longest visible
// name or header, shrinking it only as far as the fixed columns need. A
// trailing free-text column gets what is left and is cut first.
func (m model) listNameWidth(count int, names []string, fixed int) int {
	longest := 0
	for _, name := range names {
		longest = max(longest, ansi.StringWidth(syntax.Clean(name)))
	}
	room := m.width - listPrefixWidth(count) - 2 - fixed
	return max(1, min(longest, max(12, room)))
}

// padCell truncates or pads s to exactly width cells.
func padCell(s string, width int) string {
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, max(0, width-1), "") + "…"
	}
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

// listTableRow renders a list entry whose columns follow the name at a shared
// width, so values sit beside what they describe instead of at the far edge.
func (m model) listTableRow(index, selected, count int, name string, nameWidth int, columns string) string {
	return m.listRow(index, selected, count, padCell(syntax.Clean(name), nameWidth)+"  "+columns, "")
}

// listRow renders one list entry the same way on every screen: relative
// distance, selection marker, the entry text, then right-aligned columns.
func (m model) listRow(index, selected, count int, text, right string) string {
	distance := index - selected
	if distance < 0 {
		distance = -distance
	}
	numberWidth := len(fmt.Sprintf("%d", max(1, count-1)))
	marker := "  "
	if index == selected {
		marker = m.accent() + "▸ " + styleReset
	}
	left := fmt.Sprintf("%*d %s%s", numberWidth, distance, marker, syntax.Clean(text))
	line := bar(m.width, left, syntax.Clean(right))
	if index == selected {
		return "\x1b[7m" + line + strings.Repeat(" ", max(0, m.width-ansi.StringWidth(line))) + styleReset
	}
	return styleDim + line + styleReset
}

// listStatus returns the filter input or applied filter for a status line, or
// "" when the list has no filter.
func (l listState) listStatus(width int) string {
	if l.filtering {
		return "/" + renderInput(l.draft, l.draftCursor, max(1, width-1))
	}
	if l.filter != "" {
		return "/" + syntax.Clean(l.filter)
	}
	return ""
}

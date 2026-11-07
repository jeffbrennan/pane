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

// listPress resolves key for a list screen. While the list's filter is
// being edited the key goes to the filter unresolved.
func (m *model) listPress(l *listState, key tea.KeyMsg, scope string) keyPress {
	if l.filtering {
		return rawPress(key)
	}
	return m.press(key, scope)
}

// handleListKey applies the shared list grammar: counts, top and bottom,
// half and full page motions, group jumps and filter editing. n is the
// number of visible entries and groups maps an index to the start of its
// group for the group motions; nil groups step five entries at a time. It
// reports whether it consumed the key, so the screen can handle its own
// actions.
func (m *model) handleListKey(l *listState, p keyPress, n, page int, groups func(int) int) bool {
	last := max(0, n-1)
	if l.filtering {
		switch keyString := p.key.String(); keyString {
		case "esc":
			// The first escape drops the filter; the next one leaves the screen.
			l.clearFilter()
		case "enter":
			// The screen's primary action takes the key once filtering ends.
			l.filtering = false
			return false
		case "down", "ctrl+n":
			l.selected = min(l.selected+1, last)
			l.offset, _ = listViewport(*l, n, page)
		case "up", "ctrl+p":
			l.selected = max(0, l.selected-1)
			l.offset, _ = listViewport(*l, n, page)
		default:
			before := l.draft
			l.draft, l.draftCursor, _ = editLine(l.draft, l.draftCursor, keyString, listRunes(p.key))
			if before != l.draft {
				l.filter = l.draft
				l.reset()
			}
		}
		return true
	}
	count := p.count
	switch p.action {
	case actNone:
		// Counts and pending sequences resolve to no action.
		return m.pending != "" || m.count > 0
	case actTop:
		l.selected = 0
		if p.hadCount {
			l.selected = min(count-1, last)
		}
	case actScreenTop, actScreenMiddle, actScreenBottom:
		top, visible := listViewport(*l, n, page)
		end := min(last, top+visible-1)
		l.selected = top
		if p.action == actScreenMiddle {
			l.selected = (top + end) / 2
		} else if p.action == actScreenBottom {
			l.selected = end
		}
	case actDown:
		l.selected = min(l.selected+count, last)
	case actUp:
		l.selected = max(0, l.selected-count)
	case actBottom:
		if p.hadCount {
			l.selected = min(count-1, last)
		} else {
			l.selected = last
		}
	case actHalfDown:
		l.selected = min(l.selected+max(1, page/2)*count, last)
	case actHalfUp:
		l.selected = max(0, l.selected-max(1, page/2)*count)
	case actPageDown:
		l.selected = min(l.selected+max(1, page)*count, last)
	case actPageUp:
		l.selected = max(0, l.selected-max(1, page)*count)
	case actNextGroup:
		l.selected = listGroupJump(l.selected, n, count, groups, 1)
	case actPrevGroup:
		l.selected = listGroupJump(l.selected, n, count, groups, -1)
	case actSearch:
		l.filtering = true
		l.draft = l.filter
		l.draftCursor = len([]rune(l.draft))
	case actBack:
		if l.filter != "" {
			l.clearFilter()
			return true
		}
		return false
	default:
		return false
	}
	l.selected = min(max(0, l.selected), last)
	// Keep the viewport where it is so the cursor moves within it, scrolling
	// only once the cursor reaches an edge.
	l.offset, _ = listViewport(*l, n, page)
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

// truncateCell cuts s to width cells, marking the cut with an ellipsis.
func truncateCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width-1, "") + "…"
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
	return m.styledRow(index, selected, count, syntax.Clean(text), syntax.Clean(right))
}

// styledRow is listRow for text that carries its own colors; the caller
// cleans anything that came from the repository. Each reset inside the text
// restores the row's dim or selected style.
func (m model) styledRow(index, selected, count int, text, right string) string {
	rowStyle := styleDim
	if index == selected {
		rowStyle = "\x1b[7m"
	}
	text = strings.ReplaceAll(text, styleReset, styleReset+rowStyle)
	right = strings.ReplaceAll(right, styleReset, styleReset+rowStyle)
	distance := index - selected
	if distance < 0 {
		distance = -distance
	}
	numberWidth := len(fmt.Sprintf("%d", max(1, count-1)))
	marker := "  "
	if index == selected {
		marker = m.accent() + "▸ " + styleReset + rowStyle
	}
	left := fmt.Sprintf("%*d %s%s", numberWidth, distance, marker, text)
	line := bar(m.width, left, right)
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

package ui

import (
	"strings"

	"github.com/jeffbrennan/pane/internal/syntax"
)

// Changed-line blocks are paired only up to inlinePairs lines. Bounding the
// search keeps rendering independent of a huge contiguous change.
const inlinePairs = 32

// inlineSpansForRow computes changed-word spans for the visible row at index.
// Building every changed line eagerly dominated large diffs; a screen needs
// spans only for the lines it draws.
func (m model) inlineSpansForRow(index int) []syntax.Span {
	if index < 0 || index >= len(m.rows) {
		return nil
	}
	row := m.rows[index]
	if row.kind != '+' && row.kind != '-' && row.kind != 'd' {
		return nil
	}
	var oldStart, oldEnd, newStart, newEnd int
	if row.kind == '+' {
		newStart, newEnd = m.inlineLineRun(index, false)
		oldStart, oldEnd = m.inlineLinesBefore(newStart)
	} else {
		oldStart, oldEnd = m.inlineLineRun(index, true)
		newStart, newEnd = m.inlineLinesAfter(oldEnd)
	}
	// A pure addition or deletion has no counterpart to compare against, which
	// is common in large diffs. Bail out before materializing either side.
	if oldStart == oldEnd || newStart == newEnd {
		return nil
	}
	// Each side can include up to inlinePairs lines found while scanning in
	// both directions, so size the stack buffers for the combined bound.
	var oldLines, newLines [2*inlinePairs + 2]int
	var oldStarts, newStarts [2*inlinePairs + 2]int
	oldCount := m.inlineLineNumbers(oldStart, oldEnd, oldLines[:], oldStarts[:])
	newCount := m.inlineLineNumbers(newStart, newEnd, newLines[:], newStarts[:])
	if oldCount == 0 || oldCount != newCount {
		return nil
	}
	line := row.lineNumber()
	for j := 0; j < oldCount; j++ {
		if oldLines[j] != line && newLines[j] != line {
			continue
		}
		oldNext, newNext := oldEnd, newEnd
		if j+1 < oldCount {
			oldNext = oldStarts[j+1]
		}
		if j+1 < newCount {
			newNext = newStarts[j+1]
		}
		removed, added := syntax.ChangedSpans(m.inlineLineText(oldStarts[j], oldNext), m.inlineLineText(newStarts[j], newNext))
		if oldLines[j] == line {
			return clipRowSpan(m.rows, oldStart, index, removed)
		}
		return clipRowSpan(m.rows, newStart, index, added)
	}
	return nil
}

func (m model) inlineLineRun(index int, removal bool) (int, int) {
	match := func(kind byte) bool {
		if removal {
			return kind == '-' || kind == 'd'
		}
		return kind == '+'
	}
	path := m.rows[index].path
	start := index
	lines := 0
	last := -1
	for start > 0 && match(m.rows[start-1].kind) && m.rows[start-1].path == path {
		if line := m.rows[start-1].lineNumber(); line != last {
			lines++
			if lines > inlinePairs {
				return index, index
			}
			last = line
		}
		start--
	}
	end := index + 1
	last = m.rows[index].lineNumber()
	lines = 1
	for end < len(m.rows) && match(m.rows[end].kind) && m.rows[end].path == path {
		if line := m.rows[end].lineNumber(); line != last {
			lines++
			if lines > inlinePairs {
				return start, end
			}
			last = line
		}
		end++
	}
	return start, end
}

func (m model) inlineLinesBefore(index int) (int, int) {
	if index <= 0 || index > len(m.rows) {
		return index, index
	}
	if m.rows[index-1].kind != '-' && m.rows[index-1].kind != 'd' {
		return index, index
	}
	return m.inlineLineRun(index-1, true)
}

func (m model) inlineLinesAfter(index int) (int, int) {
	if index < 0 || index >= len(m.rows) || m.rows[index].kind != '+' {
		return index, index
	}
	return m.inlineLineRun(index, false)
}

// inlineLineNumbers records each distinct source line's number and the row
// where it starts. It returns the count without allocating.
func (m model) inlineLineNumbers(start, end int, lines, starts []int) int {
	count := 0
	for i := start; i < end; {
		line, kind := m.rows[i].lineNumber(), m.rows[i].kind
		lines[count], starts[count] = line, i
		count++
		i++
		for i < end && m.rows[i].lineNumber() == line && m.rows[i].kind == kind {
			i++
		}
	}
	return count
}

// inlineLineText joins the wrapped rows for one source line. Unwrapped lines
// are already one string, so the common case does not allocate.
func (m model) inlineLineText(start, end int) string {
	if end-start <= 1 {
		return m.rows[start].text
	}
	size := 0
	for i := start; i < end; i++ {
		size += len(m.rows[i].text)
	}
	var joined strings.Builder
	joined.Grow(size)
	for i := start; i < end; i++ {
		joined.WriteString(m.rows[i].text)
	}
	return joined.String()
}

func clipRowSpan(rows []row, blockStart, index int, spans []syntax.Span) []syntax.Span {
	if len(spans) == 0 {
		return nil
	}
	offset := 0
	for i := blockStart; i < index; i++ {
		if rows[i].lineNumber() == rows[index].lineNumber() && rows[i].kind == rows[index].kind {
			offset += len(rows[i].text)
		}
	}
	return syntax.Clip(spans, offset, offset+len(rows[index].text))
}

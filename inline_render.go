package main

// Changed-line blocks are paired only up to inlinePairs lines. Bounding the
// search keeps rendering independent of a huge contiguous change.
const inlinePairs = 32

type inlineLine struct {
	kind byte
	line int
	text string
}

// inlineSpansForRow computes changed-word spans for the visible row at index.
// Building every changed line eagerly dominated large diffs; a screen needs
// spans only for the lines it draws.
func (m model) inlineSpansForRow(index int) []syntaxSpan {
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
	oldLines, newLines := m.inlineLines(oldStart, oldEnd), m.inlineLines(newStart, newEnd)
	if len(oldLines) == 0 || len(oldLines) != len(newLines) {
		return nil
	}
	for j := range oldLines {
		if oldLines[j].line != row.lineNumber() && newLines[j].line != row.lineNumber() {
			continue
		}
		removed, added := changedSpans(oldLines[j].text, newLines[j].text)
		if oldLines[j].line == row.lineNumber() {
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

func (m model) inlineLines(start, end int) []inlineLine {
	var lines []inlineLine
	for i := start; i < end; {
		line := m.rows[i].lineNumber()
		kind := m.rows[i].kind
		var text []byte
		j := i
		for ; j < end && m.rows[j].lineNumber() == line && m.rows[j].kind == kind; j++ {
			text = append(text, m.rows[j].text...)
		}
		lines = append(lines, inlineLine{kind: kind, line: line, text: string(text)})
		i = j
	}
	return lines
}

func clipRowSpan(rows []row, blockStart, index int, spans []syntaxSpan) []syntaxSpan {
	if len(spans) == 0 {
		return nil
	}
	offset := 0
	for i := blockStart; i < index; i++ {
		if rows[i].lineNumber() == rows[index].lineNumber() && rows[i].kind == rows[index].kind {
			offset += len(rows[i].text)
		}
	}
	return clipSyntax(spans, offset, offset+len(rows[index].text))
}

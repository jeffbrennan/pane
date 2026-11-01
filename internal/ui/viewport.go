package ui

import (
	"github.com/jeffbrennan/pane/internal/syntax"
)

func (m model) fileHeaderAt(index int) *row {
	if index < 0 || index >= len(m.rows) || m.rows[index].kind == 'f' || m.rows[index].kind == 'F' {
		return nil
	}
	header := m.rows[index].headerRow() - 1
	if header >= 0 && header < index && m.rows[header].kind == 'f' {
		if m.rows[header].oldPath != "" {
			pinned := m.rows[header]
			pinned.renameAdded = true
			pinned.text = " → " + displayPath(pinned.path, m.shortPaths)
			_, inline := syntax.RenamePathSpans(pinned.oldPath, pinned.path, m.shortPaths)
			for i := range inline {
				inline[i].Start += len(" → ")
				inline[i].End += len(" → ")
			}
			pinned.setInlineSpans(inline)
			return &pinned
		}
		return &m.rows[header]
	}
	return nil
}

// diffViewport returns the first row of the viewport, the number of rows
// shown below a pinned file header, and that header. The header covers the
// viewport's first row rather than shrinking it, so pinning never changes
// which rows fit and scrolling stays one row per step.
func (m model) diffViewport(available int) (int, int, *row) {
	if available <= 0 {
		return 0, 0, nil
	}
	start := m.viewportStart(available)
	header := m.fileHeaderAt(start)
	if available < 2 || header == nil {
		return start, available, nil
	}
	return start, available - 1, header
}

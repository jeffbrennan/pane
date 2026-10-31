package main

func (m model) fileHeaderAt(index int) *row {
	if index < 0 || index >= len(m.rows) || m.rows[index].kind == 'f' || m.rows[index].kind == 'F' {
		return nil
	}
	header := m.rows[index].headerIndex - 1
	if header >= 0 && header < index && m.rows[header].kind == 'f' {
		if m.rows[header].oldPath != "" {
			pinned := m.rows[header]
			pinned.renameAdded = true
			pinned.text = " → " + displayPath(pinned.path, m.shortPaths)
			_, pinned.inline = renamePathSpans(pinned.oldPath, pinned.path, m.shortPaths)
			for i := range pinned.inline {
				pinned.inline[i].start += len(" → ")
				pinned.inline[i].end += len(" → ")
			}
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

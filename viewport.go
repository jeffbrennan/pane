package main

func (m model) fileHeaderAt(index int) *row {
	if index < 0 || index >= len(m.rows) || m.rows[index].kind == 'f' {
		return nil
	}
	if m.rows[index].kind == 's' {
		index--
		if index < 0 {
			return nil
		}
		if m.rows[index].kind == 'f' {
			return &m.rows[index]
		}
	}
	path := m.rows[index].path
	for i := index - 1; i >= 0 && m.rows[i].path == path; i-- {
		if m.rows[i].kind == 'f' {
			return &m.rows[i]
		}
	}
	return nil
}

func (m model) diffViewport(available int) (int, int, *row) {
	if available <= 0 {
		return 0, 0, nil
	}
	start := m.viewportStart(available)
	if available < 2 || m.fileHeaderAt(start) == nil {
		return start, available, nil
	}
	start = m.viewportStart(available - 1)
	header := m.fileHeaderAt(start)
	if header == nil {
		return start, available, nil
	}
	return start, available - 1, header
}

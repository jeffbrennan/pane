package main

func (m model) fileHeaderAt(index int) *row {
	if index < 0 || index >= len(m.rows) || m.rows[index].kind == 'f' {
		return nil
	}
	header := m.rows[index].headerIndex - 1
	if header >= 0 && header < index && m.rows[header].kind == 'f' {
		return &m.rows[header]
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

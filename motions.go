package main

import (
	"strconv"
	"strings"
)

type jumpLocation struct {
	path, text string
	line       int
	target     int
	offset     int
	kind       byte
	index, top int
}

func (m model) location() jumpLocation {
	loc := jumpLocation{index: m.pos, top: m.viewTop}
	if m.pos >= 0 && m.pos < len(m.rows) {
		r := m.rows[m.pos]
		loc.path, loc.text, loc.line, loc.target, loc.offset, loc.kind = r.path, r.text, r.line, r.target, r.offset, r.kind
	}
	return loc
}

func sameLocation(a, b jumpLocation) bool {
	if a.path == "" || b.path == "" {
		return a.path == b.path && a.index == b.index
	}
	return a.path == b.path && a.kind == b.kind && a.target == b.target && a.text == b.text && a.offset == b.offset
}

func (m *model) recordJump(before jumpLocation) {
	after := m.location()
	if sameLocation(before, after) || len(m.rows) == 0 {
		return
	}
	if len(m.jumps) == 0 {
		m.jumps = append(m.jumps, before)
		m.jumpIndex = 0
	} else {
		m.jumps = m.jumps[:m.jumpIndex+1]
		if !sameLocation(m.jumps[m.jumpIndex], before) {
			m.jumps = append(m.jumps, before)
		}
	}
	m.jumps = append(m.jumps, after)
	if len(m.jumps) > 100 {
		m.jumps = m.jumps[len(m.jumps)-100:]
	}
	m.jumpIndex = len(m.jumps) - 1
}

func (m *model) travelJump(direction int) {
	if len(m.jumps) == 0 || len(m.rows) == 0 {
		return
	}
	if direction < 0 && m.jumpIndex == len(m.jumps)-1 && !sameLocation(m.jumps[m.jumpIndex], m.location()) {
		m.jumps = append(m.jumps, m.location())
		m.jumpIndex++
	}
	next := m.jumpIndex + direction
	if next < 0 || next >= len(m.jumps) {
		return
	}
	m.jumpIndex = next
	loc := m.jumps[next]
	index := -1
	if loc.path == "" {
		index = min(loc.index, len(m.rows)-1)
	} else {
		bestDistance := int(^uint(0) >> 1)
		for i, r := range m.rows {
			if r.path == loc.path && r.kind == loc.kind && r.target == loc.target && r.text == loc.text && r.offset == loc.offset {
				index = i
				break
			}
			if r.path == loc.path && r.kind == loc.kind {
				distance := max(r.target-loc.target, loc.target-r.target)*1000 + max(r.offset-loc.offset, loc.offset-r.offset)
				if distance < bestDistance {
					index, bestDistance = i, distance
				}
			}
		}
	}
	if index >= 0 {
		m.pos = index
		m.viewTop = max(0, index-(loc.index-loc.top))
		m.reveal()
	}
}

func (m *model) sourceLineJump(number int) {
	if len(m.rows) == 0 {
		return
	}
	path := m.rows[m.pos].path
	best, distance := -1, int(^uint(0)>>1)
	for i, r := range m.rows {
		if r.path != path || r.offset != 0 {
			continue
		}
		fields := strings.Fields(r.gutter)
		if len(fields) == 0 {
			continue
		}
		line, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		d := max(line-number, number-line)
		if d < distance || (d == distance && best >= 0 && m.rows[best].kind == '-' && r.kind != '-') {
			best, distance = i, d
		}
	}
	if best >= 0 {
		m.pos = best
		m.reveal()
	}
}

func (m *model) screenJump(position string) {
	if len(m.rows) == 0 {
		return
	}
	start, visible, pinned := m.diffViewport(max(1, m.height-2))
	if pinned != nil && m.pos != start {
		start++
	} else if pinned != nil {
		visible++
	}
	end := min(len(m.rows)-1, start+visible-1)
	switch position {
	case "H":
		m.pos = min(start, end)
	case "M":
		m.pos = (start + end) / 2
	case "L":
		m.pos = end
	}
}

func changedRow(r row) bool { return r.kind == '+' || r.kind == '-' || r.kind == 'd' }

func (m *model) changeJump(direction int) {
	if len(m.rows) == 0 {
		return
	}
	i := m.pos
	if changedRow(m.rows[i]) {
		path := m.rows[i].path
		for i >= 0 && i < len(m.rows) && changedRow(m.rows[i]) && m.rows[i].path == path {
			i += direction
		}
	} else {
		i += direction
	}
	for i >= 0 && i < len(m.rows) {
		if changedRow(m.rows[i]) {
			if direction < 0 {
				path := m.rows[i].path
				for i > 0 && changedRow(m.rows[i-1]) && m.rows[i-1].path == path {
					i--
				}
			}
			m.pos = i
			m.reveal()
			return
		}
		i += direction
	}
}

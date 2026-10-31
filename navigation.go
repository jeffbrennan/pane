package main

func (m *model) reveal() {
	top, available, _ := m.diffViewport(max(1, m.height-2))
	margin := min(3, (available-1)/2)
	if m.pos < top+margin {
		top = m.pos - margin
	} else if m.pos >= top+available-margin {
		top = m.pos - available + margin + 1
	}
	m.viewTop = max(0, min(top, max(0, len(m.rows)-available)))
}

func (m *model) scrollViewport(amount int) {
	if len(m.rows) == 0 {
		return
	}
	top, available, _ := m.diffViewport(max(1, m.height-2))
	m.viewTop = max(0, min(top+amount, max(0, len(m.rows)-available)))
	m.pos = max(m.viewTop, min(m.pos, m.viewTop+available-1))
	m.clamp()
}

func (m *model) positionViewport(position string) {
	if len(m.rows) == 0 {
		return
	}
	available := max(1, m.height-2)
	top := m.pos
	switch position {
	case "z":
		top -= available / 2
	case "b":
		top -= available - 1
	}
	preview := *m
	preview.viewTop = top
	_, visible, _ := preview.diffViewport(available)
	if visible != available {
		available = visible
		top = m.pos
		switch position {
		case "z":
			top -= available / 2
		case "b":
			top -= available - 1
		}
	}
	m.viewTop = max(0, min(top, max(0, len(m.rows)-available)))
}

func (m *model) horizontalMove(amount int) {
	if !m.wrap {
		m.horizontal = max(0, m.horizontal+amount)
	}
}

func (m *model) handleDiffNavigation(key string) bool {
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && m.pending == "" {
		m.count = min(1000000, m.count*10+int(key[0]-'0'))
		return true
	}
	hadCount := m.count > 0
	count := max(1, m.count)
	before := m.location()
	jump := false
	defer func() {
		if jump {
			m.recordJump(before)
		}
	}()
	if m.pending != "" {
		pending := m.pending
		m.pending = ""
		m.count = 0
		if key == "esc" {
			return true
		}
		if pending == "g" && key == "g" {
			if hadCount {
				m.sourceLineJump(count)
			} else {
				m.move(-m.pos)
			}
			jump = true
			return true
		}
		if pending == "g" {
			switch key {
			case "H", "M", "L":
				m.screenJump(key)
				jump = true
				return true
			case "?":
				m.startSearchDirection(-1)
				return true
			case "i":
				for range min(count, len(m.jumps)) {
					m.travelJump(1)
				}
				return true
			}
		}
		if (pending == "]" || pending == "[") && key == "c" {
			direction := 1
			if pending == "[" {
				direction = -1
			}
			for range min(count, len(m.rows)) {
				m.changeJump(direction)
			}
			jump = true
			return true
		}
		if pending == "z" {
			switch key {
			case "t", "z", "b":
				m.positionViewport(key)
				return true
			case "h", "ctrl+h", "backspace":
				m.horizontalMove(-4)
				return true
			case "l":
				m.horizontalMove(4)
				return true
			case "H":
				m.horizontalMove(-max(1, m.width/2))
				return true
			case "L":
				m.horizontalMove(max(1, m.width/2))
				return true
			}
		}
	}
	_, page, _ := m.diffViewport(max(1, m.height-2))
	if key != "g" && key != "z" && key != "]" && key != "[" {
		m.count = 0
	}
	switch key {
	case "g", "z", "]", "[":
		m.pending = key
	case "G", "end":
		if hadCount && key == "G" {
			m.sourceLineJump(count)
		} else {
			m.move(len(m.rows) - 1 - m.pos)
		}
		jump = true
	case "home":
		m.move(-m.pos)
		jump = true
	case "j", "down":
		m.move(count)
	case "k", "up":
		m.move(-count)
	case "}":
		for range min(count, len(m.rows)) {
			m.paragraphJump(1)
		}
	case "{":
		for range min(count, len(m.rows)) {
			m.paragraphJump(-1)
		}
	case "J":
		for range min(count, len(m.rows)) {
			m.jump(1)
		}
		jump = true
	case "K":
		for range min(count, len(m.rows)) {
			m.jump(-1)
		}
		jump = true
	case "ctrl+o":
		for range min(count, len(m.jumps)) {
			m.travelJump(-1)
		}
	case "n", "N":
		direction := 1
		if key == "N" {
			direction = -1
		}
		for range min(count, max(1, len(m.rows))) {
			m.searchJump(direction * m.searchDirection())
		}
	case "ctrl+d":
		m.move(max(1, page/2) * count)
	case "ctrl+u":
		m.move(-max(1, page/2) * count)
	case "ctrl+f", "pgdown":
		m.move(page * count)
	case "ctrl+b", "pgup":
		m.move(-page * count)
	case "ctrl+e":
		m.scrollViewport(count)
	case "ctrl+y":
		m.scrollViewport(-count)
	case "h", "ctrl+h", "backspace":
		m.horizontalMove(-4)
	case "l":
		m.horizontalMove(4)
	default:
		return false
	}
	return true
}

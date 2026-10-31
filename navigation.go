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
	if m.pending != "" {
		pending := m.pending
		m.pending = ""
		if key == "esc" {
			return true
		}
		if pending == "g" && key == "g" {
			m.move(-m.pos)
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
	switch key {
	case "g", "z":
		m.pending = key
	case "G", "end":
		m.move(len(m.rows) - 1 - m.pos)
	case "home":
		m.move(-m.pos)
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "}":
		m.paragraphJump(1)
	case "{":
		m.paragraphJump(-1)
	case "J":
		m.jump(1)
	case "K":
		m.jump(-1)
	case "ctrl+d":
		m.move(max(1, page/2))
	case "ctrl+u":
		m.move(-max(1, page/2))
	case "ctrl+f", "pgdown":
		m.move(page)
	case "ctrl+b", "pgup":
		m.move(-page)
	case "ctrl+e":
		m.scrollViewport(1)
	case "ctrl+y":
		m.scrollViewport(-1)
	case "h", "ctrl+h", "backspace":
		m.horizontalMove(-4)
	case "l":
		m.horizontalMove(4)
	default:
		return false
	}
	return true
}

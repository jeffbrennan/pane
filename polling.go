package main

const (
	blurredRefreshMin = blurredRefreshTicks
	blurredRefreshMax = 30 // 15 s at the 500 ms tick
)

func (m model) blurredRefreshInterval() int {
	if m.blurredInterval < blurredRefreshMin {
		return blurredRefreshMin
	}
	return min(m.blurredInterval, blurredRefreshMax)
}

// backOffBlurredRefresh widens the polling interval while an unfocused pane
// keeps seeing the same status, so idle worktrees do not keep paying for
// `git status`. Any change or a focus event resets it.
func (m *model) backOffBlurredRefresh() {
	if !m.blurred {
		return
	}
	m.blurredInterval = min(m.blurredRefreshInterval()*2, blurredRefreshMax)
}

func (m *model) resetBlurredRefresh() {
	if m.blurred {
		m.blurredInterval = blurredRefreshMin
	}
}

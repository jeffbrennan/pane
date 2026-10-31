package main

import (
	"fmt"

	"github.com/mattn/go-runewidth"
)

func (m model) historyHeader() []string {
	branch := m.snapshot.branch
	lines := []string{historyTitle(branch, m.width)}
	limit := min(2, max(0, m.height-4))
	if limit == 0 {
		if action := m.actionStatus(); action != "" {
			lines[0] = fit(" pane  "+action, m.width)
		}
		return lines
	}
	type statusLine struct{ text, style string }
	var status []statusLine
	if !branch.available {
		status = append(status, statusLine{"  Loading repository status…", "\x1b[2m"})
	} else {
		syncStyle := "\x1b[2m"
		if branch.countsKnown && (branch.ahead > 0 || branch.behind > 0) {
			syncStyle = "\x1b[33m"
		}
		switch {
		case branch.upstream == "":
			status = append(status, statusLine{"  No upstream configured", syncStyle})
		case !branch.countsKnown:
			status = append(status, statusLine{"  Upstream counts unavailable", syncStyle})
		case m.width < 40:
			status = append(status, statusLine{fmt.Sprintf("  ↑%d ↓%d", branch.ahead, branch.behind), syncStyle})
		default:
			status = append(status, statusLine{fmt.Sprintf("  ↑%d ahead  ↓%d behind", branch.ahead, branch.behind), syncStyle})
		}
		changes := fmt.Sprintf("%d unstaged · %d staged", len(m.snapshot.unstaged), len(m.snapshot.staged))
		changeStyle := "\x1b[2m"
		if len(m.snapshot.unstaged) > 0 || len(m.snapshot.staged) > 0 {
			changeStyle = ""
		}
		status = append(status, statusLine{"  " + changes, changeStyle})
	}
	if action := m.actionStatus(); action != "" {
		status[0] = statusLine{"  " + action, "\x1b[33m"}
	}
	for _, line := range status[:min(limit, len(status))] {
		text := fit(line.text, m.width)
		if line.style != "" {
			text = line.style + text + "\x1b[0m"
		}
		lines = append(lines, text)
	}
	return lines
}

func (m model) actionStatus() string {
	if !m.busy {
		return ""
	}
	switch m.activeAction {
	case "push":
		branch := m.snapshot.branch
		if branch.available && branch.upstream != "" && branch.countsKnown && branch.ahead == 0 {
			return "Checking push…"
		}
		return "Pushing…"
	case "pull":
		return "Pulling…"
	default:
		return ""
	}
}

func historyTitle(branch branchStatus, width int) string {
	prefix := fit(" pane  ", width)
	if !branch.available {
		return prefix
	}
	name := branch.name
	if name == "" {
		name = "unknown"
	}
	name = fit(clean(name), max(0, width-runewidth.StringWidth(prefix)))
	result := prefix + "\x1b[1m" + name + "\x1b[0m"
	remaining := max(0, width-runewidth.StringWidth(prefix)-runewidth.StringWidth(name))
	if branch.upstream != "" && remaining > 0 {
		result += "\x1b[2m" + fit(" → "+clean(branch.upstream), remaining) + "\x1b[0m"
	}
	return result
}

// historyHeaderHeight matches len(m.historyHeader()) without rendering it.
func (m model) historyHeaderHeight() int {
	status := 2
	if !m.snapshot.branch.available {
		status = 1
	}
	return 1 + min(min(2, max(0, m.height-4)), status)
}

func (m model) historyAvailable() int {
	return max(0, m.height-m.historyHeaderHeight()-1)
}

package main

import "fmt"

func (m model) historyHeader() []string {
	lines := []string{fit(" pane  HISTORY", m.width)}
	limit := min(3, max(0, m.height-4))
	if limit == 0 {
		return lines
	}
	branch := m.snapshot.branch
	var status []string
	if !branch.available {
		status = []string{"  Loading repository status…"}
	} else {
		name := branch.name
		if name == "" {
			name = "unknown"
		}
		status = append(status, "  Branch: "+clean(name))
		switch {
		case branch.upstream == "":
			status = append(status, "  No upstream configured")
		case !branch.countsKnown:
			status = append(status, "  Upstream counts unavailable")
		case m.width < 40:
			status = append(status, fmt.Sprintf("  ↑%d ↓%d (cached)", branch.ahead, branch.behind))
		default:
			status = append(status, fmt.Sprintf("  ↑%d unpushed  ↓%d behind (cached)", branch.ahead, branch.behind))
		}
		changes := fmt.Sprintf("%d unstaged · %d staged", len(m.snapshot.unstaged), len(m.snapshot.staged))
		if branch.upstream != "" {
			changes = clean(branch.upstream) + " · " + changes
		}
		status = append(status, "  "+changes)
	}
	for _, line := range status[:min(limit, len(status))] {
		lines = append(lines, fit(line, m.width))
	}
	return lines
}

func (m model) historyAvailable() int {
	return max(0, m.height-len(m.historyHeader())-1)
}

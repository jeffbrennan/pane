package main

import (
	"fmt"
	"strings"
)

// historyHeader shows the branch on the title row and a one-line summary of
// its sync state and local changes below it.
func (m model) historyHeader() []string {
	branch := m.snapshot.branch
	title := m.historyTitle(branch, max(0, m.width-1))
	badge := ""
	if m.history.path != "" {
		title = "history · " + clean(m.history.path)
		badge = styleDim + "whole-commit counts" + styleReset
	}
	lines := []string{m.titleBar(" "+title, badge)}
	if m.height-4 <= 0 {
		return lines
	}
	if !branch.available {
		return append(lines, fit(styleDim+" Loading repository status…"+styleReset, m.width))
	}
	var parts []string
	switch {
	case branch.upstream == "":
		parts = append(parts, styleDim+"no upstream"+styleReset)
	case !branch.countsKnown:
		parts = append(parts, styleDim+"upstream unknown"+styleReset)
	case branch.ahead == 0 && branch.behind == 0:
		parts = append(parts, styleDim+"\x1b[32m✓ up to date"+styleReset)
	case m.width < 40:
		parts = append(parts, m.warning()+fmt.Sprintf("↑%d ↓%d", branch.ahead, branch.behind)+styleReset)
	default:
		if branch.ahead > 0 {
			parts = append(parts, m.warning()+fmt.Sprintf("↑%d ahead", branch.ahead)+styleReset)
		}
		if branch.behind > 0 {
			parts = append(parts, m.warning()+fmt.Sprintf("↓%d behind", branch.behind)+styleReset)
		}
	}
	count := func(n int, label string) string {
		return styleBold + fmt.Sprint(n) + styleReset + styleDim + " " + label + styleReset
	}
	unstaged, staged := len(m.snapshot.unstaged), len(m.snapshot.staged)
	if unstaged > 0 {
		parts = append(parts, count(unstaged, "unstaged"))
	}
	if staged > 0 {
		parts = append(parts, count(staged, "staged"))
	}
	if unstaged == 0 && staged == 0 {
		parts = append(parts, styleDim+"clean"+styleReset)
	}
	if m.history.stashCount > 0 {
		parts = append(parts, styleDim+fmt.Sprintf("%d stashed", m.history.stashCount)+styleReset)
	}
	return append(lines, bar(m.width, " "+strings.Join(parts, styleDim+" · "+styleReset), ""))
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
	case "fetch":
		return "Fetching…"
	case "stash-push":
		return "Stashing…"
	case "stash-pop":
		return "Popping stash…"
	case "stash-drop":
		return "Dropping stash…"
	case "amend":
		return "Checking remotes and updating message…"
	default:
		return ""
	}
}

// historyTitle shows the branch and its upstream within width columns.
func (m model) historyTitle(branch branchStatus, width int) string {
	if !branch.available || width <= 0 {
		return ""
	}
	name := branch.name
	if name == "" {
		name = "unknown"
	}
	name = fit(clean(name), width)
	result := styleBold + m.accent() + name + styleReset
	remaining := max(0, width-stringWidth(name))
	if branch.upstream != "" && remaining > 0 {
		result += styleDim + fit(" → "+clean(branch.upstream), remaining) + styleReset
	}
	return result
}

// historyHeaderHeight matches len(m.historyHeader()) without rendering it.
func (m model) historyHeaderHeight() int {
	if m.height-4 <= 0 {
		return 1
	}
	return 2
}

func (m model) historyAvailable() int {
	return max(0, m.height-m.historyHeaderHeight()-1)
}

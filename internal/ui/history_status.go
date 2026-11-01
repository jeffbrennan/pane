package ui

import (
	"fmt"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
	"strings"
)

// historyHeader shows the branch on the title row and a one-line summary of
// its sync state and local changes below it.
func (m model) historyHeader() []string {
	branch := m.snapshot.Branch
	title := m.historyTitle(branch, max(0, m.width-1))
	badge := ""
	if m.history.path != "" {
		title = "history · " + syntax.Clean(m.history.path)
		badge = styleDim + "whole-commit counts" + styleReset
	}
	lines := []string{m.titleBar(" "+title, badge)}
	if m.height-4 <= 0 {
		return lines
	}
	if !branch.Available {
		return append(lines, fit(styleDim+" Loading repository status…"+styleReset, m.width))
	}
	var parts []string
	switch {
	case branch.Upstream == "":
		parts = append(parts, styleDim+"no upstream"+styleReset)
	case !branch.CountsKnown:
		parts = append(parts, styleDim+"upstream unknown"+styleReset)
	case branch.Ahead == 0 && branch.Behind == 0:
		parts = append(parts, styleDim+"\x1b[32m✓ up to date"+styleReset)
	case m.width < 40:
		parts = append(parts, m.warning()+fmt.Sprintf("↑%d ↓%d", branch.Ahead, branch.Behind)+styleReset)
	default:
		if branch.Ahead > 0 {
			parts = append(parts, m.warning()+fmt.Sprintf("↑%d ahead", branch.Ahead)+styleReset)
		}
		if branch.Behind > 0 {
			parts = append(parts, m.warning()+fmt.Sprintf("↓%d behind", branch.Behind)+styleReset)
		}
	}
	count := func(n int, label string) string {
		return styleBold + fmt.Sprint(n) + styleReset + styleDim + " " + label + styleReset
	}
	unstaged, staged := len(m.snapshot.Unstaged), len(m.snapshot.Staged)
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
		branch := m.snapshot.Branch
		if branch.Available && branch.Upstream != "" && branch.CountsKnown && branch.Ahead == 0 {
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
func (m model) historyTitle(branch git.BranchStatus, width int) string {
	if !branch.Available || width <= 0 {
		return ""
	}
	name := branch.Name
	if name == "" {
		name = "unknown"
	}
	name = fit(syntax.Clean(name), width)
	result := styleBold + m.accent() + name + styleReset
	remaining := max(0, width-stringWidth(name))
	if branch.Upstream != "" && remaining > 0 {
		result += styleDim + fit(" → "+syntax.Clean(branch.Upstream), remaining) + styleReset
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

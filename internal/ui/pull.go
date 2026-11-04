package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type pullFailed struct {
	issue git.PullIssue
}

// startPull runs git pull. Failures pane can resolve open a dialog offering
// the fix instead of only showing Git's error.
func (m model) startPull() (tea.Model, tea.Cmd) {
	return m.runPull("", nil)
}

// runPull runs prepare, then pulls with args. A resolvable failure, including
// one hit while resolving another, opens the next dialog. A non-empty message
// replaces Git's output in the flash.
func (m model) runPull(message string, prepare func(git.Repo) error, args ...string) (tea.Model, tea.Cmd) {
	m.modal = ""
	m.busy = true
	m.activeAction = "pull"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		if prepare != nil {
			if err := prepare(repo); err != nil {
				return completed{action: "pull", err: err}
			}
		}
		output, issue, err := repo.Pull(args...)
		if issue != nil {
			return pullFailed{issue: *issue}
		}
		if message == "" {
			return completed{message: output, action: "pull", err: err}
		}
		return completed{message: message, action: "pull-resolved", err: err}
	}
}

func (m model) finishPullFailure(msg pullFailed) (tea.Model, tea.Cmd) {
	m.busy = false
	m.activeAction = ""
	m.pull = msg.issue
	m.modal = "pull-issue"
	return m, nil
}

func (m model) handlePullIssueKey(key string) (tea.Model, tea.Cmd) {
	issue := m.pull
	switch {
	case key == "esc" || key == "q" || key == "n" || key == "ctrl+c":
		m.modal = ""
	case issue.Kind == git.PullGone && (key == "enter" || key == "y"):
		return m.runPull("Switched to "+issue.Target+" and pulled", func(r git.Repo) error {
			return r.SwitchToDefault(issue)
		})
	case issue.Kind == git.PullUntracked && (key == "enter" || key == "y"):
		return m.runPull("Tracking "+issue.Upstream+" and pulled", func(r git.Repo) error {
			return r.TrackUpstream(issue)
		})
	case issue.Kind == git.PullDiverged && (key == "m" || key == "M" || key == "r" || key == "R"):
		rebase := key == "r" || key == "R"
		flag, message := "--no-rebase", "Merged "+issue.Upstream
		if rebase {
			flag, message = "--rebase", "Rebased onto "+issue.Upstream
		}
		var prepare func(git.Repo) error
		if key == "M" || key == "R" {
			message += " · pull.rebase set globally"
			prepare = func(r git.Repo) error { return r.SetPullRebase(rebase) }
		}
		return m.runPull(message, prepare, flag, "--no-edit")
	}
	return m, nil
}

// goneSummary only says the branch was merged when Git can prove it.
func goneSummary(gone git.PullIssue) string {
	if gone.Merged {
		return gone.Branch + " was merged into " + gone.Target + "."
	}
	return gone.Upstream + " was deleted on the remote."
}

// recovery is the content of a dialog offering to resolve a failed pull or
// push. short is the one-line form for tiny terminals.
type recovery struct {
	title, short, explain, command string
	commits                        []string
	hints                          []string
	danger                         bool
}

func (r recovery) plainHints() string {
	var parts []string
	for i := 0; i+1 < len(r.hints); i += 2 {
		parts = append(parts, strings.ReplaceAll(r.hints[i], "↵", "enter")+" "+r.hints[i+1])
	}
	return strings.Join(parts, "  ")
}

func (m model) recovery() recovery {
	if m.modal == "push-rejected" {
		return pushRecovery(m.pushIssue)
	}
	return pullRecovery(m.pull)
}

func pullRecovery(issue git.PullIssue) recovery {
	switch issue.Kind {
	case git.PullGone:
		explain := issue.Upstream + " was deleted on the remote."
		if issue.Merged {
			explain = issue.Branch + " was merged into " + issue.Target + " and " + issue.Upstream + " was deleted."
		}
		title := "Switch to " + issue.Target + "?"
		return recovery{
			title: title, short: goneSummary(issue) + " " + title, explain: explain,
			command: "Pane runs git switch " + issue.Target + " and git pull. " + issue.Branch + " is kept.",
			hints:   []string{"↵", "switch", "esc", "cancel"},
		}
	case git.PullUntracked:
		title := "Track " + issue.Upstream + "?"
		return recovery{
			title: title, short: title,
			explain: issue.Branch + " has no upstream, but " + issue.Remote + " has a branch with the same name.",
			command: "Pane runs git branch --set-upstream-to=" + issue.Upstream + " and git pull.",
			hints:   []string{"↵", "track", "esc", "cancel"},
		}
	default:
		return recovery{
			title: "Merge or rebase?", short: issue.Branch + " and " + issue.Upstream + " have diverged.",
			explain: fmt.Sprintf("%s and %s have diverged: %s local, %s remote.", issue.Branch, issue.Upstream, commitCount(issue.Ahead), commitCount(issue.Behind)),
			command: "m runs git pull --no-rebase, r runs git pull --rebase. M or R also sets pull.rebase in your global Git configuration.",
			hints:   []string{"m", "merge", "r", "rebase", "M/R", "always", "esc", "cancel"},
		}
	}
}

func commitCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

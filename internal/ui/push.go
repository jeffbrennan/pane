package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

// pushState holds the upstream question shown when the current branch has no
// configured upstream.
type pushState struct {
	branch  string
	remote  string
	remotes []string
}

type pushProbed struct {
	probe git.PushProbe
	err   error
}

// startPush asks Git whether the branch already tracks an upstream before
// running anything, so a new branch can offer to set one instead of failing.
func (m model) startPush() (tea.Model, tea.Cmd) {
	m.busy = true
	m.activeAction = "push"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		probe, err := repo.ProbePush()
		return pushProbed{probe: probe, err: err}
	}
}

func (m model) finishPushProbe(msg pushProbed) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.busy = false
		m.activeAction = ""
		m.errorText = msg.err.Error()
		m.errorPos = 0
		m.modal = "error"
		return m, nil
	}
	if msg.probe.Branch == "" || msg.probe.Upstream != "" {
		// Detached HEAD keeps Git's own error; an upstream push works as before.
		return m.runPush("", func(r git.Repo) (string, *git.PushIssue, error) { return r.Push() })
	}
	m.busy = false
	m.push = pushState{branch: msg.probe.Branch, remote: msg.probe.Remote, remotes: msg.probe.Remotes}
	m.modal = "push-upstream"
	return m, nil
}

// selectPushRemote chooses a numbered remote when the repository has several.
func (m model) selectPushRemote(key string) (model, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return m, false
	}
	index := int(key[0] - '1')
	if index >= len(m.push.remotes) {
		return m, false
	}
	m.push.remote = m.push.remotes[index]
	return m, true
}

// confirmPush runs the push that sets the upstream. always also turns on Git's
// push.autoSetupRemote, but only because the user pressed a.
func (m model) confirmPush(always bool) (tea.Model, tea.Cmd) {
	if m.push.remote == "" || m.push.branch == "" {
		return m, nil
	}
	remote, branch := m.push.remote, m.push.branch
	m.modal = ""
	m.busy = true
	m.activeAction = "push"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		if always {
			if err := repo.SetPushAutoSetupRemote(); err != nil {
				return completed{action: "push", err: err}
			}
		}
		_, err := repo.Action("push", "--set-upstream", remote, branch)
		message := ""
		if always && err == nil {
			message = "push.autoSetupRemote enabled globally"
		}
		return completed{action: "push", message: message, err: err}
	}
}

type pushRejected struct {
	issue git.PushIssue
}

// runPush runs push, opening the rejected-push dialog when the remote moved.
// A non-empty message replaces Git's output in the flash.
func (m model) runPush(message string, push func(git.Repo) (string, *git.PushIssue, error)) (tea.Model, tea.Cmd) {
	m.modal = ""
	m.busy = true
	m.activeAction = "push"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		_, issue, err := push(repo)
		if issue != nil {
			return pushRejected{issue: *issue}
		}
		return completed{message: message, action: "push", err: err}
	}
}

func (m model) finishPushRejected(msg pushRejected) (tea.Model, tea.Cmd) {
	m.busy = false
	m.activeAction = ""
	m.pushIssue = msg.issue
	m.modal = "push-rejected"
	return m, nil
}

func (m model) handlePushRejectedKey(key string) (tea.Model, tea.Cmd) {
	issue := m.pushIssue
	switch {
	case key == "esc" || key == "q" || key == "n" || key == "ctrl+c":
		m.modal = ""
	case issue.Kind == git.PushRewritten && key == "f":
		return m.runPush("Force pushed "+issue.Branch, func(r git.Repo) (string, *git.PushIssue, error) {
			return r.ForcePush(issue)
		})
	case issue.Kind == git.PushBehind && (key == "enter" || key == "y"):
		return m.pullThenPush()
	}
	return m, nil
}

// pullThenPush brings in the remote's new commits before pushing again. A pull
// that needs a decision stops at its dialog; push again after resolving it.
func (m model) pullThenPush() (tea.Model, tea.Cmd) {
	m.modal = ""
	m.busy = true
	m.activeAction = "pull"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		if _, issue, err := repo.Pull(); issue != nil {
			return pullFailed{issue: *issue}
		} else if err != nil {
			return completed{action: "pull", err: err}
		}
		_, issue, err := repo.Push()
		if issue != nil {
			return pushRejected{issue: *issue}
		}
		return completed{message: "Pulled and pushed", action: "push", err: err}
	}
}

func pushRecovery(issue git.PushIssue) recovery {
	upstream := issue.Upstream()
	if issue.Kind == git.PushRewritten {
		title := "Force push " + issue.Branch + "?"
		return recovery{
			title: title, short: title, commits: issue.Replaced, danger: true,
			explain: fmt.Sprintf("%s has %s that %s no longer has, likely from an amend or rebase. A force push replaces:", upstream, commitCount(len(issue.Replaced)), issue.Branch),
			command: "Pane runs git push --force-with-lease, which stops if " + upstream + " changed since.",
			hints:   []string{"f", "force push", "esc", "cancel"},
		}
	}
	return recovery{
		title: "Pull before pushing?", short: upstream + " has new commits. Pull, then push?", commits: issue.Replaced,
		explain: fmt.Sprintf("%s has %s that %s does not:", upstream, commitCount(len(issue.Replaced)), issue.Branch),
		command: "Pane runs git pull, then git push.",
		hints:   []string{"↵", "pull and push", "esc", "cancel"},
	}
}

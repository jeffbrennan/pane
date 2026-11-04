package ui

import (
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
		return m.runAction("push")
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

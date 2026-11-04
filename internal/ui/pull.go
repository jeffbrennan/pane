package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type pullFailed struct {
	gone git.GoneUpstream
}

// startPull runs git pull. A pull that fails because the upstream branch was
// deleted, as after a merged pull request, offers to switch to the default
// branch instead of only showing Git's error.
func (m model) startPull() (tea.Model, tea.Cmd) {
	m.busy = true
	m.activeAction = "pull"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		output, gone, err := repo.Pull()
		if gone != nil {
			return pullFailed{gone: *gone}
		}
		return completed{message: output, action: "pull", err: err}
	}
}

func (m model) finishPullFailure(msg pullFailed) (tea.Model, tea.Cmd) {
	m.busy = false
	m.activeAction = ""
	m.pull = msg.gone
	m.modal = "pull-gone"
	return m, nil
}

// confirmSwitchToDefault switches to the default branch and pulls it.
func (m model) confirmSwitchToDefault() (tea.Model, tea.Cmd) {
	gone := m.pull
	m.modal = ""
	m.busy = true
	m.activeAction = "pull"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		output, err := repo.SwitchAndPull(gone)
		return completed{message: output, action: "switch-pull", err: err}
	}
}

// goneSummary only says the branch was merged when Git can prove it.
func goneSummary(gone git.GoneUpstream) string {
	if gone.Merged {
		return gone.Branch + " was merged into " + gone.Target + "."
	}
	return gone.Upstream + " was deleted on the remote."
}

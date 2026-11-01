package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type blameLoaded struct {
	hash      string
	path      string
	line      int
	commit    git.CommitInfo
	jumps     []jumpLocation
	jumpIndex int
	before    jumpLocation
	err       error
}

func (m model) startBlame() (tea.Model, tea.Cmd) {
	if m.screen != "commit" || m.stash.viewing {
		m.flash = "Blame is available in commit diffs"
		return m, nil
	}
	view := m.commitModel()
	if view.pos < 0 || view.pos >= len(view.rows) {
		return m, nil
	}
	r := view.rows[view.pos]
	if r.path == "" || (r.kind != ' ' && r.kind != '-') {
		m.flash = "Blame needs a context or removed line"
		return m, nil
	}
	oldPath := r.path
	for _, c := range m.history.changes {
		if c.Path == r.path && c.OldPath != "" {
			oldPath = c.OldPath
			break
		}
	}
	line := r.sourceLine
	if r.kind == ' ' {
		added, deleted := 0, 0
		for i := 0; i < view.pos; i++ {
			row := view.rows[i]
			if row.path != r.path || row.offset > 0 {
				continue
			}
			switch row.kind {
			case '+':
				added++
			case '-', 'd':
				deleted++
			}
		}
		line -= added - deleted
	}
	if line <= 0 {
		m.flash = "No source line to blame"
		return m, nil
	}
	repo := m.commitRepository()
	hash := m.history.current.Hash
	before := view.location()
	jumps := append([]jumpLocation(nil), view.jumps...)
	jumpIndex := view.jumpIndex
	return m, func() tea.Msg {
		blamed, final, err := repo.Blame(hash, oldPath, line)
		if err != nil {
			return blameLoaded{err: err}
		}
		commit, err := repo.ResolveCommit(blamed)
		if err != nil {
			return blameLoaded{err: err}
		}
		return blameLoaded{hash: blamed, path: oldPath, line: final, commit: commit, jumps: jumps, jumpIndex: jumpIndex, before: before}
	}
}

func (m model) applyBlame(msg blameLoaded) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.flash = "Blame: " + msg.err.Error()
		return m, nil
	}
	if m.history.positions == nil {
		m.history.positions = make(map[string]commitPosition)
	}
	position := commitPosition{path: msg.path, line: msg.line}
	m.history.positions[msg.hash] = position
	updated, cmd := m.openCommit(msg.commit)
	next := updated.(model)
	next.history.jumps = msg.jumps
	next.history.jumpIndex = msg.jumpIndex
	if len(next.history.jumps) == 0 || !sameLocation(next.history.jumps[len(next.history.jumps)-1], msg.before) {
		next.history.jumps = append(next.history.jumps, msg.before)
	}
	next.history.jumpIndex = len(next.history.jumps) - 1
	next.history.pendingPosition = &position
	next.picker.pendingPath = msg.path
	next.flash = "Blamed " + shortHash(msg.hash)
	return next, cmd
}

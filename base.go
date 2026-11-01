package main

import (
	"bytes"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type baseState struct {
	revision, hash              string
	request                     int
	loading, initialized, dirty bool
}

type baseLoaded struct {
	hash    string
	request int
	changed bool
	err     error
}

// defaultBase picks the upstream branch, then origin's default branch, for
// `pane -b` with no revision.
func (r repository) defaultBase() (string, error) {
	if out, err := r.command("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" && name != "@{upstream}" {
			return name, nil
		}
	}
	if out, err := r.command("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("no upstream branch; pass a base revision, for example pane -b main")
}

// openBaseFrom starts a base review from a history commit to the working tree.
func (m model) openBaseFrom(revision string) (tea.Model, tea.Cmd) {
	hash, err := m.repo.mergeBase(revision)
	if err != nil {
		m.flash = "Base review: " + err.Error()
		return m, nil
	}
	m.screen = "base"
	m.base = baseState{revision: revision, hash: hash, initialized: true}
	m.history.directCommit = true
	m.history.changes, m.history.rows = nil, nil
	m.history.request++
	m.base.request++
	m.startCommitLoad()
	m.contextFiles = nil
	m.history.streamPaths, m.history.streamLoaded, m.history.streamGutter = nil, 0, 0
	m.history.streamError, m.history.streamEndPending = "", false
	m.history.streamActive = true
	m.history.loading = true
	m.history.commitLoadingVisible = false
	m.history.pos, m.history.horizontal, m.history.viewTop = 0, 0, 0
	m.picker.pendingPath = ""
	m.collapsedFiles, m.generatedHandled = nil, nil
	repo := m.commitRepository()
	return m, tea.Batch(startBaseStream(repo, hash, m.base.request, m.height), commitLoadingTimer(m.history.request))
}

func (r repository) mergeBase(revision string) (string, error) {
	commit, err := r.resolveCommit(revision)
	if err != nil {
		return "", fmt.Errorf("invalid base %q: %w", revision, err)
	}
	out, err := r.command("merge-base", commit.hash, "HEAD")
	if err != nil {
		return "", fmt.Errorf("no common ancestor with %s: %w", revision, gitError(out, err))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r repository) untrackedChanges() ([]change, error) {
	out, err := r.command("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, gitError(out, err)
	}
	var untracked []string
	for _, path := range bytes.Split(out, []byte{0}) {
		if len(path) > 0 {
			untracked = append(untracked, string(path))
		}
	}
	added, err := parallelDiffs(untracked, r.untrackedDiff)
	if err != nil {
		return nil, err
	}
	for i := range added {
		added[i].untracked = true
	}
	return added, nil
}

func (m model) refreshBase(force bool) (model, tea.Cmd) {
	if m.screen != "base" {
		return m, nil
	}
	if m.base.loading {
		m.base.dirty = m.base.dirty || force
		return m, nil
	}
	m.base.loading = true
	m.base.request++
	repo, base, request := m.repo, m.base, m.base.request
	return m, func() tea.Msg {
		hash, err := repo.mergeBase(base.revision)
		if err != nil {
			return baseLoaded{request: request, err: err}
		}
		changed := force || !base.initialized || hash != base.hash
		return baseLoaded{hash: hash, request: request, changed: changed}
	}
}

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

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
	changes []change
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

func (r repository) baseChanges(hash string) ([]change, error) {
	args := []string{"diff", "--name-status", "-M", "-z", hash, "--"}
	out, err := r.commandStdout(args...)
	if err != nil {
		return nil, gitError(out, err)
	}
	fields := bytes.Split(out, []byte{0})
	var files []change
	for i := 0; i < len(fields) && len(fields[i]) > 0; {
		status := string(fields[i])
		i++
		if i >= len(fields) || len(fields[i]) == 0 {
			return nil, fmt.Errorf("unexpected Git change format")
		}
		c := change{path: string(fields[i])}
		i++
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i >= len(fields) || len(fields[i]) == 0 {
				return nil, fmt.Errorf("unexpected Git rename format")
			}
			c.oldPath, c.path = c.path, string(fields[i])
			i++
		}
		files = append(files, c)
	}
	args = append([]string{"diff"}, r.diffOptions()...)
	args = append(args, "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
	patch, err := r.commandStdout(args...)
	if err != nil {
		return nil, gitError(patch, err)
	}
	patches := splitPatch(string(patch))
	paths := make([]string, len(files))
	byPath := make(map[string]change, len(files))
	for i, c := range files {
		paths[i], byPath[c.path] = c.path, c
	}
	changes, err := parallelDiffs(paths, func(path string) (string, error) {
		if diff, ok := patches[path]; ok {
			return diff, nil
		}
		c := byPath[path]
		args := append([]string{"diff"}, r.diffOptions()...)
		args = append(args, "--src-prefix=a/", "--dst-prefix=b/", hash, "--", c.path)
		if c.oldPath != "" {
			args = append(args, c.oldPath)
		}
		out, err := r.commandStdout(args...)
		if err != nil {
			return "", gitError(out, err)
		}
		if len(out) == 0 && r.ignoreSpace {
			return "Whitespace-only changes", nil
		}
		return string(out), nil
	})
	if err != nil {
		return nil, err
	}
	for i := range changes {
		changes[i].oldPath = files[i].oldPath
	}
	out, err = r.command("ls-files", "--others", "--exclude-standard", "-z")
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
	changes = append(changes, added...)
	r.loadSyntaxDiffs(changes, contextKey{commit: hash, base: true})
	warmSyntax(changes)
	return changes, nil
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
		if !changed {
			return baseLoaded{hash: hash, request: request}
		}
		changes, err := repo.baseChanges(hash)
		return baseLoaded{hash: hash, changes: changes, request: request, changed: true, err: err}
	}
}

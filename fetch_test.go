package main

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestFetchUpdatesHistoryStatus(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "initial\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init remote: %s: %v", out, err)
	}
	if _, err := r.Action("remote", "add", "team/origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("push", "-u", "team/origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "--branch", "main", remote, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %s: %v", out, err)
	}
	other := git.Repo{Root: clone}
	for _, args := range [][]string{{"config", "user.name", "Test"}, {"config", "user.email", "test@example.com"}} {
		if _, err := other.Action(args...); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, other, "file", "remote update\n")
	if _, err := other.Commit("remote update"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Action("push"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	var fetchArgs []string
	r.CommandHook = func(args []string) {
		if len(args) > 0 && args[0] == "fetch" {
			fetchArgs = append([]string(nil), args...)
		}
	}
	m := model{repo: r, snapshot: snapshot, width: 80, height: 20, screen: "history", history: historyState{filter: "keep", selected: 1}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	m = updated.(model)
	if cmd == nil || !m.busy || !strings.Contains(m.View(), "Fetching…") {
		t.Fatal("fetch did not start with progress")
	}
	result := cmd().(completed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !slices.Equal(fetchArgs, []string{"fetch", "--prune", "--", "team/origin"}) {
		t.Fatalf("wrong fetch target: %q", fetchArgs)
	}
	updated, cmd = m.Update(result)
	m = updated.(model)
	if cmd == nil || m.screen != "history" || m.history.filter != "keep" || m.history.selected != 1 {
		t.Fatalf("fetch changed history state: %#v", m.history)
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if m.snapshot.Branch.Behind != 1 || !strings.Contains(m.View(), "↓1 behind") {
		t.Fatalf("fetch did not update status: %#v %q", m.snapshot.Branch, m.View())
	}
}

func TestFetchNoRemoteAndNoUpstream(t *testing.T) {
	r := testRepo(t)
	message, err := r.Fetch()
	if err != nil || message != "No remotes configured" {
		t.Fatalf("no remote: %q %v", message, err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init remote: %s: %v", out, err)
	}
	if _, err := r.Action("remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	var fetchArgs []string
	r.CommandHook = func(args []string) {
		if len(args) > 0 && args[0] == "fetch" {
			fetchArgs = append([]string(nil), args...)
		}
	}
	if _, err := r.Fetch(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fetchArgs, []string{"fetch", "--all", "--prune"}) {
		t.Fatalf("fetch args: %q", fetchArgs)
	}
}

func TestFetchAvailableAcrossViewsAndNotInSearch(t *testing.T) {
	for _, screen := range []string{"", "history", "commit", "base", "sequence"} {
		m := model{screen: screen}
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
		if cmd == nil || !updated.(model).busy || updated.(model).activeAction != "fetch" {
			t.Errorf("fetch unavailable on %q", screen)
		}
	}
	m := model{screen: "history", history: historyState{searching: true}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	if cmd != nil || updated.(model).history.draft != "F" || updated.(model).busy {
		t.Fatal("fetch shortcut intercepted history search")
	}
}

func TestFetchNoRemoteShowsFlashAndErrorsShowModal(t *testing.T) {
	r := testRepo(t)
	m := model{repo: r, width: 80, height: 15}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	updated, _ = updated.(model).Update(cmd())
	if got := updated.(model); got.busy || got.flash != "No remotes configured" || got.modal != "" {
		t.Fatalf("no remote state: %#v", got)
	}
	if _, err := r.Action("remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git")); err != nil {
		t.Fatal(err)
	}
	m = model{repo: r, width: 80, height: 15}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	updated, _ = updated.(model).Update(cmd())
	if got := updated.(model); got.busy || got.modal != "error" || got.errorText == "" {
		t.Fatalf("fetch error state: %#v", got)
	}
}

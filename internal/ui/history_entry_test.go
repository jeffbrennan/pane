package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestDefaultBaseUsesUpstream(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := r.CommandStdout("init", "--bare", remote); err != nil {
		t.Fatalf("init bare: %s %v", out, err)
	}
	if _, err := r.Command("remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("push", "-u", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	base, err := r.DefaultBase()
	if err != nil {
		t.Fatal(err)
	}
	if base != "origin/main" {
		t.Fatalf("default base %q", base)
	}
}

func TestDefaultBaseFallsBackToOriginHead(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if _, err := r.CommandStdout("init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("fetch", "origin"); err != nil {
		t.Fatal(err)
	}
	// Without a configured upstream, origin's default branch is the fallback.
	if _, err := r.Command("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"); err != nil {
		t.Fatal(err)
	}
	base, err := r.DefaultBase()
	if err != nil || base != "origin/main" {
		t.Fatalf("default base %q %v", base, err)
	}
}

func TestDefaultBaseWithoutRemote(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DefaultBase(); err == nil || !strings.Contains(err.Error(), "no upstream") {
		t.Fatalf("expected an error, got %v", err)
	}
}

func TestHistoryRStartsBaseReview(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("one"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "two\n")
	if _, err := r.Commit("two"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, git.HistoryPage)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 80, height: 20, screen: "history", repo: r, viewed: newViewedStore(r.Root),
		history: historyState{commits: commits, directCommit: false}}
	updated, cmd := m.handleHistoryKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = updated.(model)
	if m.screen != "base" || m.base.revision != commits[0].Hash || m.base.hash != commits[0].Hash {
		t.Fatalf("base review: screen=%q base=%#v", m.screen, m.base)
	}
	if cmd == nil || !m.history.streamActive || !m.history.loading {
		t.Fatal("base review did not start loading")
	}
}

func TestHistoryVisualSelectionOpensSequence(t *testing.T) {
	commits := []git.CommitInfo{{Hash: "aaaa"}, {Hash: "bbbb"}, {Hash: "cccc"}}
	m := model{width: 80, height: 20, screen: "history", history: historyState{commits: commits, selected: 0}}
	updated, _ := m.handleHistoryKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("V")})
	m = updated.(model)
	if !m.history.visualActive {
		t.Fatal("V did not start a selection")
	}
	updated, _ = m.handleHistoryKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(model)
	updated, cmd := m.handleHistoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != "sequence" || len(m.sequence.commits) != 2 || m.sequence.commits[0].Hash != "aaaa" || m.sequence.commits[1].Hash != "bbbb" {
		t.Fatalf("sequence: screen=%q commits=%#v", m.screen, m.sequence.commits)
	}
	if cmd == nil {
		t.Fatal("sequence review did not start loading")
	}
	m.rebuildSequence()
	view := m.sequenceView()
	if !strings.Contains(view, "1/2") {
		t.Fatalf("badge: %q", view)
	}
}

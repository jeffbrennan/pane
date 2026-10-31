package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStashPushInspectAndPop(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "tracked", "before\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "tracked", "after\n")
	writeFile(t, r, "untracked", "new content\n")
	snapshot, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: snapshot, width: 80, height: 20}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	if cmd == nil || !updated.(model).busy {
		t.Fatal("stash did not start")
	}
	result := cmd().(completed)
	if result.err != nil || result.message != "Stashed 2 files" {
		t.Fatalf("stash push: %#v", result)
	}
	status, err := r.load()
	if err != nil || len(status.staged)+len(status.unstaged) != 0 {
		t.Fatalf("worktree not clean: %#v %v", status, err)
	}
	updated, _ = updated.(model).Update(result)
	m = updated.(model)
	if m.screen != "history" {
		t.Fatal("stash did not open history")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	if len(m.stash.entries) != 1 || !strings.Contains(m.View(), "stash@{0}") {
		t.Fatalf("stash list: %#v %q", m.stash, m.View())
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	if m.screen != "commit" || !strings.Contains(m.View(), "after") || !strings.Contains(m.View(), "new content") {
		t.Fatalf("stash diff missing content: %q", m.View())
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd != nil || updated.(model).modal != "stash-confirm" {
		t.Fatal("pop did not request confirmation")
	}
	updated, cmd = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	result = cmd().(completed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	content, err := os.ReadFile(filepath.Join(r.root, "untracked"))
	if err != nil || string(content) != "new content\n" {
		t.Fatalf("untracked not restored: %q %v", content, err)
	}
	entries, err := r.stashes()
	if err != nil || len(entries) != 0 {
		t.Fatalf("stash not popped: %#v %v", entries, err)
	}
}

func TestStashDropConfirmationAndStaleSelection(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "tracked", "before\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "tracked", "first\n")
	if _, err := r.stashPush(); err != nil {
		t.Fatal(err)
	}
	entries, err := r.stashes()
	if err != nil || len(entries) != 1 {
		t.Fatalf("stashes: %#v %v", entries, err)
	}
	m := model{repo: r, screen: "stashes", width: 70, height: 15, stash: stashState{entries: entries}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd != nil || updated.(model).modal != "stash-confirm" || !strings.Contains(updated.(model).View(), "Drop stash?") {
		t.Fatal("drop did not request confirmation")
	}
	writeFile(t, r, "tracked", "second\n")
	if _, err := r.stashPush(); err != nil {
		t.Fatal(err)
	}
	updated, cmd = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	result := cmd().(completed)
	if result.err == nil || !strings.Contains(result.err.Error(), "stash list changed") {
		t.Fatalf("stale stash not rejected: %#v", result)
	}
	entries, err = r.stashes()
	if err != nil || len(entries) != 2 {
		t.Fatalf("stale action changed stashes: %#v %v", entries, err)
	}
}

func TestStashDropAndHistoryCount(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "base\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "changed\n")
	if _, err := r.stashPush(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: snapshot, width: 80, height: 15, screen: "history", history: historyState{request: 1, loading: true}}
	updated, _ := m.Update(m.fetchHistory(0, historyPage)())
	m = updated.(model)
	if m.history.stashCount != 1 || !strings.Contains(m.View(), "1 stashed") {
		t.Fatalf("stash count missing: %q", m.View())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	updated, _ = updated.(model).Update(cmd())
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	updated, cmd = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil || updated.(model).modal != "" {
		t.Fatal("drop cancellation failed")
	}
	entries, err := r.stashes()
	if err != nil || len(entries) != 1 {
		t.Fatal("cancelling removed the stash")
	}
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	_, cmd = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if result := cmd().(completed); result.err != nil {
		t.Fatal(result.err)
	}
	entries, err = r.stashes()
	if err != nil || len(entries) != 0 {
		t.Fatalf("drop failed: %#v %v", entries, err)
	}
}

func TestStashPopConflictKeepsStash(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "base\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "stashed\n")
	if _, err := r.stashPush(); err != nil {
		t.Fatal(err)
	}
	entries, err := r.stashes()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "local\n")
	if _, err := r.stashAction("pop", entries[0]); err == nil {
		t.Fatal("expected conflicting pop to fail")
	}
	entries, err = r.stashes()
	if err != nil || len(entries) != 1 {
		t.Fatalf("conflicting pop removed stash: %#v %v", entries, err)
	}
	content, err := os.ReadFile(filepath.Join(r.root, "file"))
	if err != nil || string(content) != "local\n" {
		t.Fatalf("conflicting pop overwrote local changes: %q %v", content, err)
	}
}

func TestStashPopMergeConflictKeepsStash(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "base\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "stashed\n")
	if _, err := r.stashPush(); err != nil {
		t.Fatal(err)
	}
	entries, err := r.stashes()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "committed\n")
	if _, err := r.commit("diverged"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.stashAction("pop", entries[0]); err == nil {
		t.Fatal("expected merge conflict")
	}
	entries, err = r.stashes()
	if err != nil || len(entries) != 1 {
		t.Fatalf("merge conflict removed stash: %#v %v", entries, err)
	}
	content, err := os.ReadFile(filepath.Join(r.root, "file"))
	if err != nil || !strings.Contains(string(content), "<<<<<<<") {
		t.Fatalf("expected conflict markers: %q %v", content, err)
	}
}

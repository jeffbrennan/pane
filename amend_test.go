package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditLatestCommitMessage(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "original\n")
	if _, err := r.Commit(":q\n\nbody"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "staged\n")
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, width: 80, height: 20, screen: "history", history: historyState{commits: commits}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = updated.(model)
	if m.modal != "commit" || m.input != ":q" || m.inputBody != "\n\nbody" || !strings.Contains(m.View(), "Edit commit message") {
		t.Fatalf("edit dialog: %#v", m)
	}
	m = keys(t, m, "ctrl+u", "f", "i", "x")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("amend not started")
	}
	result := cmd().(completed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	message, err := r.Command("log", "-1", "--format=%B")
	if err != nil || strings.TrimSpace(string(message)) != "fix\n\nbody" {
		t.Fatalf("message %q: %v", message, err)
	}
	content, err := r.Command("show", "HEAD:file")
	if err != nil || string(content) != "original\n" {
		t.Fatalf("amend included staged changes: %q %v", content, err)
	}
	index, err := r.Command("show", ":file")
	if err != nil || string(index) != "staged\n" {
		t.Fatalf("index changed: %q %v", index, err)
	}
}

func TestAmendRejectsPublishedAndStaleHead(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "content")
	if _, err := r.Commit("original"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote with spaces.git")
	if out, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init remote: %s %v", out, err)
	}
	if _, err := r.Action("remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AmendMessage(commits[0].Hash, "new"); err == nil || !strings.Contains(err.Error(), "already reachable") {
		t.Fatalf("published commit not rejected: %v", err)
	}
	writeFile(t, r, "second", "next")
	if _, err := r.Commit("local"); err != nil {
		t.Fatal(err)
	}
	latest, err := r.History(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AmendMessage(latest[0].Hash, "corrected"); err != nil {
		t.Fatalf("unpublished commit rejected: %v", err)
	}
	if _, err := r.AmendMessage(strings.Repeat("0", 40), "new"); err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("stale HEAD not rejected: %v", err)
	}
}

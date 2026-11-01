package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func headerText(rows []row) string {
	var parts []string
	for _, r := range rows {
		parts = append(parts, r.text)
	}
	return strings.Join(parts, "\n")
}

func TestCommitDetailAndMergeParents(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base.txt", "base\n")
	if _, err := r.Commit("base commit"); err != nil {
		t.Fatal(err)
	}
	branch, err := r.Command("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("checkout", "-b", "side"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "side.txt", "side\n")
	if _, err := r.Commit("side commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("checkout", strings.TrimSpace(string(branch))); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "main.txt", "main\n")
	if _, err := r.Commit("main commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("merge", "--no-ff", "side", "-m", "merge side\n\n* merged side\n\nCo-authored-by: Other <other@example.com>"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(commit.Parents) != 2 || commit.Author != "Test" || commit.Committer != "Test" {
		t.Fatalf("detail: %#v", commit)
	}
	rows := commitHeaderBlock(commit, 80, false, time.Now())
	text := headerText(rows)
	if !strings.Contains(text, "merge") || !strings.Contains(text, shortHash(commit.Parents[0])) || !strings.Contains(text, shortHash(commit.Parents[1])) {
		t.Fatalf("missing merge parents: %s", text)
	}
	if !strings.Contains(text, "merged side") || !strings.Contains(text, "Co-authored-by") {
		t.Fatalf("missing body or trailer: %s", text)
	}
	if rows[len(rows)-1].kind != 'm' || strings.TrimSpace(rows[len(rows)-1].text) != "" {
		t.Fatal("header block does not end with a blank row")
	}
}

func TestCommitHeaderSearchAndJumpToFirstFile(t *testing.T) {
	commit := CommitInfo{Hash: "abcdef1234567890", Author: "Alice", Email: "alice@example.com", Date: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), Subject: "subject line", Message: "subject line\n\nthe reason is in the body\n\nFixes: #12"}
	m := model{width: 100, height: 30, wrap: true, screen: "commit", history: historyState{current: commit, meta: commit, changes: []Change{{Path: "file.go", Diff: "@@ -0,0 +1 @@\n+line\n"}}}}
	m.rebuildCommit()
	if !strings.Contains(headerText(m.history.rows), "the reason is in the body") || !strings.Contains(headerText(m.history.rows), "2h ago") {
		t.Fatalf("header block missing: %s", headerText(m.history.rows))
	}
	if index, _ := m.commitModel().findMatch("reason is in the body", -1, 1); index < 0 || m.history.rows[index].kind != 'm' {
		t.Fatal("search did not find header body text")
	}
	view := m.commitModel()
	view.pos = 0
	view.jump(1)
	if view.rows[view.pos].kind != 'f' || view.rows[view.pos].path != "file.go" {
		t.Fatalf("J from the header did not land on the first file: %#v", view.rows[view.pos])
	}
}

func TestCommitHeaderCollapsesWithHL(t *testing.T) {
	commit := CommitInfo{Hash: "abcdef1234567890", Author: "Alice", Email: "alice@example.com", Date: time.Now().Format(time.RFC3339), Subject: "subject", Message: "subject\n\nbody"}
	m := model{width: 100, height: 30, wrap: true, screen: "commit", history: historyState{current: commit, meta: commit, changes: []Change{{Path: "file.go", Diff: "@@ -0,0 +1 @@\n+line\n"}}}}
	m.rebuildCommit()
	expanded := len(m.history.rows)
	updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	if !m.history.headerCollapsed || len(m.history.rows) >= expanded || m.history.rows[0].kind != 'M' {
		t.Fatalf("h did not collapse the header: %d rows, %#v", len(m.history.rows), m.history.rows)
	}
	updated, _ = m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(model)
	if m.history.headerCollapsed || len(m.history.rows) != expanded {
		t.Fatal("l did not expand the header again")
	}
}

func TestStreamCommitAttachesMetadata(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("first commit\n\nbody text"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	stream := r.StreamCommit(context.Background(), strings.TrimSpace(string(hash)), startupSyntaxLines)
	first := <-stream
	if first.Meta.Hash != strings.TrimSpace(string(hash)) || first.Meta.Author != "Test" || !strings.Contains(first.Meta.Message, "body text") {
		t.Fatalf("first chunk metadata: %#v", first.Meta)
	}
	for range stream {
	}
}

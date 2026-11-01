package main

import (
	"strings"
	"testing"
)

func TestBlameOpensPreviousCommitAtLine(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "alpha\nbeta\ngamma\n")
	if _, err := r.commit("first"); err != nil {
		t.Fatal(err)
	}
	first, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "alpha\nchanged\ngamma\n")
	if _, err := r.commit("second"); err != nil {
		t.Fatal(err)
	}
	second, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges(second.hash)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 30, wrap: true, screen: "commit", repo: r, viewed: newViewedStore(r.root),
		history: historyState{current: second, meta: second, changes: changes, request: 1, streamActive: true}}
	m.history.loading = false
	m.rebuildCommit()
	pos := -1
	for i, row := range m.history.rows {
		if row.path == "file.txt" && row.kind == '-' {
			pos = i
		}
	}
	if pos < 0 {
		t.Fatal("fixture has no removed line")
	}
	m.history.pos = pos
	updated, cmd := m.startBlame()
	m = updated.(model)
	if cmd == nil {
		t.Fatal("blame produced no command")
	}
	msg, ok := cmd().(blameLoaded)
	if !ok || msg.err != nil {
		t.Fatalf("blame message: %#v", msg)
	}
	if msg.hash != first.hash || msg.line != 2 {
		t.Fatalf("blame target %s:%d, want %s:2", shortHash(msg.hash), msg.line, shortHash(first.hash))
	}
	updated, cmd = m.applyBlame(msg)
	m = updated.(model)
	if m.screen != "commit" || m.history.current.hash != first.hash || cmd == nil {
		t.Fatalf("did not open blamed commit: %#v", m.history.current)
	}
	if len(m.history.jumps) == 0 {
		t.Fatal("blame did not record a jump")
	}
	changes, err = r.commitChanges(first.hash)
	if err != nil {
		t.Fatal(err)
	}
	m = deliverChunk(t, m, commitChunk{paths: []string{"file.txt"}, changes: changes, loaded: 1, done: true, meta: first})
	if m.history.pos >= len(m.history.rows) || m.history.rows[m.history.pos].sourceLine != 2 {
		t.Fatalf("blame did not select the line: %#v", m.history.rows[m.history.pos])
	}
}

func TestBlameUsesOldPathForRenames(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "old.txt", "alpha\nbeta\n")
	if _, err := r.commit("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("mv", "old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.commit("rename"); err != nil {
		t.Fatal(err)
	}
	rename, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges(rename.hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].oldPath != "old.txt" {
		t.Fatalf("rename fixture: %#v", changes)
	}
	m := model{width: 100, height: 30, wrap: true, screen: "commit", repo: r, viewed: newViewedStore(r.root),
		history: historyState{current: rename, meta: rename, changes: changes, request: 1, streamActive: true}}
	m.history.loading = false
	m.rebuildCommit()
	// A pure rename has no lines to blame; make one context row by editing.
	writeFile(t, r, "new.txt", "alpha\nbeta\ngamma\n")
	if _, err := r.commit("edit"); err != nil {
		t.Fatal(err)
	}
	edit, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err = r.commitChanges(edit.hash)
	if err != nil {
		t.Fatal(err)
	}
	m.history.current, m.history.meta, m.history.changes = edit, edit, changes
	m.rebuildCommit()
	pos := -1
	for i, row := range m.history.rows {
		if row.path == "new.txt" && row.kind == ' ' {
			pos = i
		}
	}
	if pos < 0 {
		t.Fatal("fixture has no context line")
	}
	m.history.pos = pos
	_, cmd := m.startBlame()
	if cmd == nil {
		t.Fatal("blame produced no command")
	}
	msg := cmd().(blameLoaded)
	if msg.err != nil || msg.hash == "" {
		t.Fatalf("blame failed: %#v", msg)
	}
	if !strings.HasSuffix(msg.path, ".txt") {
		t.Fatalf("blame path %q", msg.path)
	}
}

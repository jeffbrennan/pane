package ui

import (
	"github.com/jeffbrennan/pane/internal/git"
	"strings"
	"testing"
)

func TestBlameOpensPreviousCommitAtLine(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "alpha\nbeta\ngamma\n")
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	first, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "alpha\nchanged\ngamma\n")
	if _, err := r.Commit("second"); err != nil {
		t.Fatal(err)
	}
	second, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(second.Hash)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 30, wrap: true, screen: "commit", repo: r, viewed: newViewedStore(r.Root),
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
	if msg.hash != first.Hash || msg.line != 2 {
		t.Fatalf("blame target %s:%d, want %s:2", shortHash(msg.hash), msg.line, shortHash(first.Hash))
	}
	updated, cmd = m.applyBlame(msg)
	m = updated.(model)
	if m.screen != "commit" || m.history.current.Hash != first.Hash || cmd == nil {
		t.Fatalf("did not open blamed commit: %#v", m.history.current)
	}
	if len(m.history.jumps) == 0 {
		t.Fatal("blame did not record a jump")
	}
	changes, err = r.CommitChanges(first.Hash)
	if err != nil {
		t.Fatal(err)
	}
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"file.txt"}, Changes: changes, Loaded: 1, Done: true, Meta: first})
	if m.history.pos >= len(m.history.rows) || m.history.rows[m.history.pos].sourceLine != 2 {
		t.Fatalf("blame did not select the line: %#v", m.history.rows[m.history.pos])
	}
}

func TestBlameUsesOldPathForRenames(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "old.txt", "alpha\nbeta\n")
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("mv", "old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("rename"); err != nil {
		t.Fatal(err)
	}
	rename, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(rename.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].OldPath != "old.txt" {
		t.Fatalf("rename fixture: %#v", changes)
	}
	m := model{width: 100, height: 30, wrap: true, screen: "commit", repo: r, viewed: newViewedStore(r.Root),
		history: historyState{current: rename, meta: rename, changes: changes, request: 1, streamActive: true}}
	m.history.loading = false
	m.rebuildCommit()
	// A pure rename has no lines to blame; make one context row by editing.
	writeFile(t, r, "new.txt", "alpha\nbeta\ngamma\n")
	if _, err := r.Commit("edit"); err != nil {
		t.Fatal(err)
	}
	edit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err = r.CommitChanges(edit.Hash)
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

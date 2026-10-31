package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDiscardSelectedHunkPreservesOtherChanges(t *testing.T) {
	r := testRepo(t)
	original := "first\n" + strings.Repeat("context\n", 12) + "last\n"
	writeFile(t, r, "file.txt", original)
	if err := r.stage("file.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	changed := "changed first\n" + strings.Repeat("context\n", 12) + "changed last\n"
	writeFile(t, r, "file.txt", changed)
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: s, width: 80, height: 20, wrap: true}
	m.rebuild()
	for i, row := range m.rows {
		if row.text == "changed first" {
			m.pos = i
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(model)
	if m.modal != "discard" || !strings.Contains(m.View(), "Discard hunk") {
		t.Fatalf("missing confirmation: %q", m.View())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("missing discard command")
	}
	if result := cmd().(completed); result.err != nil {
		t.Fatal(result.err)
	}
	content, err := os.ReadFile(filepath.Join(r.root, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := "first\n" + strings.Repeat("context\n", 12) + "changed last\n"
	if string(content) != want {
		t.Fatalf("discarded wrong content: %q", content)
	}
	if !updated.(model).busy {
		t.Fatal("discard did not mark model busy")
	}
}

func TestDiscardFileKeepsStagedChangesAndRejectsStale(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "base\n")
	if err := r.stage("file.txt", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "working\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	request := discardRequest{change: s.unstaged[0], whole: true}
	writeFile(t, r, "file.txt", "newer\n")
	if err := r.discard(request); err == nil {
		t.Fatal("discard accepted a stale diff")
	}
	writeFile(t, r, "file.txt", "working\n")
	if err := r.discard(request); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(r.root, "file.txt"))
	if err != nil || string(content) != "base\n" {
		t.Fatalf("did not restore staged version: %q, %v", content, err)
	}
}

func TestDiscardUntrackedConfirmationAndStaleBinary(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new.bin", "\x00one")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: s, width: 80, height: 10}
	m.rebuild()
	updated, _ := m.startDiscard()
	m = updated.(model)
	if m.modal != "discard" || !strings.Contains(m.View(), "Delete untracked file from disk") {
		t.Fatalf("missing deletion confirmation: %q", m.View())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if updated.(model).modal != "" || cmd != nil {
		t.Fatal("cancel did not close confirmation")
	}
	writeFile(t, r, "new.bin", "\x00two")
	if err := r.discard(m.discard); err == nil {
		t.Fatal("discard accepted modified binary file")
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.discard(discardRequest{change: s.unstaged[0], whole: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(r.root, "new.bin")); !os.IsNotExist(err) {
		t.Fatalf("file was not deleted: %v", err)
	}
}

func TestDiscardUnavailableForStagedAndContextOutsideHunk(t *testing.T) {
	m := model{staged: true, rows: []row{{path: "file", kind: 'f'}}}
	updated, _ := m.startDiscard()
	if m = updated.(model); m.modal != "" || !strings.Contains(m.flash, "Unstage") {
		t.Fatalf("staged discard was allowed: %#v", m)
	}
	if got := selectedHunk("@@ -5 +5 @@\n-old\n+new\n", row{kind: ' ', sourceLine: 3}); got != "" {
		t.Fatalf("selected hunk outside its range: %q", got)
	}
}

func TestDiscardHunkRestoresDeletedFile(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "deleted file.txt", "first\nsecond\n")
	if err := r.stage("deleted file.txt", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, "deleted file.txt")); err != nil {
		t.Fatal(err)
	}
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	rows := buildRowsExpanded(s.unstaged, 80, true, map[string]bool{"deleted file.txt": true})
	var selected row
	for _, r := range rows {
		if r.kind == 'd' {
			selected = r
			break
		}
	}
	patch := selectedHunk(s.unstaged[0].diff, selected)
	if patch == "" {
		t.Fatal("missing deleted file hunk")
	}
	if err := r.discard(discardRequest{change: s.unstaged[0], patch: patch}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(r.root, "deleted file.txt"))
	if err != nil || string(content) != "first\nsecond\n" {
		t.Fatalf("deleted file was not restored: %q, %v", content, err)
	}
}

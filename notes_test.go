package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func noteTestModel(t *testing.T) model {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "one.txt", "first\nsecond\nthird\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "one.txt", "first\nchanged\nthird\n")
	snapshot, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 40, wrap: true, snapshot: snapshot, repo: r}
	m.rebuild()
	return m
}

func changedRowIndex(m model) int {
	for i, r := range m.rows {
		if r.path == "one.txt" && r.kind == '+' {
			return i
		}
	}
	return -1
}

func TestAddEditAndDeleteNote(t *testing.T) {
	m := noteTestModel(t)
	m.pos = changedRowIndex(m)
	if m.pos < 0 {
		t.Fatal("fixture has no added line")
	}
	line := m.rows[m.pos].sourceLine
	if !m.startNote() || m.modal != "note" {
		t.Fatal("m did not open the note editor")
	}
	m.input = "this needs a test"
	m.saveNote()
	ref := noteRef{view: "worktree", path: "one.txt", line: line}
	if m.notes[ref] != "this needs a test" {
		t.Fatalf("notes: %#v", m.notes)
	}
	found := false
	for _, r := range m.rows {
		if r.kind == 'n' && r.path == "one.txt" && strings.Contains(r.text, "this needs a test") {
			found = true
		}
	}
	if !found {
		t.Fatalf("note row missing: %#v", m.rows)
	}
	if index, _ := m.findMatch("this needs a test", -1, 1); index < 0 {
		t.Fatal("search did not find the note")
	}

	// Editing pre-fills the note.
	m.pos = changedRowIndex(m)
	m.startNote()
	if m.input != "this needs a test" {
		t.Fatalf("editor prefill: %q", m.input)
	}
	m.input = "edited"
	m.saveNote()
	if m.notes[ref] != "edited" {
		t.Fatalf("edit not saved: %#v", m.notes)
	}

	// An empty note deletes it.
	m.pos = changedRowIndex(m)
	m.startNote()
	m.input = ""
	m.saveNote()
	if _, ok := m.notes[ref]; ok {
		t.Fatal("empty note did not delete")
	}
}

func TestNotesMarkdownExport(t *testing.T) {
	m := noteTestModel(t)
	m.pos = changedRowIndex(m)
	line := m.rows[m.pos].sourceLine
	m.startNote()
	m.input = "please rename this"
	m.saveNote()
	m.pos = changedRowIndex(m)
	m.startNote()
	m.noteTarget = noteRef{view: "worktree", path: "one.txt", line: 1}
	m.input = "header note"
	m.saveNote()

	text, count := m.notesMarkdown()
	if count != 2 {
		t.Fatalf("count=%d text=%s", count, text)
	}
	for _, want := range []string{"### `one.txt:1`", "### `one.txt:" + itoa(line) + "`", "> changed", "please rename this", "header note"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestNotesSurviveWorkingTreeRefresh(t *testing.T) {
	m := noteTestModel(t)
	m.pos = changedRowIndex(m)
	line := m.rows[m.pos].sourceLine
	m.startNote()
	m.input = "keep me"
	m.saveNote()

	// Change a different file and splice the refresh.
	writeFile(t, m.repo, "two.txt", "new file\n")
	snapshot, err := m.repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.previousChanges = append([]git.Change(nil), m.currentChanges()...)
	m.snapshot = snapshot
	m.refreshRows()
	count := 0
	for _, r := range m.rows {
		if r.kind == 'n' && strings.Contains(r.text, "keep me") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("note rows after refresh: %d", count)
	}
	if m.notes[noteRef{view: "worktree", path: "one.txt", line: line}] != "keep me" {
		t.Fatal("note was lost")
	}
}

func TestNoteEditorKeys(t *testing.T) {
	m := noteTestModel(t)
	m.pos = changedRowIndex(m)
	m.startNote()
	updated, _ := m.handleModal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = updated.(model)
	if m.input != "abc" || m.inputCursor != 3 {
		t.Fatalf("typing: %q %d", m.input, m.inputCursor)
	}
	updated, _ = m.handleModal(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.modal != "" {
		t.Fatal("esc did not close the note editor")
	}
}

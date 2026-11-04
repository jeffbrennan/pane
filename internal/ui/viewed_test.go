package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func viewedTestModel(t *testing.T, r git.Repo) model {
	t.Helper()
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(commit.Hash)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 40, wrap: true, screen: "commit", viewed: newViewedStore(r.Root),
		history: historyState{current: commit, meta: commit, changes: changes, request: 1}}
	m.history.loading = false
	m.rebuildCommit()
	return m
}

func TestViewedStoreRoundTripAndBlobInvalidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newViewedStore("/repo")
	if store.marked("commit:abc", "file.go", "blob1") {
		t.Fatal("fresh store is marked")
	}
	store.set("commit:abc", "file.go", "blob1", true)
	if !store.marked("commit:abc", "file.go", "blob1") {
		t.Fatal("mark did not persist")
	}
	if store.marked("commit:abc", "file.go", "blob2") {
		t.Fatal("a changed blob should clear the mark")
	}
	reloaded := newViewedStore("/repo")
	if !reloaded.marked("commit:abc", "file.go", "blob1") {
		t.Fatal("mark did not survive a reload")
	}
	store.set("commit:abc", "file.go", "blob1", false)
	if store.marked("commit:abc", "file.go", "blob1") {
		t.Fatal("unmark left the entry behind")
	}
}

func firstFileRow(rows []row) int {
	for i, row := range rows {
		if row.kind == 'f' {
			return i
		}
	}
	return -1
}

func TestToggleViewedCollapsesAndAdvances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := testRepo(t)
	writeFile(t, r, "one.go", "package one\n")
	writeFile(t, r, "two.go", "package two\n")
	if _, err := r.Commit("two files"); err != nil {
		t.Fatal(err)
	}
	m := viewedTestModel(t, r)
	pos := firstFileRow(m.history.rows)
	if pos < 0 || m.history.rows[pos].path != "one.go" {
		t.Fatalf("unexpected first file row: %#v", m.history.rows[pos])
	}
	m.history.pos = pos
	updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = updated.(model)
	if !m.history.rows[pos].viewed {
		t.Fatal("file was not marked viewed")
	}
	if m.history.rows[pos+1].path == "one.go" {
		t.Fatalf("viewed file did not collapse: %#v", m.history.rows[pos+1])
	}
	if m.history.rows[m.history.pos].path != "two.go" || m.history.rows[m.history.pos].kind != 'f' {
		t.Fatalf("cursor did not advance: row %#v", m.history.rows[m.history.pos])
	}
	identity := "commit:" + m.history.current.Hash
	blob := ""
	for _, c := range m.history.changes {
		if c.Path == "one.go" {
			blob = diffBlob(c.Diff)
		}
	}
	if !m.viewed.marked(identity, "one.go", blob) {
		t.Fatal("mark was not persisted")
	}
	if viewed, total := viewedCountRows(m.history.rows); viewed != 1 || total != 2 {
		t.Fatalf("viewed=%d total=%d", viewed, total)
	}
	if m.history.viewedFiles != 1 || m.history.totalFiles != 2 {
		t.Fatalf("badge counts viewed=%d total=%d", m.history.viewedFiles, m.history.totalFiles)
	}

	// Toggling again reopens the file.
	for i, row := range m.history.rows {
		if row.kind == 'f' && row.path == "one.go" {
			m.history.pos = i
		}
	}
	updated, _ = m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = updated.(model)
	if m.history.rows[0].viewed {
		t.Fatal("unmark left the row viewed")
	}
	if m.viewed.marked(identity, "one.go", blob) {
		t.Fatal("unmark was not persisted")
	}
	if m.history.viewedFiles != 0 || m.history.totalFiles != 2 {
		t.Fatalf("badge counts after unmark: viewed=%d total=%d", m.history.viewedFiles, m.history.totalFiles)
	}
}

func TestViewedMarkReappliesOnReopen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := testRepo(t)
	writeFile(t, r, "one.go", "package one\n")
	if _, err := r.Commit("one file"); err != nil {
		t.Fatal(err)
	}
	m := viewedTestModel(t, r)
	m.history.pos = firstFileRow(m.history.rows)
	updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = updated.(model)
	reopened := viewedTestModel(t, r)
	reopened.viewed = m.viewed
	reopened.rebuildCommit()
	pos := firstFileRow(reopened.history.rows)
	if !reopened.history.rows[pos].viewed {
		t.Fatal("reopening the commit lost the viewed mark")
	}
	count := 0
	for _, row := range reopened.history.rows {
		if row.path == "one.go" && row.kind != 's' {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reopened viewed file was not collapsed: %d rows", count)
	}
}

func TestPickerShowsAndHidesViewed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := testRepo(t)
	writeFile(t, r, "one.go", "package one\n")
	writeFile(t, r, "two.go", "package two\n")
	if _, err := r.Commit("two files"); err != nil {
		t.Fatal(err)
	}
	m := viewedTestModel(t, r)
	m.history.pos = firstFileRow(m.history.rows)
	updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = updated.(model)
	view := m.commitModel()
	view.openPicker()
	entries := view.fileEntries()
	if len(entries) != 2 || !entries[0].viewed {
		t.Fatalf("picker entries: %#v", entries)
	}
	if items := view.pickerItems(); len(items) != 2 {
		t.Fatalf("picker items: %#v", items)
	}
	view.handlePickerKey(keyMsg("v"))
	if items := view.pickerItems(); len(items) != 1 || items[0].path != "two.go" {
		t.Fatalf("hide viewed: %#v", items)
	}
	if shown := view.View(); !containsAll(shown, "1/2 viewed", "hiding viewed") {
		t.Fatalf("picker info: %q", shown)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

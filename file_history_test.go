package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func fileHistoryFixture(t *testing.T) repository {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "old name", "first line\nsecond line\nthird line\n")
	if _, err := r.commit("first"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "old name", "first line\nsecond line\nthird line\nfourth line\n")
	if _, err := r.commit("second"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("mv", "old name", "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.commit("rename"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "renamed", "first line\nsecond line\nthird line\nfourth line\nfifth line\n")
	writeFile(t, r, "another", "another file\n")
	if _, err := r.commit("latest"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFileHistoryFollowsRenamesAndPages(t *testing.T) {
	r := fileHistoryFixture(t)
	commits, err := r.historyWithPath(0, 10, "%aI", "renamed")
	if err != nil || len(commits) != 4 {
		t.Fatalf("file history: %#v %v", commits, err)
	}
	for i, want := range []struct{ subject, path string }{{"latest", "renamed"}, {"rename", "renamed"}, {"second", "old name"}, {"first", "old name"}} {
		if commits[i].subject != want.subject || commits[i].filePath != want.path {
			t.Errorf("commit %d: %#v", i, commits[i])
		}
	}
	page, err := r.historyWithPath(2, 2, "%aI", "renamed")
	if err != nil || len(page) != 2 || page[0].filePath != "old name" || page[0].subject != "second" {
		t.Fatalf("renamed history page: %#v %v", page, err)
	}
}

func TestFileHistoryReturnAndSelectHistoricalPath(t *testing.T) {
	r := fileHistoryFixture(t)
	writeFile(t, r, "renamed", "working changes\n")
	snapshot, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: snapshot, width: 90, height: 20, wrap: true}
	m.rebuild()
	for i, row := range m.rows {
		if row.path == "renamed" && row.kind == '+' {
			m.pos = i
			break
		}
	}
	original := m.rows[m.pos]
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	if cmd == nil || updated.(model).screen != "history" {
		t.Fatal("file history did not open")
	}
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	if m.history.path != "renamed" || len(m.history.commits) != 4 || !strings.Contains(m.View(), "history · renamed") {
		t.Fatalf("file history: %#v", m.history)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch := cmd().(tea.BatchMsg)
	updated, _ = updated.(model).Update(batch[0]())
	m = updated.(model)
	if len(m.history.rows) == 0 || m.history.rows[m.history.pos].path != "renamed" {
		t.Fatal("did not select the file in a multi-file commit")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	m.history.selected = 2
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch = cmd().(tea.BatchMsg)
	updated, _ = updated.(model).Update(batch[0]())
	m = updated.(model)
	if m.screen != "commit" || len(m.history.rows) == 0 || m.history.rows[m.history.pos].path != "old name" || m.history.rows[m.history.pos].kind != 'f' {
		t.Fatalf("did not select historical file: %#v", m.history)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != "history" || m.history.path != "renamed" {
		t.Fatal("commit did not return to file history")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != "" || len(m.rows) == 0 || m.rows[m.pos].path != original.path || m.rows[m.pos].kind != original.kind {
		t.Fatalf("did not restore original diff position: %#v", m)
	}
}

func TestFileHistoryCommandLine(t *testing.T) {
	r := fileHistoryFixture(t)
	path := filepath.Join(r.root, "renamed")
	for _, args := range [][]string{{"--", path}, {"log", "--", path}} {
		m, err := initialModel(r, args)
		if err != nil || m.screen != "history" || m.history.path != "renamed" {
			t.Fatalf("startup %q: %#v %v", args, m, err)
		}
		batch := m.Init()().(tea.BatchMsg)
		result := batch[2]().(historyLoaded)
		if result.err != nil || len(result.commits) != 4 {
			t.Fatalf("startup history: %#v", result)
		}
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("CLI file history did not quit")
		}
	}
	if _, err := initialModel(r, []string{"--", filepath.Join(t.TempDir(), "outside")}); err == nil {
		t.Fatal("accepted path outside repository")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFileHistoryRestoresCommitWithCurrentWhitespaceMode(t *testing.T) {
	r := fileHistoryFixture(t)
	commit, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges(commit.hash)
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, screen: "commit", width: 80, height: 18, history: historyState{current: commit, changes: changes}}
	m.rebuildCommit()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	m.repo.ignoreSpace = true
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != "commit" || !m.repo.ignoreSpace || !m.history.loading || cmd == nil {
		t.Fatal("original commit was not reloaded with the current whitespace mode")
	}
	result, ok := cmd().(commitStreamLoaded)
	if !ok || result.chunk.err != nil {
		t.Fatalf("reloading commit: %#v", result)
	}
	m.cancelCommitLoad()
}

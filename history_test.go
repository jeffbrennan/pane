package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHistoryAndCommitChanges(t *testing.T) {
	r := testRepo(t)
	if commits, err := r.history(); err != nil || len(commits) != 0 {
		t.Fatalf("empty history: %#v %v", commits, err)
	}
	writeFile(t, r, "first file", "initial\n")
	if _, err := r.commit("first commit"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "first file", "updated\n")
	writeFile(t, r, "second", "new\n")
	if _, err := r.commit("second commit\n\nDetailed body"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history()
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].subject != "second commit" || commits[1].author != "Test" || commits[0].email != "test@example.com" {
		t.Fatalf("history: %#v", commits)
	}
	if !strings.Contains(commits[0].message, "Detailed body") {
		t.Fatalf("missing body: %#v", commits[0])
	}
	changes, err := r.commitChanges(commits[0].hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || !strings.Contains(changes[0].diff, "+updated") {
		t.Fatalf("changes: %#v", changes)
	}
	changes, err = r.commitChanges(commits[1].hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.Contains(changes[0].diff, "+initial") {
		t.Fatalf("root changes: %#v", changes)
	}
}

func TestHistoryMergeFirstParent(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base", "base\n")
	if _, err := r.commit("base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "feature", "feature\n")
	if _, err := r.commit("feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("checkout", "-"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "main", "main\n")
	if _, err := r.commit("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("merge", "--no-ff", "-m", "merge", "feature"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges(commits[0].hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].path != "feature" || !strings.Contains(changes[0].diff, "+feature") {
		t.Fatalf("merge changes: %#v", changes)
	}
}

func TestHistorySearchAndReadOnlyNavigation(t *testing.T) {
	m := model{width: 60, height: 10, wrap: true, screen: "history", history: historyState{commits: []commitInfo{
		{hash: "abc123456789", author: "Alice", email: "a@example.com", subject: "One"},
		{hash: "def123456789", author: "Bob", email: "b@example.com", subject: "Fix issue", message: "Fix issue\n\nbody detail"},
	}}}
	for _, query := range []string{"ALICE", "example.com", "fix", "def123", "body detail"} {
		m.history.filter = query
		if got := m.filteredHistory(); len(got) == 0 {
			t.Fatalf("no result for %q", query)
		}
	}
	m.history.filter = ""
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix")})
	m = updated.(model)
	if len(m.filteredHistory()) != 1 || !strings.Contains(m.historyView(), "Fix issue") {
		t.Fatal("live search failed")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.history.searching || m.history.filter != "fix" {
		t.Fatal("search not applied")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != "commit" || cmd == nil || m.history.current.author != "Bob" {
		t.Fatal("commit not opened")
	}
	for _, key := range []string{" ", "c", "p", "P"} {
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil || updated.(model).screen != "commit" {
			t.Fatalf("mutating key %q accepted", key)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).screen != "history" {
		t.Fatal("back to history failed")
	}
}

func TestHistorySelectionPresentationAndScrolling(t *testing.T) {
	m := model{width: 60, height: 9, screen: "history", history: historyState{commits: []commitInfo{
		{hash: "aaaaaaaa", author: "Alice", subject: "first"},
		{hash: "bbbbbbbb", author: "Bob", subject: "second"},
		{hash: "cccccccc", author: "Carol", subject: "third"},
	}}}
	view := m.historyView()
	if !strings.Contains(view, "\x1b[1m▸ aaaaaaaa") || !strings.Contains(view, "\x1b[2m  bbbbbbbb") || !strings.Contains(view, "first\x1b[0m\n\n") {
		t.Fatalf("missing selection styles or spacing: %q", view)
	}
	m.history.selected = 2
	view = m.historyView()
	if !strings.Contains(view, "\x1b[1m▸ cccccccc") || !strings.Contains(view, "third") {
		t.Fatalf("selected commit not visible: %q", view)
	}
}

func TestSuccessfulCommitOpensHistory(t *testing.T) {
	m := model{width: 60, height: 10, busy: true, history: historyState{selected: 5, filter: "old"}}
	updated, cmd := m.Update(completed{message: "[main abc] message", commitMessage: "message"})
	got := updated.(model)
	if got.screen != "history" || got.history.selected != 0 || got.history.filter != "" || !got.history.loading || cmd == nil {
		t.Fatalf("history did not open after commit: %#v", got)
	}
}

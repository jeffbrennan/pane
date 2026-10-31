package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInitialModelOpensCommitRevision(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "first\n")
	if _, err := r.commit("first commit"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("tag", "-a", "release", "-m", "release"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "second\n")
	if _, err := r.commit("second commit"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "uncommitted\n")
	for _, revision := range []string{hash, hash[:8], "release", "HEAD~1"} {
		t.Run(revision, func(t *testing.T) {
			m, err := initialModel(r, []string{revision})
			if err != nil {
				t.Fatal(err)
			}
			if m.screen != "commit" || !m.history.loading || m.history.current.hash != hash || m.history.current.subject != "first commit" {
				t.Fatalf("incorrect startup model: %#v", m.history)
			}
			batch, ok := m.Init()().(tea.BatchMsg)
			if !ok || len(batch) != 3 {
				t.Fatalf("missing startup commands: %#v", batch)
			}
			result, ok := batch[2]().(commitLoaded)
			if !ok || result.err != nil || len(result.changes) != 1 || !strings.Contains(result.changes[0].diff, "+first") {
				t.Fatalf("incorrect commit diff: %#v", result)
			}
			m.width, m.height = 60, 12
			updated, _ := m.Update(result)
			m = updated.(model)
			if !strings.Contains(m.View(), "first commit") || !strings.Contains(m.View(), "first") {
				t.Fatalf("missing commit review: %q", m.View())
			}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if got := updated.(model); got.screen != "history" || !got.history.loading || cmd == nil {
				t.Fatalf("history not loaded on return: %#v", got.history)
			}
		})
	}
}

func TestInitialModelRejectsInvalidCommitArguments(t *testing.T) {
	r := testRepo(t)
	for _, args := range [][]string{{"missing"}, {""}, {"--help"}, {"HEAD", "extra"}} {
		if _, err := initialModel(r, args); err == nil {
			t.Fatalf("accepted invalid arguments: %q", args)
		}
	}
	writeFile(t, r, "file", "text\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	blob, err := r.action("rev-parse", "HEAD:file")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initialModel(r, []string{blob}); err == nil {
		t.Fatal("accepted a non-commit object")
	}
}

func TestDirectCommitBackBeforeLoading(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "text\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	m, err := initialModel(r, []string{"HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	request := m.history.request
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != "history" || cmd == nil || m.history.request == request {
		t.Fatal("back did not load history and invalidate the commit request")
	}
	updated, _ = m.Update(commitLoaded{request: request, changes: []change{{path: "stale"}}})
	if got := updated.(model); got.screen != "history" || len(got.history.changes) != 0 {
		t.Fatal("stale commit response changed history")
	}
}

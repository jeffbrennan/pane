package ui

import (
	"fmt"
	"github.com/jeffbrennan/pane/internal/git"
	"strings"
	"testing"
)

func TestStepBetweenCommits(t *testing.T) {
	commits := []git.CommitInfo{{Hash: "newest"}, {Hash: "middle"}, {Hash: "oldest"}}
	m := model{width: 80, height: 20, wrap: true, screen: "commit", history: historyState{commits: commits, current: commits[0]}}
	updated, _ := m.stepCommit(1)
	next := updated.(model)
	if next.history.current.Hash != "middle" || next.screen != "commit" {
		t.Fatalf("next commit: %#v", next.history.current)
	}
	next.history.loading = false
	updated, _ = next.stepCommit(1)
	next = updated.(model)
	if next.history.current.Hash != "oldest" {
		t.Fatalf("second step: %#v", next.history.current)
	}
	next.history.loading = false
	updated, _ = next.stepCommit(1)
	next = updated.(model)
	if next.history.current.Hash != "oldest" || next.flash == "" {
		t.Fatalf("step past the end: %#v %q", next.history.current, next.flash)
	}
}

func TestStepAtLoadedEndFetchesPage(t *testing.T) {
	commits := []git.CommitInfo{{Hash: "a"}, {Hash: "b"}}
	m := model{width: 80, height: 20, wrap: true, screen: "commit", history: historyState{commits: commits, current: commits[1], more: true}}
	updated, cmd := m.stepCommit(1)
	next := updated.(model)
	if cmd == nil || next.history.pendingStep != 1 || !next.history.fetching {
		t.Fatalf("did not wait for the next page: %#v", next.history)
	}
}

func TestStepRestoresPosition(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", strings.Repeat("line\n", 20))
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.Repeat("line\n", 20), "\n")
	lines[9] = "changed"
	writeFile(t, r, "file.txt", strings.Join(lines, "\n"))
	if _, err := r.Commit("second"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, git.HistoryPage)
	if err != nil || len(commits) != 2 {
		t.Fatalf("history: %v %v", commits, err)
	}
	m := model{width: 80, height: 30, wrap: true, screen: "commit", history: historyState{commits: commits, current: commits[0], request: 1, streamActive: true}}
	m.history.loading = false
	changes, err := r.CommitChanges(commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	m.history.changes = changes
	m.rebuildCommit()
	target := -1
	for i, row := range m.history.rows {
		if row.sourceLine == 8 && row.kind == ' ' {
			target = i
		}
	}
	if target < 0 {
		t.Fatal("fixture has no context line 8")
	}
	m.history.pos = target
	m.saveCommitPosition()

	updated, _ := m.stepCommit(1)
	next := updated.(model)
	changes, err = r.CommitChanges(next.history.current.Hash)
	if err != nil {
		t.Fatal(err)
	}
	next = deliverChunk(t, next, git.CommitChunk{Paths: []string{"file.txt"}, Changes: changes, Loaded: 1, Done: true})
	if next.history.current.Hash != commits[1].Hash {
		t.Fatalf("stepped to %s", next.history.current.Hash)
	}

	updated, _ = next.stepCommit(-1)
	back := updated.(model)
	changes, err = r.CommitChanges(back.history.current.Hash)
	if err != nil {
		t.Fatal(err)
	}
	back = deliverChunk(t, back, git.CommitChunk{Paths: []string{"file.txt"}, Changes: changes, Loaded: 1, Done: true})
	if back.history.pos >= len(back.history.rows) || back.history.rows[back.history.pos].sourceLine != 8 {
		t.Fatalf("position not restored: row %#v", back.history.rows[back.history.pos])
	}
}

func TestStepKeepsFileInFileHistory(t *testing.T) {
	commits := []git.CommitInfo{{Hash: "newer", FilePath: "file.txt"}, {Hash: "older", FilePath: "file.txt"}}
	m := model{width: 80, height: 20, wrap: true, screen: "commit", history: historyState{commits: commits, current: commits[0], path: "file.txt"}}
	updated, _ := m.stepCommit(1)
	next := updated.(model)
	if next.picker.pendingPath != "file.txt" {
		t.Fatalf("file history step did not keep the file: %q", next.picker.pendingPath)
	}
	if index, total := next.commitListPosition(); index != 2 || total != 2 {
		t.Fatalf("position %d/%d", index, total)
	}
}

func TestCommitTitleShowsListPosition(t *testing.T) {
	commits := []git.CommitInfo{{Hash: "aaaa", Subject: "one"}, {Hash: "bbbb", Subject: "two"}}
	m := model{width: 120, height: 30, wrap: true, screen: "commit", history: historyState{commits: commits, current: commits[1]}}
	view := m.commitModel()
	if !strings.Contains(view.viewTitle, fmt.Sprintf("%d/%d", 2, 2)) {
		t.Fatalf("title %q", view.viewTitle)
	}
}

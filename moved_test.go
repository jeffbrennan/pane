package main

import (
	"github.com/jeffbrennan/pane/internal/git"
	"strings"
	"testing"
)

func movedFixture(t *testing.T) (model, []git.Change) {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "a.go", "package a\n\nfunc one() {}\nfunc two() {}\nfunc three() {}\nfunc four() {}\n")
	writeFile(t, r, "b.go", "package b\n")
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "a.go", "package a\n")
	writeFile(t, r, "b.go", "package b\n\nfunc one() {}\nfunc two() {}\nfunc three() {}\nfunc four() {}\n")
	if _, err := r.Commit("move"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(commit.Hash)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 40, wrap: true, screen: "commit", repo: r,
		history: historyState{current: commit, meta: commit, changes: changes, request: 1}}
	m.history.loading = false
	m.rebuildCommit()
	return m, changes
}

func TestMovedIndexFindsBlocksLazily(t *testing.T) {
	m, changes := movedFixture(t)
	index := newMovedIndex()
	index.addChanges(changes)
	removed := rowsForKind(m.history.rows, "a.go", '-')
	added := rowsForKind(m.history.rows, "b.go", '+')
	if len(removed) == 0 || len(added) == 0 {
		t.Fatalf("fixture rows: removed=%d added=%d", len(removed), len(added))
	}
	if moved, _, _ := index.lookup(removed[0]); !moved {
		t.Fatal("removed block was not recognised as moved")
	}
	moved, hint, show := index.lookup(added[len(added)-1])
	if !moved || !show || !strings.Contains(hint, "moved from a.go") {
		t.Fatalf("added block: moved=%v show=%v hint=%q", moved, show, hint)
	}
}

func TestMovedHintRendersOnlyWhenVisible(t *testing.T) {
	m, _ := movedFixture(t)
	view := m.commitModel()
	rendered := view.View()
	if !strings.Contains(rendered, "moved from a.go:2") {
		t.Fatalf("missing moved hint: %q", rendered)
	}
	if view.history.moved == nil {
		t.Fatal("moved index was not built")
	}

	// A viewport that ends before the added block does not render its hint.
	view.pos, view.viewTop = 0, 0
	view.height = 6
	if strings.Contains(view.View(), "moved from") {
		t.Fatal("hint rendered for an off-screen block")
	}
}

func TestShortMovedRunsStayNormal(t *testing.T) {
	index := newMovedIndex()
	index.addChange(git.Change{Path: "b", Diff: "@@ -1,2 +1,2 @@\n-one\n-two\n+one\n+two\n"})
	row := row{path: "b", kind: '+', target: 1, sourceLine: 1, text: "one"}
	if moved, _, _ := index.lookup(row); moved {
		t.Fatal("a two-line move should not be marked")
	}
}

func TestWorkingTreeIgnoresMovedIndex(t *testing.T) {
	m := model{width: 100, height: 20, wrap: true, history: historyState{moved: newMovedIndex()}}
	row := row{path: "a", kind: '+', target: 1, text: "one"}
	if moved, _, _ := m.movedFor(row); moved {
		t.Fatal("working-tree rows should not use the commit moved index")
	}
}

func rowsForKind(rows []row, path string, kind byte) []row {
	var matches []row
	for _, row := range rows {
		if row.path == path && row.kind == kind {
			matches = append(matches, row)
		}
	}
	return matches
}

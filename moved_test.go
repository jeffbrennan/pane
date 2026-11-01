package main

import (
	"strings"
	"testing"
)

func TestMovedBlocksAreDimmedAndHinted(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "a.go", "package a\n\nfunc one() {}\nfunc two() {}\nfunc three() {}\nfunc four() {}\n")
	writeFile(t, r, "b.go", "package b\n")
	if _, err := r.commit("first"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "a.go", "package a\n")
	writeFile(t, r, "b.go", "package b\n\nfunc one() {}\nfunc two() {}\nfunc three() {}\nfunc four() {}\n")
	if _, err := r.commit("move"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges(commit.hash)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 40, wrap: true, screen: "commit", repo: r,
		history: historyState{current: commit, meta: commit, changes: changes, request: 1}}
	m.history.loading = false
	m.rebuildCommit()
	moved, hinted := 0, false
	for _, row := range m.history.rows {
		if row.isMoved() {
			moved++
		}
		if row.kind == 'm' && strings.Contains(row.text, "moved from") {
			hinted = true
		}
	}
	if moved < 8 {
		t.Fatalf("only %d rows marked moved", moved)
	}
	if !hinted {
		t.Fatal("missing moved-from hint")
	}
}

func TestShortMovedRunsStayNormal(t *testing.T) {
	rows := []row{
		{path: "a", kind: '-', sourceLine: 1, offset: 0, text: "one"},
		{path: "a", kind: '-', sourceLine: 2, offset: 0, text: "two"},
		{path: "b", kind: '+', sourceLine: 1, offset: 0, target: 1, text: "one"},
		{path: "b", kind: '+', sourceLine: 2, offset: 0, target: 2, text: "two"},
	}
	marked := markMovedBlocks(rows)
	for _, row := range marked {
		if row.isMoved() {
			t.Fatal("a two-line move should not be marked")
		}
	}
}

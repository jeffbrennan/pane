package ui

import (
	"context"
	"fmt"

	"github.com/jeffbrennan/pane/internal/git"
	"sync/atomic"
	"testing"
)

func syntaxBatchFixture(t *testing.T) (git.Repo, []git.Change) {
	t.Helper()
	r := testRepo(t)
	paths := []string{"a.go", "b.go", "c.go", "space name.go", "q\"t.go", "é.go", "old.go"}
	for i, path := range paths {
		writeFile(t, r, path, fmt.Sprintf("package main\nvar value%d = %d\n", i, i))
	}
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths[:len(paths)-1] {
		writeFile(t, r, path, fmt.Sprintf("package main\nvar value%d = %d\n", i, i+100))
	}
	if out, err := r.Command("mv", "old.go", "new.go"); err != nil {
		t.Fatalf("rename: %s: %v", out, err)
	}
	writeFile(t, r, "added.go", "package main\nvar added = true\n")
	if out, err := r.Command("add", "--all"); err != nil {
		t.Fatalf("stage changes: %s: %v", out, err)
	}
	if _, err := r.Commit("changes"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return r, changes
}

func TestSyntaxDemandBatchesOnlyMatchingContexts(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	var regular []git.Change
	for _, c := range changes {
		if c.Path == "a.go" || c.Path == "b.go" {
			regular = append(regular, c)
		}
	}
	if len(regular) != 2 {
		t.Fatal("missing fixture changes")
	}
	targets := []syntaxTarget{
		{key: git.Context{Commit: "HEAD", Path: regular[0].Path}, change: regular[0]},
		{key: git.Context{Commit: "HEAD~1", Path: regular[0].Path}, change: regular[0]},
		{key: git.Context{Commit: "HEAD", Path: regular[1].Path}, change: regular[1]},
		{key: git.Context{Commit: "HEAD~1", Path: regular[1].Path}, change: regular[1]},
	}
	want := make([]string, len(targets))
	for i, target := range targets {
		var err error
		want[i], err = r.FullContext(target.key, target.change.Diff, target.change.OldPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands atomic.Int64
	r.CommandHook = func([]string) { commands.Add(1) }
	m := model{repo: r, screen: "sequence"}
	msg, ok := m.loadSyntaxTargets(targets)().(syntaxDemandLoaded)
	if !ok || len(msg.results) != len(targets) || commands.Load() != 2 {
		t.Fatalf("results=%d commands=%d", len(msg.results), commands.Load())
	}
	for i, result := range msg.results {
		if result.key != targets[i].key || result.syntaxDiff != want[i] {
			t.Fatalf("result %d does not match its individual context", i)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.screen = "commit"
	m.history.commitContext = ctx
	msg = m.loadSyntaxTargets(targets)().(syntaxDemandLoaded)
	for _, result := range msg.results {
		if !result.cancelled {
			t.Fatal("cancelled syntax target was not marked cancelled")
		}
	}
}

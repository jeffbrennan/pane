package git

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func syntaxBatchFixture(t *testing.T) (Repo, []Change) {
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
	changes, err := r.CommitChangesWithoutSyntax("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return r, changes
}

func TestFullContextsMatchesIndividualCommitLoads(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	key := Context{Commit: "HEAD"}
	want := make([]string, len(changes))
	for i, c := range changes {
		fileKey := key
		fileKey.Path = c.Path
		var err error
		want[i], err = r.FullContext(fileKey, c.Diff, c.OldPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands atomic.Int64
	r.CommandHook = func([]string) { commands.Add(1) }
	got := r.FullContexts(changes, key)
	if commands.Load() != 4 {
		t.Fatalf("got %d Git commands, want one batch and three fallbacks", commands.Load())
	}
	for i, result := range got {
		if result.Err != nil || result.Diff != want[i] {
			t.Fatalf("%s differs from individual load: %v\ngot %q\nwant %q", changes[i].Path, result.Err, result.Diff, want[i])
		}
	}
}

func TestFullContextsMatchesWorkingAndStagedLoads(t *testing.T) {
	r, _ := syntaxBatchFixture(t)
	paths := []string{"a.go", "b.go", "c.go", "space name.go"}
	for _, path := range paths {
		writeFile(t, r, path, "package main\nvar staged = 1\n")
	}
	if out, err := r.Command("add", "--all"); err != nil {
		t.Fatalf("stage: %s: %v", out, err)
	}
	for _, path := range paths {
		writeFile(t, r, path, "package main\nvar working = 2\n")
	}
	for _, key := range []Context{{}, {Staged: true}, {Base: true, Commit: "HEAD~1"}} {
		changes := make([]Change, len(paths))
		want := make([]string, len(paths))
		for i, path := range paths {
			diff, err := r.diffPaths(path, "", key.Staged)
			if err != nil {
				t.Fatal(err)
			}
			changes[i] = Change{Path: path, Diff: diff}
			fileKey := key
			fileKey.Path = path
			want[i], err = r.FullContext(fileKey, diff, "")
			if err != nil {
				t.Fatal(err)
			}
		}
		var commands atomic.Int64
		r.CommandHook = func([]string) { commands.Add(1) }
		got := r.FullContexts(changes, key)
		r.CommandHook = nil
		if commands.Load() != 1 {
			t.Fatalf("context %#v: got %d commands, want 1", key, commands.Load())
		}
		for i, result := range got {
			if result.Err != nil || result.Diff != want[i] {
				t.Fatalf("context %#v file %s differs: %v", key, paths[i], result.Err)
			}
		}
	}
}

func TestFullContextsFallsBackForMissingAndCancelledPaths(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	changes = append(changes, Change{Path: "missing.go", Diff: "whitespace-only"})
	got := r.FullContexts(changes, Context{Commit: "HEAD"})
	if result := got[len(got)-1]; result.Err != nil || result.Diff != "" {
		t.Fatalf("missing path fallback: %#v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Context = ctx
	got = r.FullContexts(changes, Context{Commit: "HEAD"})
	for i, c := range changes {
		if strings.Contains(c.Diff, "\nnew file mode ") {
			continue
		}
		if got[i].Err == nil {
			t.Fatalf("cancelled load for %s succeeded", c.Path)
		}
	}
}

func TestFullContextsFallsBackForCustomDiffPrefixes(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	if out, err := r.Command("config", "diff.noprefix", "true"); err != nil {
		t.Fatalf("configure prefixes: %s: %v", out, err)
	}
	var regular []Change
	for _, c := range changes {
		if c.Path == "a.go" || c.Path == "b.go" || c.Path == "c.go" {
			regular = append(regular, c)
		}
	}
	key := Context{Commit: "HEAD"}
	want := make([]string, len(regular))
	for i, c := range regular {
		fileKey := key
		fileKey.Path = c.Path
		var err error
		want[i], err = r.FullContext(fileKey, c.Diff, c.OldPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands atomic.Int64
	r.CommandHook = func([]string) { commands.Add(1) }
	got := r.FullContexts(regular, key)
	if commands.Load() != int64(len(regular)+1) {
		t.Fatalf("got %d Git commands, want batch plus individual fallbacks", commands.Load())
	}
	for i, result := range got {
		if result.Err != nil || result.Diff != want[i] {
			t.Fatalf("custom-prefix context for %s differs: %v", regular[i].Path, result.Err)
		}
	}
}

func TestFullContextsLimitsBatchSize(t *testing.T) {
	r := testRepo(t)
	const files = 65
	for i := 0; i < files; i++ {
		writeFile(t, r, fmt.Sprintf("file-%03d.go", i), "package main\nvar value = 1\n")
	}
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < files; i++ {
		writeFile(t, r, fmt.Sprintf("file-%03d.go", i), "package main\nvar value = 2\n")
	}
	if _, err := r.Commit("changes"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChangesWithoutSyntax("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var commands atomic.Int64
	r.CommandHook = func([]string) { commands.Add(1) }
	got := r.FullContexts(changes, Context{Commit: "HEAD"})
	if len(got) != files || commands.Load() != 3 {
		t.Fatalf("got %d contexts from %d commands, want %d from 3", len(got), commands.Load(), files)
	}
	for i, result := range got {
		if result.Err != nil || !strings.Contains(result.Diff, "+var value = 2") {
			t.Fatalf("file %d: %v, %q", i, result.Err, result.Diff)
		}
	}
}

func TestSyntaxDiffsStopAtBudget(t *testing.T) {
	huge := "@@ -0,0 +1 @@\n" + strings.Repeat("+x\n", SyntaxBudgetLines)
	// The first file is over the budget, so neither file asks Git for context.
	diffs := Repo{}.SyntaxDiffs([]Change{{Path: "a.go", Diff: huge}, {Path: "b.go", Diff: "@@ -0,0 +1 @@\n+x\n"}}, Context{})
	if diffs[0] != "" || diffs[1] != "" {
		t.Fatalf("highlighted past the budget: %q", diffs)
	}
}

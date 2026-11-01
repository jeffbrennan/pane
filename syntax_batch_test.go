package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func syntaxBatchFixture(t *testing.T) (repository, []change) {
	t.Helper()
	r := testRepo(t)
	paths := []string{"a.go", "b.go", "c.go", "space name.go", "q\"t.go", "é.go", "old.go"}
	for i, path := range paths {
		writeFile(t, r, path, fmt.Sprintf("package main\nvar value%d = %d\n", i, i))
	}
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths[:len(paths)-1] {
		writeFile(t, r, path, fmt.Sprintf("package main\nvar value%d = %d\n", i, i+100))
	}
	if out, err := r.command("mv", "old.go", "new.go"); err != nil {
		t.Fatalf("rename: %s: %v", out, err)
	}
	writeFile(t, r, "added.go", "package main\nvar added = true\n")
	if out, err := r.command("add", "--all"); err != nil {
		t.Fatalf("stage changes: %s: %v", out, err)
	}
	if _, err := r.commit("changes"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChangesWithoutSyntax("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return r, changes
}

func TestFullContextsMatchesIndividualCommitLoads(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	key := contextKey{commit: "HEAD"}
	want := make([]string, len(changes))
	for i, c := range changes {
		fileKey := key
		fileKey.path = c.path
		var err error
		want[i], err = r.fullContext(fileKey, c.diff, c.oldPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands atomic.Int64
	r.commandHook = func([]string) { commands.Add(1) }
	got := r.fullContexts(changes, key)
	if commands.Load() != 4 {
		t.Fatalf("got %d Git commands, want one batch and three fallbacks", commands.Load())
	}
	for i, result := range got {
		if result.err != nil || result.diff != want[i] {
			t.Fatalf("%s differs from individual load: %v\ngot %q\nwant %q", changes[i].path, result.err, result.diff, want[i])
		}
	}
}

func TestFullContextsMatchesWorkingAndStagedLoads(t *testing.T) {
	r, _ := syntaxBatchFixture(t)
	paths := []string{"a.go", "b.go", "c.go", "space name.go"}
	for _, path := range paths {
		writeFile(t, r, path, "package main\nvar staged = 1\n")
	}
	if out, err := r.command("add", "--all"); err != nil {
		t.Fatalf("stage: %s: %v", out, err)
	}
	for _, path := range paths {
		writeFile(t, r, path, "package main\nvar working = 2\n")
	}
	for _, key := range []contextKey{{}, {staged: true}, {base: true, commit: "HEAD~1"}} {
		changes := make([]change, len(paths))
		want := make([]string, len(paths))
		for i, path := range paths {
			diff, err := r.diffPaths(path, "", key.staged)
			if err != nil {
				t.Fatal(err)
			}
			changes[i] = change{path: path, diff: diff}
			fileKey := key
			fileKey.path = path
			want[i], err = r.fullContext(fileKey, diff, "")
			if err != nil {
				t.Fatal(err)
			}
		}
		var commands atomic.Int64
		r.commandHook = func([]string) { commands.Add(1) }
		got := r.fullContexts(changes, key)
		r.commandHook = nil
		if commands.Load() != 1 {
			t.Fatalf("context %#v: got %d commands, want 1", key, commands.Load())
		}
		for i, result := range got {
			if result.err != nil || result.diff != want[i] {
				t.Fatalf("context %#v file %s differs: %v", key, paths[i], result.err)
			}
		}
	}
}

func TestFullContextsFallsBackForMissingAndCancelledPaths(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	changes = append(changes, change{path: "missing.go", diff: "whitespace-only"})
	got := r.fullContexts(changes, contextKey{commit: "HEAD"})
	if result := got[len(got)-1]; result.err != nil || result.diff != "" {
		t.Fatalf("missing path fallback: %#v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.context = ctx
	got = r.fullContexts(changes, contextKey{commit: "HEAD"})
	for i, c := range changes {
		if strings.Contains(c.diff, "\nnew file mode ") {
			continue
		}
		if got[i].err == nil {
			t.Fatalf("cancelled load for %s succeeded", c.path)
		}
	}
}

func TestFullContextsFallsBackForCustomDiffPrefixes(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	if out, err := r.command("config", "diff.noprefix", "true"); err != nil {
		t.Fatalf("configure prefixes: %s: %v", out, err)
	}
	var regular []change
	for _, c := range changes {
		if c.path == "a.go" || c.path == "b.go" || c.path == "c.go" {
			regular = append(regular, c)
		}
	}
	key := contextKey{commit: "HEAD"}
	want := make([]string, len(regular))
	for i, c := range regular {
		fileKey := key
		fileKey.path = c.path
		var err error
		want[i], err = r.fullContext(fileKey, c.diff, c.oldPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands atomic.Int64
	r.commandHook = func([]string) { commands.Add(1) }
	got := r.fullContexts(regular, key)
	if commands.Load() != int64(len(regular)+1) {
		t.Fatalf("got %d Git commands, want batch plus individual fallbacks", commands.Load())
	}
	for i, result := range got {
		if result.err != nil || result.diff != want[i] {
			t.Fatalf("custom-prefix context for %s differs: %v", regular[i].path, result.err)
		}
	}
}

func TestFullContextsLimitsBatchSize(t *testing.T) {
	r := testRepo(t)
	const files = 65
	for i := 0; i < files; i++ {
		writeFile(t, r, fmt.Sprintf("file-%03d.go", i), "package main\nvar value = 1\n")
	}
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < files; i++ {
		writeFile(t, r, fmt.Sprintf("file-%03d.go", i), "package main\nvar value = 2\n")
	}
	if _, err := r.commit("changes"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChangesWithoutSyntax("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var commands atomic.Int64
	r.commandHook = func([]string) { commands.Add(1) }
	got := r.fullContexts(changes, contextKey{commit: "HEAD"})
	if len(got) != files || commands.Load() != 3 {
		t.Fatalf("got %d contexts from %d commands, want %d from 3", len(got), commands.Load(), files)
	}
	for i, result := range got {
		if result.err != nil || !strings.Contains(result.diff, "+var value = 2") {
			t.Fatalf("file %d: %v, %q", i, result.err, result.diff)
		}
	}
}

func TestSyntaxDemandBatchesOnlyMatchingContexts(t *testing.T) {
	r, changes := syntaxBatchFixture(t)
	var regular []change
	for _, c := range changes {
		if c.path == "a.go" || c.path == "b.go" {
			regular = append(regular, c)
		}
	}
	if len(regular) != 2 {
		t.Fatal("missing fixture changes")
	}
	targets := []syntaxTarget{
		{key: contextKey{commit: "HEAD", path: regular[0].path}, change: regular[0]},
		{key: contextKey{commit: "HEAD~1", path: regular[0].path}, change: regular[0]},
		{key: contextKey{commit: "HEAD", path: regular[1].path}, change: regular[1]},
		{key: contextKey{commit: "HEAD~1", path: regular[1].path}, change: regular[1]},
	}
	want := make([]string, len(targets))
	for i, target := range targets {
		var err error
		want[i], err = r.fullContext(target.key, target.change.diff, target.change.oldPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands atomic.Int64
	r.commandHook = func([]string) { commands.Add(1) }
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCachedRefresh(t *testing.T) {
	r := testRepo(t)
	for _, path := range []string{"first", "second"} {
		writeFile(t, r, path, "initial\n")
	}
	if _, err := r.action("add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"first", "second"} {
		writeFile(t, r, path, "modified\n")
		old := time.Now().Add(-10 * time.Second)
		if err := os.Chtimes(filepath.Join(r.root, path), old, old); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var commands [][]string
	r.commandHook = func(args []string) {
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, append([]string(nil), args...))
	}
	cache, _, err := r.loadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	commands = nil
	cache, unchanged, err := r.loadCached(cache)
	if err != nil || !unchanged {
		t.Fatalf("unchanged = %v, err = %v", unchanged, err)
	}
	if len(commands) != 1 || commands[0][0] != "status" {
		t.Fatalf("commands: %v", commands)
	}
	writeFile(t, r, "first", "different content\n")
	commands = nil
	cache, unchanged, err = r.loadCached(cache)
	if err != nil || unchanged {
		t.Fatalf("changed: unchanged = %v, err = %v", unchanged, err)
	}
	for _, args := range commands {
		if len(args) > 0 && args[0] == "diff" && !strings.Contains(strings.Join(args, " "), "-- first") {
			t.Fatalf("unexpected diff: %v", args)
		}
	}
	if err := r.stage("first", false); err != nil {
		t.Fatal(err)
	}
	cache, _, err = r.loadCached(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.snapshot.staged) != 1 || cache.snapshot.staged[0].path != "first" {
		t.Fatalf("staged: %#v", cache.snapshot.staged)
	}
}

func TestCachedRefreshRacyWrite(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new", "first\n")
	path := filepath.Join(r.root, "new")
	stamp := time.Now()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	cache, _, err := r.loadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "new", "other\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	cache, unchanged, err := r.loadCached(cache)
	if err != nil || unchanged || !strings.Contains(cache.snapshot.unstaged[0].diff, "+other") {
		t.Fatalf("racy refresh: unchanged=%v err=%v cache=%#v", unchanged, err, cache)
	}
}

func TestStaleUnchangedRefreshDoesNotReplaceCache(t *testing.T) {
	current := &refreshCache{}
	m := model{generation: 2, loading: true, refreshCache: current}
	updated, cmd := m.Update(loadedUnchanged{generation: 1, cache: &refreshCache{}})
	next := updated.(model)
	if next.refreshCache != current || !next.loading || cmd == nil {
		t.Fatal("stale refresh should trigger a new load without replacing the cache")
	}
}

func BenchmarkIdleRefresh(b *testing.B) {
	r := repository{root: b.TempDir()}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := r.command(args...); err != nil {
			b.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	write := func(path, content string) {
		b.Helper()
		name := filepath.Join(r.root, path)
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			b.Fatal(err)
		}
		old := time.Now().Add(-10 * time.Second)
		if err := os.Chtimes(name, old, old); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < 50; i++ {
		write(fmt.Sprintf("tracked-%03d", i), "initial\n")
	}
	if _, err := r.action("add", "--all"); err != nil {
		b.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		write(fmt.Sprintf("tracked-%03d", i), "modified\n")
	}
	for i := 0; i < 200; i++ {
		write(fmt.Sprintf("untracked-%03d", i), "new\n")
	}
	cache, _, err := r.loadCached(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := r.load(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var err error
			cache, _, err = r.loadCached(cache)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

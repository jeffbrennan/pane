package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestCachedRefresh(t *testing.T) {
	r := testRepo(t)
	for _, path := range []string{"first", "second"} {
		writeFile(t, r, path, "initial\n")
	}
	if _, err := r.Action("add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"first", "second"} {
		writeFile(t, r, path, "modified\n")
		old := time.Now().Add(-10 * time.Second)
		if err := os.Chtimes(filepath.Join(r.Root, path), old, old); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var commands [][]string
	r.CommandHook = func(args []string) {
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, append([]string(nil), args...))
	}
	cache, _, err := r.LoadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	commands = nil
	cache, unchanged, err := r.LoadCached(cache)
	if err != nil || !unchanged {
		t.Fatalf("unchanged = %v, err = %v", unchanged, err)
	}
	if len(commands) != 1 || commands[0][0] != "status" {
		t.Fatalf("commands: %v", commands)
	}
	writeFile(t, r, "first", "different content\n")
	commands = nil
	cache, unchanged, err = r.LoadCached(cache)
	if err != nil || unchanged {
		t.Fatalf("changed: unchanged = %v, err = %v", unchanged, err)
	}
	for _, args := range commands {
		if len(args) > 0 && args[0] == "diff" && !strings.Contains(strings.Join(args, " "), "-- first") {
			t.Fatalf("unexpected diff: %v", args)
		}
	}
	if err := r.Stage("first", false); err != nil {
		t.Fatal(err)
	}
	cache, _, err = r.LoadCached(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Snapshot.Staged) != 1 || cache.Snapshot.Staged[0].Path != "first" {
		t.Fatalf("staged: %#v", cache.Snapshot.Staged)
	}
}

func TestCachedRefreshRacyWrite(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new", "first\n")
	path := filepath.Join(r.Root, "new")
	stamp := time.Now()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	cache, _, err := r.LoadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "new", "other\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	cache, unchanged, err := r.LoadCached(cache)
	if err != nil || unchanged || !strings.Contains(cache.Snapshot.Unstaged[0].Diff, "+other") {
		t.Fatalf("racy refresh: unchanged=%v err=%v cache=%#v", unchanged, err, cache)
	}
}

func TestStaleUnchangedRefreshDoesNotReplaceCache(t *testing.T) {
	current := &RefreshCache{}
	m := model{generation: 2, loading: true, refreshCache: current}
	updated, cmd := m.Update(loadedUnchanged{generation: 1, cache: &RefreshCache{}})
	next := updated.(model)
	if next.refreshCache != current || !next.loading || cmd == nil {
		t.Fatal("stale refresh should trigger a new load without replacing the cache")
	}
}

func BenchmarkIdleRefresh(b *testing.B) {
	r := Repo{Root: b.TempDir()}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := r.Command(args...); err != nil {
			b.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	write := func(path, content string) {
		b.Helper()
		name := filepath.Join(r.Root, path)
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
	if _, err := r.Action("add", "--all"); err != nil {
		b.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		write(fmt.Sprintf("tracked-%03d", i), "modified\n")
	}
	for i := 0; i < 200; i++ {
		write(fmt.Sprintf("untracked-%03d", i), "new\n")
	}
	cache, _, err := r.LoadCached(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := r.Load(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var err error
			cache, _, err = r.LoadCached(cache)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestBlurredPaneRefreshesLessOften(t *testing.T) {
	tickUntilLoading := func(m model) int {
		for i := 1; i <= 20; i++ {
			updated, _ := m.Update(tick{})
			m = updated.(model)
			if m.loading {
				return i
			}
		}
		return -1
	}
	if got := tickUntilLoading(model{}); got != refreshTicks {
		t.Fatalf("focused refresh after %d ticks, want %d", got, refreshTicks)
	}
	updated, _ := model{}.Update(tea.BlurMsg{})
	if got := tickUntilLoading(updated.(model)); got != blurredRefreshTicks {
		t.Fatalf("blurred refresh after %d ticks, want %d", got, blurredRefreshTicks)
	}
}

func TestFocusRefreshesImmediately(t *testing.T) {
	updated, cmd := model{blurred: true, ticks: 5}.Update(tea.FocusMsg{})
	m := updated.(model)
	if m.blurred || !m.loading || cmd == nil {
		t.Fatal("regaining focus should refresh immediately")
	}
}

func TestStatusUsesUntrackedCache(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "a.txt", "one\n", "first")
	writeFile(t, r, "new.txt", "new\n")
	r.UntrackedCache = true
	var commands []string
	r.CommandHook = func(args []string) { commands = append(commands, strings.Join(args, " ")) }
	_, entries, err := r.Status()
	if err != nil || len(entries) != 1 || entries[0].Path != "new.txt" {
		t.Fatalf("status with the untracked cache: %+v, %v", entries, err)
	}
	if want := "-c core.UntrackedCache=true status --porcelain=v2 -z --branch --renames --ahead-behind"; len(commands) != 1 || commands[0] != want {
		t.Fatalf("status should run once in the configured mode with the cache: %q", commands)
	}

	if err := os.MkdirAll(filepath.Join(r.Root, "dir", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "dir/sub/file.txt", "nested\n")
	commands = nil
	_, entries, err = r.Status()
	if err != nil || len(commands) != 2 || !strings.HasPrefix(commands[1], "ls-files --others") {
		t.Fatalf("an untracked directory should list its files: %q, %v", commands, err)
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, " ") != "dir/sub/file.txt new.txt" {
		t.Fatalf("untracked files: %q", paths)
	}

	r.UntrackedHidden = true
	commands = nil
	if _, entries, err = r.Status(); err != nil || len(commands) != 1 || len(entries) != 2 {
		t.Fatalf("hidden untracked files should still be listed: %q, %+v, %v", commands, entries, err)
	}
}

func TestStatusSettings(t *testing.T) {
	for config, want := range map[string]Repo{
		"":                                {UntrackedCache: true},
		"core.untrackedcache false\n":     {},
		"status.showuntrackedfiles no\n":  {UntrackedCache: true, UntrackedHidden: true},
		"status.showuntrackedfiles all\n": {UntrackedCache: true},
		"core.untrackedcache keep\nstatus.showuntrackedfiles false\n": {UntrackedHidden: true},
	} {
		if got := git.StatusSettings(config); got.UntrackedCache != want.UntrackedCache || got.UntrackedHidden != want.UntrackedHidden {
			t.Errorf("%q: got cache %v hidden %v", config, got.UntrackedCache, got.UntrackedHidden)
		}
	}
}

func TestExpandedStatusMatchesAllUntrackedFiles(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, ".gitignore", "*.log\n", "ignore")
	if err := os.MkdirAll(filepath.Join(r.Root, "tracked"), 0o755); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r, "tracked/old.txt", "old\n", "tracked")
	if out, err := r.Command("mv", "tracked/old.txt", "tracked/renamed.txt"); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	for _, dir := range []string{"a/b", "only ignored", "with space/x"} {
		if err := os.MkdirAll(filepath.Join(r.Root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"a/b/c.txt", "a/b/d.log", "a/e.txt", "only ignored/f.log", "with space/x/g h.txt", "tracked/new.txt", "top.txt"} {
		writeFile(t, r, path, "x\n")
	}
	got, err := r.StatusOutput()
	if err != nil {
		t.Fatal(err)
	}
	want, err := r.Command("status", "--porcelain=v2", "-z", "--branch", "--renames", "--ahead-behind", "--untracked-files=all")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("expanded status:\n%q\nwant:\n%q", got, want)
	}
}

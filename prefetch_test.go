package main

import (
	"os"
	"path/filepath"
	"testing"
)

func commitFile(t *testing.T, r repository, path, content, message string) {
	t.Helper()
	writeFile(t, r, path, content)
	for _, args := range [][]string{{"add", "--all"}, {"commit", "-m", message}} {
		if out, err := r.command(args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
}

func TestStatsStorePersistsAcrossLaunches(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "a.txt", "one\ntwo\n", "first")
	r.stats = &statsStore{path: filepath.Join(t.TempDir(), "pane", "stats")}
	if _, err := r.historyStats([]string{"HEAD"}); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	relaunched := &statsStore{path: r.stats.path}
	relaunched.fill(commits)
	if !commits[0].statsLoaded || commits[0].added != 2 || commits[0].deleted != 0 {
		t.Fatalf("stored stats not restored: %+v", commits[0])
	}
}

func TestStatsStoreIgnoresCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats")
	if err := os.WriteFile(path, []byte("garbage\nabc 1 2 0\npartial 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commits := []commitInfo{{hash: "abc"}, {hash: "partial"}}
	(&statsStore{path: path}).fill(commits)
	if !commits[0].statsLoaded || commits[0].added != 1 || commits[0].deleted != 2 || commits[1].statsLoaded {
		t.Fatalf("unexpected fill: %+v", commits)
	}
}

func TestCleanStartupOpensPrefetchedHistoryWithStats(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "a.txt", "one\n", "first")
	commitFile(t, r, "a.txt", "one\ntwo\nthree\n", "second")
	m, err := initialModel(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	m.prefetch = startPrefetch(r)
	updated, cmd := m.Update(m.prefetch.loadStatus()())
	m = updated.(model)
	if m.screen != "history" || m.prefetch != nil || cmd == nil {
		t.Fatalf("clean startup should open prefetched history; screen %q", m.screen)
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	defer m.closeHistoryStreams()
	if len(m.history.commits) != 2 || m.history.commits[0].subject != "second" {
		t.Fatalf("unexpected history: %+v", m.history.commits)
	}
	if c := m.history.commits[0]; !c.statsLoaded || c.added != 2 {
		t.Fatalf("newest commit should arrive with stats: %+v", c)
	}
}

func TestDirtyStartupDiscardsPrefetchedHistory(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "a.txt", "one\n", "first")
	writeFile(t, r, "a.txt", "changed\n")
	m, err := initialModel(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	m.prefetch = startPrefetch(r)
	updated, _ := m.Update(m.prefetch.loadStatus()())
	m = updated.(model)
	if m.screen != "" || m.prefetch != nil || len(m.snapshot.unstaged) != 1 {
		t.Fatalf("dirty startup should show the diff and drop the prefetch; screen %q", m.screen)
	}
}

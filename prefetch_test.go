package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/syntax"
)

func commitFile(t *testing.T, r Repo, path, content, message string) {
	t.Helper()
	writeFile(t, r, path, content)
	for _, args := range [][]string{{"add", "--all"}, {"commit", "-m", message}} {
		if out, err := r.Command(args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
}

func TestStatsStorePersistsAcrossLaunches(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "a.txt", "one\ntwo\n", "first")
	r.Stats = &StatsStore{Path: filepath.Join(t.TempDir(), "pane", "stats")}
	if _, err := r.HistoryStats([]string{"HEAD"}); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	relaunched := &StatsStore{Path: r.Stats.Path}
	relaunched.Fill(commits)
	if !commits[0].StatsLoaded || commits[0].Added != 2 || commits[0].Deleted != 0 {
		t.Fatalf("stored stats not restored: %+v", commits[0])
	}
}

func TestStatsStoreIgnoresCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats")
	if err := os.WriteFile(path, []byte("garbage\nabc 1 2 0\npartial 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commits := []CommitInfo{{Hash: "abc"}, {Hash: "partial"}}
	(&StatsStore{Path: path}).Fill(commits)
	if !commits[0].StatsLoaded || commits[0].Added != 1 || commits[0].Deleted != 2 || commits[1].StatsLoaded {
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
	m, cmd = applyBatch(t, m, cmd)
	m, _ = applyBatch(t, m, cmd)
	defer m.closeHistoryStreams()
	if len(m.history.commits) != 2 || m.history.commits[0].Subject != "second" {
		t.Fatalf("unexpected history: %+v", m.history.commits)
	}
	if c := m.history.commits[0]; !c.StatsLoaded || c.Added != 2 {
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
	if m.screen != "" || m.prefetch != nil || len(m.snapshot.Unstaged) != 1 {
		t.Fatalf("dirty startup should show the diff and drop the prefetch; screen %q", m.screen)
	}
}

func TestDirtyStartupFirstScreenIsHighlighted(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "main.go", "package main\n", "first")
	writeFile(t, r, "main.go", "package main\n\nfunc main() {}\n")
	m, err := initialModel(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	m.prefetch = startPrefetch(r)
	updated, _ := m.Update(m.prefetch.loadStatus()())
	m = updated.(model)
	defer m.closeHistoryStreams()
	if len(m.snapshot.Unstaged) != 1 || m.snapshot.Unstaged[0].SyntaxDiff == "" {
		t.Fatal("first load should include highlighting")
	}
	view := m.View()
	if !strings.Contains(ansi.Strip(view), "func main") || !strings.Contains(view, syntax.TokenColor(chroma.KeywordDeclaration)+"func") {
		t.Fatalf("first screen should be highlighted: %q", view)
	}
}

func TestStartupDefersOffscreenHighlighting(t *testing.T) {
	r := testRepo(t)
	body := func(word string) string {
		return "package main\n" + strings.Repeat("var "+word+" = 1\n", startupSyntaxLines)
	}
	commitFile(t, r, "a.go", body("a"), "first")
	writeFile(t, r, "b.go", body("b"))
	if out, err := r.Command("add", "b.go"); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if out, err := r.Command("commit", "-m", "second"); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	writeFile(t, r, "a.go", body("x"))
	writeFile(t, r, "b.go", body("y"))
	first, _, err := r.LoadChanges(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	unstaged := first.Snapshot.Unstaged
	if !first.Plain || unstaged[0].SyntaxDiff == "" || unstaged[1].SyntaxDiff != "" {
		t.Fatal("only the first screen's file should be highlighted at startup")
	}
	next, unchanged, err := r.LoadCached(first)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged || next.Plain || next.Snapshot.Unstaged[1].SyntaxDiff == "" {
		t.Fatal("follow-up load should highlight the deferred file")
	}
}

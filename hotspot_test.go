package main

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func hotFixture(t *testing.T) repository {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	r := testRepo(t)
	old := time.Now().Add(-400 * 24 * time.Hour).Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", old)
	t.Setenv("GIT_COMMITTER_DATE", old)
	writeFile(t, r, "old-big.txt", strings.Repeat("old line\n", 100))
	if _, err := r.commit("old big change"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_AUTHOR_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	t.Setenv("GIT_AUTHOR_NAME", "Other")
	writeFile(t, r, "recent.txt", "one\ntwo\nthree\nfour\nfive\n")
	if _, err := r.commit("recent small change"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHotspotsRankRecentEditsAboveOldBigOnes(t *testing.T) {
	r := hotFixture(t)
	files, _, err := r.hotspots("2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].path != "recent.txt" || files[1].path != "old-big.txt" {
		t.Fatalf("rank: %#v", files)
	}
	if files[0].commits != 1 || files[0].added != 5 {
		t.Fatalf("recent file: %#v", files[0])
	}
}

func TestHotspotsAuthorFilter(t *testing.T) {
	r := hotFixture(t)
	files, authors, err := r.hotspots("2.year", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].path != "recent.txt" {
		t.Fatalf("author filter: %#v", files)
	}
	if _, ok := authors["Other"]; !ok {
		t.Fatalf("authors: %#v", authors)
	}
}

func TestHotspotsMergeRenamesAndExcludeGenerated(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "before.txt", "line\n")
	writeFile(t, r, "go.sum", strings.Repeat("sum line\n", 5))
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("mv", "before.txt", "after.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.commit("rename"); err != nil {
		t.Fatal(err)
	}
	files, _, err := r.hotspots("2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.path] = true
	}
	if !paths["after.txt"] || paths["go.sum"] {
		t.Fatalf("files: %#v", paths)
	}
}

func TestHotspotScreenFoldsSortsAndOpensHistory(t *testing.T) {
	r := hotFixture(t)
	m := model{width: 120, height: 30, wrap: true, repo: r, viewed: newViewedStore(r.root)}
	updated, cmd := m.openHotspots("2.year", "")
	m = updated.(model)
	if cmd == nil {
		t.Fatal("hotspots produced no load command")
	}
	m = drainHotspots(t, m, cmd)
	if m.hotspots.loading || len(m.hotspots.files) != 2 {
		t.Fatalf("hotspots did not load: %#v", m.hotspots.files)
	}
	view := m.hotView()
	if !strings.Contains(view, "recent.txt") || !strings.Contains(view, "old-big.txt") {
		t.Fatalf("view: %q", view)
	}
	// Sort by path puts old-big.txt first.
	updated, _ = m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = updated.(model)
	items := m.hotItems()
	if len(items) != 2 || items[0].name != "old-big.txt" {
		t.Fatalf("path sort: %#v", items)
	}
	// Enter on the file opens its history.
	updated, cmd = m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != "history" || m.history.path != "old-big.txt" || cmd == nil {
		t.Fatalf("file history: screen=%q path=%q", m.screen, m.history.path)
	}
	// The author list filters the tree.
	m = model{width: 120, height: 30, wrap: true, repo: r, hotspots: hotspotState{files: m0Files(t, r), authors: m0Authors(t, r)}}
	updated, _ = m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.hotspots.mode != "authors" || len(m.hotAuthorList()) == 0 {
		t.Fatal("tab did not open the author list")
	}
	updated, _ = m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.hotspots.authorFilter == "" {
		t.Fatal("author selection did not filter")
	}
}

func drainHotspots(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	for i := 0; cmd != nil && i < 10000; i++ {
		m, cmd = applyBatch(t, m, cmd)
	}
	return m
}

func m0Files(t *testing.T, r repository) []hotFile {
	t.Helper()
	files, _, err := r.hotspots("2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func m0Authors(t *testing.T, r repository) map[string]*hotAuthor {
	t.Helper()
	_, authors, err := r.hotspots("2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	return authors
}

func TestHotspotVOpensFilteredSequence(t *testing.T) {
	r := hotFixture(t)
	m := model{width: 120, height: 30, wrap: true, repo: r, viewed: newViewedStore(r.root)}
	updated, cmd := m.openHotspots("2.year", "")
	m = updated.(model)
	m = drainHotspots(t, m, cmd)
	items := m.hotItems()
	if len(items) == 0 {
		t.Fatal("no hotspot items")
	}
	cmd = m.hotspotSequence(items[0].path)
	if cmd == nil {
		t.Fatal("no sequence command")
	}
	msg, ok := cmd().(hotspotSequenceLoaded)
	if !ok || msg.err != nil || len(msg.commits) == 0 {
		t.Fatalf("sequence message: %#v", msg)
	}
	updated, cmd = m.Update(msg)
	m = updated.(model)
	if m.screen != "sequence" || len(m.sequence.commits) == 0 || cmd == nil {
		t.Fatalf("sequence review: screen=%q commits=%d", m.screen, len(m.sequence.commits))
	}
}

func TestHotspotCacheAvoidsSecondScan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := hotFixture(t)
	if _, _, err := r.hotspots("2.year", ""); err != nil {
		t.Fatal(err)
	}
	var logs atomic.Int64
	r.commandHook = func(args []string) {
		if len(args) > 0 && args[0] == "log" {
			logs.Add(1)
		}
	}
	files, authors, err := r.hotspots("2.year", "")
	if err != nil || len(files) != 2 || len(authors) == 0 {
		t.Fatalf("cached load: %d files %v", len(files), err)
	}
	if logs.Load() != 0 {
		t.Fatalf("second load ran %d git log processes", logs.Load())
	}
}

func TestHotspotFileHistoryKeepsWindow(t *testing.T) {
	r := hotFixture(t)
	m := model{width: 120, height: 30, wrap: true, repo: r, hotspots: hotspotState{since: "6mo"}}
	updated, _ := m.openPathHistory("recent.txt")
	m = updated.(model)
	if m.screen != "history" || m.history.path != "recent.txt" || m.history.since != "6mo" {
		t.Fatalf("file history: %#v", m.history)
	}
}

func TestHotspotCacheLoadsOnlyNewCommitsAfterHeadMoves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := hotFixture(t)
	if _, _, err := r.hotspots("2.year", ""); err != nil {
		t.Fatal(err)
	}
	tip, err := r.command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "later.txt", "later\n")
	if _, err := r.commit("later change"); err != nil {
		t.Fatal(err)
	}
	head, err := r.command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var logs [][]string
	r.commandHook = func(args []string) {
		if len(args) > 0 && args[0] == "log" {
			logs = append(logs, append([]string(nil), args...))
		}
	}
	files, _, err := r.hotspots("2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("second launch ran %d git log commands: %v", len(logs), logs)
	}
	joined := strings.Join(logs[0], " ")
	wantRange := strings.TrimSpace(string(tip)) + ".." + strings.TrimSpace(string(head))
	t.Logf("incremental git log: %s", joined)
	if !strings.Contains(joined, "--numstat") || !strings.Contains(joined, wantRange) {
		t.Fatalf("incremental log did not use the cached tip: %s", joined)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.path] = true
	}
	if !paths["later.txt"] || !paths["recent.txt"] || !paths["old-big.txt"] {
		t.Fatalf("merged files: %#v", paths)
	}
	for _, file := range files {
		if file.path == "later.txt" && (file.added != 1 || file.commits != 1) {
			t.Fatalf("new commit was not merged: %#v", file)
		}
	}
}

func TestHotspotCacheFallsBackWhenHistoryIsRewritten(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := hotFixture(t)
	if _, _, err := r.hotspots("2.year", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "later.txt", "later\n")
	if _, err := r.commit("later change"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("reset", "--hard", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	files, _, err := r.hotspots("2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.path == "later.txt" {
			t.Fatalf("rewritten history kept a commit that is no longer reachable: %#v", file)
		}
	}
}

func TestSinceCutoff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, test := range []struct {
		since string
		want  time.Duration
		ok    bool
	}{
		{"1.year", 365 * 24 * time.Hour, true},
		{"2.year", 2 * 365 * 24 * time.Hour, true},
		{"6mo", 6 * 30 * 24 * time.Hour, true},
		{"90 days", 90 * 24 * time.Hour, true},
		{"2 weeks ago", 14 * 24 * time.Hour, true},
		{"", 0, true},
		{"2025-01-01", 0, false},
	} {
		got, ok := sinceCutoff(test.since, now)
		want := int64(0)
		if test.want != 0 {
			want = now.Add(-test.want).Unix()
		}
		if ok != test.ok || (ok && got != want) {
			t.Errorf("sinceCutoff(%q) = %d, %v; want %d, %v", test.since, got, ok, want, test.ok)
		}
	}
}

func TestHotspotStreamEmitsPagesAndUpdatesTree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	previous := hotspotPageSize
	hotspotPageSize = 1
	t.Cleanup(func() { hotspotPageSize = previous })
	r := hotFixture(t)
	m := model{width: 120, height: 30, wrap: true, repo: r, viewed: newViewedStore(r.root)}
	updated, cmd := m.openHotspots("2.year", "")
	m = updated.(model)
	// The start message establishes the plan.
	m, cmd = applyBatch(t, m, cmd)
	if cmd == nil || m.hotspots.stream == nil {
		t.Fatalf("hotspot stream did not start: %#v", m.hotspots)
	}
	// A partial page must already produce rows, before the stream finishes.
	updated, cmd = applyBatch(t, m, cmd)
	m = updated.(model)
	if len(m.hotspots.commits) == 0 || len(m.hotspots.files) == 0 || cmd == nil {
		t.Fatalf("first page did not update the tree: commits=%d files=%d", len(m.hotspots.commits), len(m.hotspots.files))
	}
	if !strings.Contains(m.hotView(), "recent.txt") && !strings.Contains(m.hotView(), "old-big.txt") {
		t.Fatalf("first page view: %q", m.hotView())
	}
	// Drain the rest and confirm both commits are present.
	m = drainHotspots(t, m, cmd)
	if len(m.hotspots.commits) != 2 || m.hotspots.stream != nil || m.hotspots.loading {
		t.Fatalf("stream did not finish: commits=%d stream=%v loading=%v", len(m.hotspots.commits), m.hotspots.stream, m.hotspots.loading)
	}
}

func TestHotspotSummaryCacheAvoidsCommitDecode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := hotFixture(t)
	if _, _, err := r.hotspots("2.year", ""); err != nil {
		t.Fatal(err)
	}
	// Remove the large per-commit cache; the summary alone should serve the
	// unchanged HEAD.
	if err := os.Remove(hotCachePath(r.root, "2.year", "")); err != nil {
		t.Fatal(err)
	}
	var logs atomic.Int64
	r.commandHook = func(args []string) {
		if len(args) > 0 && args[0] == "log" {
			logs.Add(1)
		}
	}
	plan, err := r.planHotspots("2.year", "")
	if err != nil || !plan.cacheHit || len(plan.files) != 2 || len(plan.items) == 0 {
		t.Fatalf("summary cache: %d files %d items %v", len(plan.files), len(plan.items), err)
	}
	if logs.Load() != 0 {
		t.Fatalf("summary cache ran %d git log processes", logs.Load())
	}
}

func TestHotspotCacheHitPrefillsTree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := hotFixture(t)
	if _, _, err := r.hotspots("2.year", ""); err != nil {
		t.Fatal(err)
	}
	m := model{width: 120, height: 30, wrap: true, repo: r}
	updated, cmd := m.openHotspots("2.year", "")
	m = updated.(model)
	m, cmd = applyBatch(t, m, cmd)
	if cmd != nil {
		t.Fatalf("cache hit started a stream")
	}
	if m.hotspots.loading || m.hotspots.items == nil || !m.hotspots.items.filled || len(m.hotspots.items.items) == 0 {
		t.Fatalf("cache hit did not prefill the tree: %#v", m.hotspots.items)
	}
	if view := m.hotView(); !strings.Contains(view, "recent.txt") {
		t.Fatalf("prefilled view: %q", view)
	}
}

func TestParallelHotspotWalkMatchesSingle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := testRepo(t)
	for i := 0; i < 25; i++ {
		writeFile(t, r, fmt.Sprintf("file-%02d.txt", i%5), strings.Repeat(fmt.Sprintf("line %d\n", i), 10))
		if _, err := r.commit(fmt.Sprintf("commit %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	oldThreshold, oldWorkers := hotspotParallelThreshold, hotspotWorkers
	t.Cleanup(func() { hotspotParallelThreshold, hotspotWorkers = oldThreshold, oldWorkers })
	clear := func() {
		_ = os.Remove(hotCachePath(r.root, "1.year", ""))
		_ = os.Remove(hotSummaryPath(r.root, "1.year", ""))
	}
	hotspotParallelThreshold, hotspotWorkers = 5, 4
	clear()
	parallelFiles, parallelAuthors, err := r.hotspots("1.year", "")
	if err != nil {
		t.Fatal(err)
	}
	hotspotParallelThreshold, hotspotWorkers = 10000, 1
	clear()
	singleFiles, singleAuthors, err := r.hotspots("1.year", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parallelFiles) != len(singleFiles) || len(parallelAuthors) != len(singleAuthors) {
		t.Fatalf("parallel %d files/%d authors, single %d/%d", len(parallelFiles), len(parallelAuthors), len(singleFiles), len(singleAuthors))
	}
	byPath := make(map[string]hotFile, len(singleFiles))
	for _, file := range singleFiles {
		byPath[file.path] = file
	}
	for _, file := range parallelFiles {
		other, ok := byPath[file.path]
		if !ok || file.added != other.added || file.deleted != other.deleted || file.commits != other.commits {
			t.Fatalf("parallel file %#v, single %#v", file, other)
		}
	}
}

package main

import (
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
	m, _ = applyBatch(t, m, cmd)
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
	m, _ = applyBatch(t, m, cmd)
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

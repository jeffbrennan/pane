package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func hotFixture(t *testing.T) git.Repo {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	r := testRepo(t)
	old := time.Now().Add(-400 * 24 * time.Hour).Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", old)
	t.Setenv("GIT_COMMITTER_DATE", old)
	writeFile(t, r, "old-big.txt", strings.Repeat("old line\n", 100))
	if _, err := r.Commit("old big change"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_AUTHOR_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	t.Setenv("GIT_AUTHOR_NAME", "Other")
	writeFile(t, r, "recent.txt", "one\ntwo\nthree\nfour\nfive\n")
	if _, err := r.Commit("recent small change"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHotspotsRankRecentEditsAboveOldBigOnes(t *testing.T) {
	r := hotFixture(t)
	files, _, err := hotspots(r, "2.year", "")
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
	files, authors, err := hotspots(r, "2.year", "Other")
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
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("mv", "before.txt", "after.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("rename"); err != nil {
		t.Fatal(err)
	}
	files, _, err := hotspots(r, "2.year", "")
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
	m := model{width: 120, height: 30, wrap: true, repo: r, viewed: newViewedStore(r.Root)}
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
	// The first file is selected and Enter opens its history.
	updated, cmd = m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != "history" || m.history.path != "recent.txt" || cmd == nil {
		t.Fatalf("file history: screen=%q path=%q", m.screen, m.history.path)
	}
	// tab cycles tree, files, authors.
	m = model{width: 120, height: 30, wrap: true, repo: r, hotspots: hotspotState{files: m0Files(t, r), authors: m0Authors(t, r)}}
	updated, _ = m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.hotspots.mode != "files" || len(m.hotFileItems()) != 2 {
		t.Fatalf("tab did not open the file view: %q %d", m.hotspots.mode, len(m.hotFileItems()))
	}
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

func m0Files(t *testing.T, r git.Repo) []hotFile {
	t.Helper()
	files, _, err := hotspots(r, "2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func m0Authors(t *testing.T, r git.Repo) map[string]*hotAuthor {
	t.Helper()
	_, authors, err := hotspots(r, "2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	return authors
}

func TestHotspotVOpensFilteredSequence(t *testing.T) {
	r := hotFixture(t)
	m := model{width: 120, height: 30, wrap: true, repo: r, viewed: newViewedStore(r.Root)}
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
	if _, _, err := hotspots(r, "2.year", ""); err != nil {
		t.Fatal(err)
	}
	var logs atomic.Int64
	r.CommandHook = func(args []string) {
		if len(args) > 0 && args[0] == "log" {
			logs.Add(1)
		}
	}
	files, authors, err := hotspots(r, "2.year", "")
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
	if _, _, err := hotspots(r, "2.year", ""); err != nil {
		t.Fatal(err)
	}
	tip, err := r.Command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "later.txt", "later\n")
	if _, err := r.Commit("later change"); err != nil {
		t.Fatal(err)
	}
	head, err := r.Command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var logs [][]string
	r.CommandHook = func(args []string) {
		if len(args) > 0 && args[0] == "log" {
			logs = append(logs, append([]string(nil), args...))
		}
	}
	files, _, err := hotspots(r, "2.year", "")
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
	if _, _, err := hotspots(r, "2.year", ""); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "later.txt", "later\n")
	if _, err := r.Commit("later change"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("reset", "--hard", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	files, _, err := hotspots(r, "2.year", "")
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
	m := model{width: 120, height: 30, wrap: true, repo: r, viewed: newViewedStore(r.Root)}
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
	if _, _, err := hotspots(r, "2.year", ""); err != nil {
		t.Fatal(err)
	}
	// Remove the large per-commit cache; the summary alone should serve the
	// unchanged HEAD.
	if err := os.Remove(hotCachePath(r.Root, "2.year", "")); err != nil {
		t.Fatal(err)
	}
	var logs atomic.Int64
	r.CommandHook = func(args []string) {
		if len(args) > 0 && args[0] == "log" {
			logs.Add(1)
		}
	}
	plan, err := planHotspots(r, "2.year", "")
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
	if _, _, err := hotspots(r, "2.year", ""); err != nil {
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
		if _, err := r.Commit(fmt.Sprintf("commit %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	oldThreshold, oldWorkers := git.HotspotParallelThreshold, git.HotspotWorkers
	t.Cleanup(func() { git.HotspotParallelThreshold, git.HotspotWorkers = oldThreshold, oldWorkers })
	clear := func() {
		_ = os.Remove(hotCachePath(r.Root, "1.year", ""))
		_ = os.Remove(hotSummaryPath(r.Root, "1.year", ""))
	}
	git.HotspotParallelThreshold, git.HotspotWorkers = 5, 4
	clear()
	parallelFiles, parallelAuthors, err := hotspots(r, "1.year", "")
	if err != nil {
		t.Fatal(err)
	}
	git.HotspotParallelThreshold, git.HotspotWorkers = 10000, 1
	clear()
	singleFiles, singleAuthors, err := hotspots(r, "1.year", "")
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

func TestHotspotScoresAttentionNotVolume(t *testing.T) {
	now := time.Now()
	var sweep []git.HotCacheChange
	for i := 0; i < 300; i++ {
		sweep = append(sweep, git.HotCacheChange{Path: fmt.Sprintf("bulk/file%03d.go", i), Added: 33})
	}
	commits := []git.HotCacheCommit{git.NewHotCacheCommit("bulk", now.Unix(), "Bulk", sweep)}
	for i := 0; i < 30; i++ {
		commits = append(commits, git.NewHotCacheCommit(fmt.Sprintf("small%d", i), now.Unix(), "Steady", []git.HotCacheChange{{Path: "src/attention.go", Added: 5}}))
	}
	files, _, _, _ := aggregateHotspots(commits, newHotPathMarks(), 0, now)
	if files[0].path != "src/attention.go" {
		t.Fatalf("bulk commit outranked steady edits: %#v", files[:2])
	}
}

func hotItemPaths(items []hotItem) string {
	var paths []string
	for _, item := range items {
		paths = append(paths, item.path)
	}
	return strings.Join(paths, "\n")
}

func TestHotspotClassificationHidesVendoredAndGenerated(t *testing.T) {
	now := time.Now()
	commits := []git.HotCacheCommit{git.NewHotCacheCommit("c1", now.Unix(), "A", []git.HotCacheChange{
		{Path: "src/main.go", Added: 10},
		{Path: "foo_test.go", Added: 20},
		{Path: "vendor/lib/x.go", Added: 30},
		{Path: "docs/guide.md", Added: 40},
		{Path: "third_party/y.go", Added: 50},
	})}
	marks := newHotPathMarks()
	marks.Generated["gen/api.go"] = true
	files, _, _, hidden := aggregateHotspots(commits, marks, 0, now)
	paths := map[string]hotClass{}
	for _, file := range files {
		paths[file.path] = file.class
	}
	if paths["src/main.go"] != hotSource || paths["foo_test.go"] != hotTests || paths["docs/guide.md"] != hotDocs {
		t.Fatalf("classes: %#v", paths)
	}
	if hidden.vendored != 2 || hidden.generated != 0 {
		t.Fatalf("hidden counts: %#v", hidden)
	}
	if paths["vendor/lib/x.go"] != hotVendored || paths["third_party/y.go"] != hotVendored {
		t.Fatalf("vendored classes: %#v", paths)
	}
	dirs := map[string]*hotDirStat{}
	if items := buildHotItems(files, dirs, false, hotTreeState{}, ""); strings.Contains(hotItemPaths(items), "vendor/") {
		t.Fatalf("vendored files shown by default: %v", hotItemPaths(items))
	}
	if items := buildHotItems(files, dirs, false, hotTreeState{showHidden: true}, ""); !strings.Contains(hotItemPaths(items), "vendor/") {
		t.Fatalf("vendored files not shown after toggling: %v", hotItemPaths(items))
	}
}

func hotTestFile(path string, score float64, class hotClass, author string, churn int) hotFile {
	return hotFile{path: path, score: score, class: class, authors: map[string]int{author: churn}, added: churn, last: time.Now()}
}

func TestHotspotTreeOpensOneLevelAndMergesChains(t *testing.T) {
	files := []hotFile{
		hotTestFile("src/bun.js/api/a.go", 10, hotSource, "A", 10),
		hotTestFile("src/bun.js/api/b.go", 9, hotSource, "A", 9),
		hotTestFile("test/heavy_test.go", 100, hotTests, "A", 100),
	}
	items := buildHotItems(files, nil, false, hotTreeState{}, "")
	if len(items) == 0 || items[0].path != "src/bun.js/api" || !items[0].directory {
		t.Fatalf("single-child chain was not merged: %#v", items)
	}
	if items[0].name != "▾ src/bun.js/api/" {
		t.Fatalf("fold marker or name: %q", items[0].name)
	}
	if len(items) < 3 || items[1].path != "src/bun.js/api/a.go" {
		t.Fatalf("top-level area did not open one level: %#v", items)
	}
	if last := items[len(items)-1]; last.class != hotTests && !strings.HasPrefix(last.path, "test") {
		t.Fatalf("tests ranked above source: %#v", items)
	}
	// Top-level areas open one level, so test/ shows its file too.
	found := false
	for _, item := range items {
		if item.path == "test" && item.directory && !item.collapsed {
			found = true
		}
	}
	if !found {
		t.Fatalf("top-level directory should be open one level: %#v", items)
	}
}

func TestHotspotTreeShowsFiveChildrenThenMore(t *testing.T) {
	var files []hotFile
	for i := 0; i < 8; i++ {
		files = append(files, hotTestFile(fmt.Sprintf("pkg/f%d.go", i), float64(100-i), hotSource, "A", i+1))
	}
	items := buildHotItems(files, nil, false, hotTreeState{}, "")
	var more *hotItem
	for i := range items {
		if items[i].more {
			more = &items[i]
		}
	}
	if more == nil || more.remaining != 3 || more.path != "pkg" {
		t.Fatalf("missing more row: %#v", items)
	}
	expanded := buildHotItems(files, nil, false, hotTreeState{showAll: map[string]bool{"pkg": true}}, "")
	for _, item := range expanded {
		if item.more {
			t.Fatalf("showAll still truncated: %#v", expanded)
		}
	}
	if len(expanded) != 9 {
		t.Fatalf("expanded rows: %d", len(expanded))
	}
}

func TestHotspotFilesViewAndAuthorFilter(t *testing.T) {
	files := []hotFile{
		hotTestFile("src/slow.go", 1, hotSource, "A", 1),
		hotTestFile("src/fast.go", 9, hotSource, "B", 9),
		hotTestFile("vendor/x.go", 100, hotVendored, "A", 100),
	}
	m := model{hotspots: hotspotState{files: files, authors: map[string]*hotAuthor{"A": {name: "A"}, "B": {name: "B"}}}}
	items := m.hotFileItems()
	if len(items) != 2 || items[0].path != "src/fast.go" {
		t.Fatalf("files view order or hidden files: %#v", items)
	}
	m.hotspots.authorFilter = "A"
	items = m.hotFileItems()
	if len(items) != 1 || items[0].path != "src/slow.go" {
		t.Fatalf("author filter: %#v", items)
	}
	m.hotspots.hiddenShown = true
	m.hotspots.authorFilter = ""
	if items := m.hotFileItems(); len(items) != 3 {
		t.Fatalf("hidden files should show after a: %#v", items)
	}
}

func TestHotspotBotsSortLast(t *testing.T) {
	m := model{hotspots: hotspotState{authors: map[string]*hotAuthor{
		"renovate-bot":        {name: "renovate-bot", commits: 900},
		"Human":               {name: "Human", commits: 3},
		"github-actions[bot]": {name: "github-actions[bot]", commits: 50},
	}}}
	authors := m.hotAuthorList()
	if authors[0].name != "Human" || !isBotAuthor(authors[len(authors)-1].name) {
		var names []string
		for _, a := range authors {
			names = append(names, fmt.Sprintf("%s(%d,bot=%v)", a.name, a.commits, isBotAuthor(a.name)))
		}
		t.Fatalf("bot ordering: %v", names)
	}
}

func TestHotspotsDropDeletedPaths(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "gone.go", "package gone\n")
	if _, err := r.Commit("add"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.Root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("remove"); err != nil {
		t.Fatal(err)
	}
	files, _, err := hotspots(r, "1.year", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.path == "gone.go" {
			t.Fatalf("deleted path kept: %#v", files)
		}
	}
}

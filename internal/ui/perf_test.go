package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// TestPerf is opt-in because it performs repeated full builds of potentially large commits.
func TestPerf(t *testing.T) {
	if os.Getenv("PANE_PERF") != "1" {
		t.Skip("set PANE_PERF=1 to run")
	}
	var changes []git.Change
	var m model
	scenario := "synthetic-many"
	if os.Getenv("PANE_PERF_REPO") != "" {
		scenario = "commit"
	}
	if name := os.Getenv("PANE_PERF_SCENARIO"); name != "" {
		scenario = name
	}
	if dir := os.Getenv("PANE_PERF_PROFILES"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		cpu, err := os.Create(filepath.Join(dir, scenario+".cpu.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.StartCPUProfile(cpu); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			pprof.StopCPUProfile()
			cpu.Close()
			if err := writePerfHeapProfile(filepath.Join(dir, scenario+".mem.pprof")); err != nil {
				t.Error(err)
			}
			runtime.KeepAlive(m)
		})
	}
	if dir := os.Getenv("PANE_PERF_REPO"); dir != "" {
		repo := git.Repo{Root: dir}
		revision := os.Getenv("PANE_PERF_COMMIT")
		if revision == "" {
			revision = "HEAD"
		}
		var err error
		measurePerf(t, scenario, "load_ns", func() {
			changes, err = repo.CommitChangesWithoutSyntax(revision)
			if err != nil {
				t.Fatal(err)
			}
		})
		measurePerf(t, scenario, "cancel_load_after_30ms_ns", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancelRepo := repo
			cancelRepo.Context = ctx
			done := make(chan struct{})
			go func() {
				_, _ = cancelRepo.CommitChangesWithoutSyntax(revision)
				close(done)
			}()
			time.Sleep(30 * time.Millisecond)
			cancel()
			<-done
		})
		measurePerf(t, scenario, "stream_first_chunk_ns", func() {
			ctx, cancel := context.WithCancel(context.Background())
			stream := repo.StreamCommit(ctx, revision, git.StartupSyntaxLines)
			chunk, ok := <-stream
			if !ok || chunk.Err != nil || len(chunk.Changes) == 0 {
				cancel()
				t.Fatalf("first stream chunk: %#v", chunk)
			}
			cancel()
			for range stream {
			}
		})
		measurePerf(t, scenario, "stream_total_ns", func() {
			for chunk := range repo.StreamCommit(context.Background(), revision, git.StartupSyntaxLines) {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
			}
		})
		var gitCommands atomic.Int64
		repo.CommandHook = func([]string) { gitCommands.Add(1) }
		if _, err := repo.CommitChangesWithoutSyntax(revision); err != nil {
			t.Fatal(err)
		}
		recordPerf(t, scenario, "open_git_commands", gitCommands.Load())
		gitCommands.Store(0)
		idleModel := model{screen: "commit"}
		for i := 0; i < 120; i++ {
			next, _ := idleModel.update(tick{})
			idleModel = next.(model)
		}
		recordPerf(t, scenario, "idle_git_commands", gitCommands.Load())
	} else {
		for i := 0; i < 500; i++ {
			changes = append(changes, git.Change{Path: fmt.Sprintf("file-%04d.txt", i), Diff: "@@ -0,0 +1,100 @@\n" + strings.Repeat("+a representative line of text\n", 100)})
		}
	}
	m = model{width: 160, height: 50, wrap: true, screen: "commit", history: historyState{changes: changes}}
	if len(changes) > 0 {
		measurePerf(t, scenario, "moved_index_ns", func() {
			index := newMovedIndex()
			index.addChanges(changes)
		})
	}
	measurePerf(t, scenario, "build_ns", func() { m.rebuildCommit() })
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "heap_after_build_bytes", int64(memory.HeapAlloc))
	recordPerf(t, scenario, "sys_after_build_bytes", int64(memory.Sys))
	beforeAlloc := memory.TotalAlloc
	m.rebuildCommit()
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "rebuild_alloc_bytes", int64(memory.TotalAlloc-beforeAlloc))
	view := m.commitModel()
	measurePerf(t, scenario, "view_ns", func() { _ = view.View() })
	runtime.ReadMemStats(&memory)
	beforeAlloc = memory.TotalAlloc
	_ = view.View()
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "view_alloc_bytes", int64(memory.TotalAlloc-beforeAlloc))
	if len(view.rows) > 0 {
		scroll := view
		scroll.screen = "commit"
		scroll.history.pos, scroll.history.viewTop = len(scroll.rows)/2, len(scroll.rows)/2
		measurePerf(t, scenario, "scroll_j_mid_ns", func() {
			_, _ = scroll.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		})
		measurePerf(t, scenario, "scroll_ctrl_d_mid_ns", func() {
			_, _ = scroll.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		})
		scroll.history.pos = max(0, len(scroll.rows)-1)
		scroll.history.viewTop = max(0, scroll.history.pos-max(1, scroll.height-2)+1)
		measurePerf(t, scenario, "scroll_j_end_ns", func() {
			_, _ = scroll.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		})
		if frames, err := strconv.Atoi(os.Getenv("PANE_PERF_SCROLL_FRAMES")); err == nil && frames > 0 {
			// A longer scroll loop gives the CPU profile enough samples to
			// show the render path rather than one-off loading work.
			for i := 0; i < frames; i++ {
				_, _ = scroll.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
				_ = scroll.View()
				_, _ = scroll.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
				_ = scroll.View()
			}
		}
	}
	if len(view.rows) > 0 {
		query := view.rows[0].path
		measurePerf(t, scenario, "search_first_hit_ns", func() { _, _ = view.findMatch(query, -1, 1) })
	}
	measurePerf(t, scenario, "search_miss_ns", func() { _, _ = view.findMatch("unlikelymissingpattern", -1, 1) })
	measurePerf(t, scenario, "search_smartcase_miss_ns", func() { _, _ = view.findMatch("UnlikelyMissingPattern", -1, 1) })
	view.startSearch()
	view.search.draft = "unlikelymissingpattern"
	view.updateSearchDraft()
	measurePerf(t, scenario, "search_narrowed_ns", func() {
		view.search.draft = "unlikelymissingpatternx"
		view.updateSearchDraft()
	})
	measurePerf(t, scenario, "resize_height_ns", func() { _, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 51}) })
	measurePerf(t, scenario, "resize_width_ns", func() { _, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 50}) })
	resized, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 50})
	resizeModel, _ := resized.(model)
	measurePerfOnce(t, scenario, "resize_width_apply_ns", func() {
		resizeModel.Update(resizeRebuild{request: resizeModel.resizeRequest})
	})
	entries := view.fileEntries()
	measurePerf(t, scenario, "picker_entries_ns", func() {
		view.picker.cache = nil
		view.fileEntries()
	})
	measurePerf(t, scenario, "picker_filter_ns", func() { filterFiles(entries, "") })
	measurePerf(t, scenario, "picker_tree_ns", func() { pickerTree(entries, nil) })
	measurePerf(t, scenario, "picker_open_ns", func() { _, _ = view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}) })
	picker := view
	picker.modal = "files"
	picker.picker = pickerState{cache: &pickerCache{}}
	measurePerf(t, scenario, "picker_key_ns", func() { picker.handlePickerKey(keyMsg("j")) })
	measurePerf(t, scenario, "picker_render_ns", func() { _ = picker.View() })
	measurePerf(t, scenario, "wrap_toggle_ns", func() { _, _ = view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}) })
	measurePerf(t, scenario, "whitespace_toggle_ns", func() {
		toggle := view
		toggle.repo.IgnoreSpace = false
		_, _ = toggle.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	})
	if len(changes) > 0 {
		c := changes[0]
		key := git.Context{Commit: view.history.current.Hash, Path: c.Path}
		demand := view
		demand.syntaxDemand = &syntaxDemand{loaded: make(map[git.Context]demandSyntax), pending: make(map[git.Context]bool)}
		measurePerf(t, scenario, "syntax_demand_apply_ns", func() {
			demand.syntaxDemand.loaded = make(map[git.Context]demandSyntax)
			demand.applySyntaxDemandLoaded(syntaxDemandLoaded{results: []demandResult{{key: key, diff: c.Diff, syntaxDiff: c.Diff}}})
		})
	}
	measurePerf(t, scenario, "jump_file_ns", func() { _, _ = view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")}) })
	measurePerf(t, scenario, "jump_end_ns", func() { _, _ = view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")}) })
	runtime.GC()
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "heap_after_first_screen_bytes", int64(memory.HeapAlloc))
	recordPerf(t, scenario, "rows", int64(len(view.rows)))
	recordPerf(t, scenario, "row_bytes", int64(unsafe.Sizeof(row{})))
	idle := model{screen: "commit"}
	refreshes := int64(0)
	for i := 0; i < 120; i++ {
		next, _ := idle.update(tick{})
		idle = next.(model)
		if idle.loading {
			refreshes++
			idle.loading = false
		}
	}
	recordPerf(t, scenario, "idle_refreshes_per_minute", refreshes)
	if len(changes) > 2000 {
		measureCommitRelease(t, scenario, changes)
	}
	measureHotspotPerf(t)
	measureSyntaxPerf(t)
	measureRefreshPerf(t)
	measureSyntheticScenarios(t)
	measureViewScenarios(t)
}

func measureViewScenarios(t *testing.T) {
	changes := make([]git.Change, 25)
	for i := range changes {
		changes[i] = git.Change{Path: fmt.Sprintf("file-%02d.go", i), Diff: "@@ -0,0 +1,10 @@\n" + strings.Repeat("+var value = 1\n", 10)}
	}
	rangeModel := model{width: 160, height: 50, wrap: true, screen: "sequence"}
	rangeModel.sequence.commits = make([]git.CommitInfo, 100)
	rangeModel.sequence.changes = make([][]git.Change, 100)
	rangeModel.sequence.states = make([]sequenceLoadState, 100)
	for i := range rangeModel.sequence.commits {
		rangeModel.sequence.commits[i] = git.CommitInfo{Hash: fmt.Sprintf("%08x", i)}
		rangeModel.sequence.states[i] = sequenceReady
		rangeModel.sequence.changes[i] = changes
	}
	rangeModel.rebuildSequence()
	measurePerf(t, "synthetic-range", "rebuild_ns", rangeModel.rebuildSequence)
	measurePerf(t, "synthetic-range", "view_ns", func() { _ = rangeModel.View() })
	recordPerf(t, "synthetic-range", "commits", int64(len(rangeModel.sequence.commits)))

	baseModel := model{width: 160, height: 50, wrap: true, screen: "base", history: historyState{changes: changes, current: git.CommitInfo{Hash: "base"}}}
	baseModel.rebuildCommit()
	measurePerf(t, "synthetic-base", "build_ns", baseModel.rebuildCommit)
	measurePerf(t, "synthetic-base", "view_ns", func() { _ = baseModel.View() })

	historyModel := model{width: 160, height: 50, screen: "history"}
	historyModel.history.commits = make([]git.CommitInfo, 500)
	for i := range historyModel.history.commits {
		historyModel.history.commits[i] = git.CommitInfo{Hash: fmt.Sprintf("%08x", i), Subject: fmt.Sprintf("commit %d", i)}
	}
	measurePerf(t, "synthetic-history", "view_ns", func() { _ = historyModel.View() })
	historyModel.history.list.filter = "commit 4"
	measurePerf(t, "synthetic-history", "filtered_view_ns", func() { _ = historyModel.View() })
	recordPerf(t, "synthetic-history", "commits", 500)
}

func measureSyntheticScenarios(t *testing.T) {
	scenarios := map[string][]git.Change{
		"huge-file": func() []git.Change {
			return []git.Change{{Path: "huge.txt", Diff: "@@ -0,0 +1,100000 @@\n" + strings.Repeat("+a line in a very large file\n", 100000)}}
		}(),
		"long-lines": func() []git.Change {
			changes := make([]git.Change, 100)
			for i := range changes {
				changes[i] = git.Change{Path: fmt.Sprintf("minified-%02d.js", i), Diff: "@@ -0,0 +1 @@\n+" + strings.Repeat("abcdefghij", 2000)}
			}
			return changes
		}(),
		"many-small": func() []git.Change {
			changes := make([]git.Change, 25)
			for i := range changes {
				changes[i] = git.Change{Path: fmt.Sprintf("small-%02d.go", i), Diff: "@@ -0,0 +1,10 @@\n" + strings.Repeat("+var value = 1\n", 10)}
			}
			return changes
		}(),
	}
	for name, changes := range scenarios {
		movedIndex := newMovedIndex()
		measurePerf(t, "synthetic-"+name, "moved_index_ns", func() {
			movedIndex = newMovedIndex()
			movedIndex.addChanges(changes)
		})
		m := model{width: 160, height: 50, wrap: true, snapshot: git.Snapshot{Unstaged: changes}}
		m.rebuild()
		measurePerf(t, "synthetic-"+name, "build_ns", func() { m.rebuild() })
		measurePerf(t, "synthetic-"+name, "view_ns", func() { _ = m.View() })
		measurePerf(t, "synthetic-"+name, "search_miss_ns", func() { _, _ = m.findMatch("unlikelymissingpattern", -1, 1) })
		measurePerf(t, "synthetic-"+name, "wrap_toggle_ns", func() { _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}) })
		recordPerf(t, "synthetic-"+name, "rows", int64(len(m.rows)))
	}
}

func measureRefreshPerf(t *testing.T) {
	t.Helper()
	repo := testRepo(t)
	const files = 1000
	paths := make([]string, files)
	for i := range paths {
		paths[i] = fmt.Sprintf("file-%03d.go", i)
		writeFile(t, repo, paths[i], "package main\n"+strings.Repeat("var value = 1\n", 50))
	}
	if _, err := repo.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		writeFile(t, repo, path, "package main\n"+strings.Repeat("var value = 2\n", 50))
	}
	cache, _, err := repo.LoadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, paths[0], "package main\nvar value = 3\n")
	var commands atomic.Int64
	repo.CommandHook = func([]string) { commands.Add(1) }
	next, unchanged, err := repo.LoadCached(cache)
	if err != nil || unchanged || len(next.Snapshot.Unstaged) != files {
		t.Fatal("refresh fixture did not load changed files")
	}
	recordPerf(t, "synthetic-refresh", "git_commands", commands.Load())
	m := model{width: 160, height: 50, wrap: true}
	m.snapshot = cache.Snapshot
	measurePerf(t, "synthetic-refresh", "full_rebuild_ns", func() { m.rebuild() })
	m.snapshot = cache.Snapshot
	nextChanges := next.Snapshot
	m.previousChanges = append([]git.Change(nil), cache.Snapshot.Unstaged...)
	measurePerf(t, "synthetic-refresh", "spliced_refresh_ns", func() {
		m.snapshot = cache.Snapshot
		m.previousChanges = append(m.previousChanges[:0], cache.Snapshot.Unstaged...)
		m.snapshot = nextChanges
		m.refreshRows()
		m.snapshot = cache.Snapshot
	})
	measurePerf(t, "synthetic-refresh", "unchanged_refresh_ns", func() {
		updated, _ := m.Update(loadedUnchanged{generation: m.generation, cache: cache})
		m = updated.(model)
	})
}

func measureSyntaxPerf(t *testing.T) {
	t.Helper()
	repo := testRepo(t)
	for i := 0; i < git.SyntaxBudgetFiles; i++ {
		writeFile(t, repo, fmt.Sprintf("file-%02d.go", i), "package main\nvar value = 1\n")
	}
	if _, err := repo.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < git.SyntaxBudgetFiles; i++ {
		writeFile(t, repo, fmt.Sprintf("file-%02d.go", i), "package main\nvar value = 2\n")
	}
	if _, err := repo.Commit("changes"); err != nil {
		t.Fatal(err)
	}
	changes, err := repo.CommitChangesWithoutSyntax("HEAD")
	if err != nil || len(changes) != git.SyntaxBudgetFiles {
		t.Fatalf("syntax fixture: %d changes, %v", len(changes), err)
	}
	var commands atomic.Int64
	repo.CommandHook = func([]string) { commands.Add(1) }
	key := git.Context{Commit: "HEAD"}
	measurePerf(t, "synthetic-syntax", "syntax_load_ns", func() { _ = repo.SyntaxDiffs(changes, key) })
	commands.Store(0)
	_ = repo.SyntaxDiffs(changes, key)
	recordPerf(t, "synthetic-syntax", "syntax_git_commands", commands.Load())
	targets := make([]syntaxTarget, 3)
	for i := range targets {
		targets[i] = syntaxTarget{key: git.Context{Commit: "HEAD", Path: changes[i].Path}, change: changes[i]}
	}
	m := model{repo: repo, screen: "commit", history: historyState{current: git.CommitInfo{Hash: "HEAD"}}}
	measurePerf(t, "synthetic-syntax", "demand_load_ns", func() { _ = m.loadSyntaxTargets(targets)() })
	commands.Store(0)
	_ = m.loadSyntaxTargets(targets)()
	recordPerf(t, "synthetic-syntax", "demand_git_commands", commands.Load())
	largeDiff := "diff --git a/large.go b/large.go\nnew file mode 100644\n@@ -0,0 +1,2000 @@\n" + strings.Repeat("+var value = 1 // comment\n", 2000)
	measurePerf(t, "synthetic-syntax", "whole_file_tokenize_ns", func() {
		syntax.ResetCache()
		syntax.Lines("large.go", largeDiff)
	})
}

func BenchmarkWorkingTreeRefresh(b *testing.B) {
	base := make([]git.Change, 1000)
	for i := range base {
		base[i] = git.Change{Path: fmt.Sprintf("file-%04d.go", i), Diff: "@@ -0,0 +1,50 @@\n" + strings.Repeat("+var value = 2\n", 50)}
	}
	next := append([]git.Change(nil), base...)
	next[0].Diff = "@@ -0,0 +1,51 @@\n" + strings.Repeat("+var value = 3\n", 51)
	m := model{width: 160, height: 50, wrap: true, snapshot: git.Snapshot{Unstaged: base}}
	m.rebuild()
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.snapshot = git.Snapshot{Unstaged: next}
			m.rebuild()
		}
	})
	b.Run("splice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.snapshot = git.Snapshot{Unstaged: next}
			m.previousChanges = base
			m.refreshRows()
		}
	})
}

func measurePerf(t *testing.T, scenario, metric string, f func()) {
	t.Helper()
	f()
	values := make([]int64, 5)
	for i := range values {
		start := time.Now()
		f()
		values[i] = time.Since(start).Nanoseconds()
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	recordPerf(t, scenario, metric, values[len(values)/2])
}

// measurePerfOnce records a single run for work that mutates state and so
// cannot be repeated without changing what it measures.
func measurePerfOnce(t *testing.T, scenario, metric string, f func()) {
	t.Helper()
	start := time.Now()
	f()
	recordPerf(t, scenario, metric, time.Since(start).Nanoseconds())
}

func recordPerf(t *testing.T, scenario, metric string, value int64) {
	t.Helper()
	t.Logf("%s %s %d", scenario, metric, value)
	path := os.Getenv("PANE_PERF_LOG")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	revision, err := exec.Command("git", "describe", "--always", "--dirty").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(file, "%s\t%s\t%s\t%s\t%d\t%s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(revision)), scenario, metric, value, os.Getenv("PANE_PERF_RUN")); err != nil {
		t.Fatal(err)
	}
}

// measureCommitRelease shows what a finished huge commit keeps alive and what
// dropping its rows and patch returns to the OS.
func measureCommitRelease(t *testing.T, scenario string, changes []git.Change) {
	t.Helper()
	m := model{width: 160, height: 50, wrap: true, screen: "commit", history: historyState{changes: changes}}
	m.rebuildCommit()
	rows := len(m.history.rows)
	if rows <= 500000 {
		return
	}
	var memory runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "sys_with_rows_bytes", int64(memory.Sys))
	recordPerf(t, scenario, "heap_with_rows_bytes", int64(memory.HeapAlloc))
	recordPerf(t, scenario, "rows_before_release", int64(rows))
	debug.FreeOSMemory()
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "sys_retained_bytes", int64(memory.Sys))
	m.releaseLargeCommitRows()
	runtime.ReadMemStats(&memory)
	recordPerf(t, scenario, "sys_after_release_bytes", int64(memory.Sys))
	recordPerf(t, scenario, "heap_after_release_bytes", int64(memory.HeapAlloc))
	recordPerf(t, scenario, "rows_after_release", int64(len(m.history.rows)))
}

// measureHotspotPerf measures the streamed hotspot view: the first page of
// commits and a warm cache launch. It uses the external repo when one is
// configured, otherwise this checkout.
func measureHotspotPerf(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := os.Getenv("PANE_PERF_REPO")
	if root == "" {
		root = "../.."
	}
	repo := git.Repo{Root: root}
	since, author := "1.year", ""
	cache := hotCachePath(repo.Root, since, author)
	summary := hotSummaryPath(repo.Root, since, author)
	clear := func() {
		_ = os.Remove(cache)
		_ = os.Remove(summary)
	}
	measurePerf(t, "hotspot", "first_page_ns", func() {
		clear()
		plan, err := planHotspots(repo, since, author)
		if err != nil {
			t.Fatal(err)
		}
		if plan.cacheHit {
			t.Fatal("cold hotspot plan was a cache hit")
		}
		page, _, err := plan.stream.NextPage(hotspotFirstPageSize)
		plan.stream.Close()
		if err != nil || len(page) == 0 {
			t.Fatalf("hotspot first page: %d commits, %v", len(page), err)
		}
	})
	measurePerf(t, "hotspot", "first_view_ns", func() {
		clear()
		plan, err := planHotspots(repo, since, author)
		if err != nil {
			t.Fatal(err)
		}
		page, _, err := plan.stream.NextPage(hotspotFirstPageSize)
		plan.stream.Close()
		if err != nil || len(page) == 0 {
			t.Fatalf("hotspot first view: %d commits, %v", len(page), err)
		}
		model := model{width: 160, height: 50, repo: repo, hotspots: hotspotState{commits: page, cutoff: plan.cutoff}}
		model.hotspots.files, model.hotspots.authors, model.hotspots.dirs, model.hotspots.hidden = aggregateHotspots(page, builtinPathMarks(page), plan.cutoff, time.Now())
		if view := model.hotView(); view == "" {
			t.Fatal("hotspot first view was empty")
		}
	})
	measurePerf(t, "hotspot", "full_ns", func() {
		clear()
		if _, _, err := hotspots(repo, since, author); err != nil {
			t.Fatal(err)
		}
	})
	clear()
	if _, _, err := hotspots(repo, since, author); err != nil {
		t.Fatal(err)
	}
	measurePerf(t, "hotspot", "cache_hit_ns", func() {
		plan, err := planHotspots(repo, since, author)
		if err != nil || !plan.cacheHit {
			t.Fatalf("hotspot cache hit: %v hit=%v", err, plan.cacheHit)
		}
	})
	measurePerf(t, "hotspot", "cache_view_ns", func() {
		plan, err := planHotspots(repo, since, author)
		if err != nil || !plan.cacheHit {
			t.Fatalf("hotspot cache view: %v hit=%v", err, plan.cacheHit)
		}
		model := model{width: 160, height: 50, repo: repo, hotspots: hotspotState{
			files: plan.files, authors: plan.authors,
			items: &hotItemsCache{items: plan.items, filled: len(plan.items) > 0, builtAt: time.Now()},
		}}
		if view := model.hotView(); view == "" {
			t.Fatal("hotspot cache view was empty")
		}
	})
}

func writePerfHeapProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	runtime.GC()
	return pprof.WriteHeapProfile(file)
}

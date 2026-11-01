package main

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
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPerf is opt-in because it performs repeated full builds of potentially large commits.
func TestPerf(t *testing.T) {
	if os.Getenv("PANE_PERF") != "1" {
		t.Skip("set PANE_PERF=1 to run")
	}
	var changes []change
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
			if err := writeHeapProfile(filepath.Join(dir, scenario+".mem.pprof")); err != nil {
				t.Error(err)
			}
			runtime.KeepAlive(m)
		})
	}
	if dir := os.Getenv("PANE_PERF_REPO"); dir != "" {
		repo := repository{root: dir}
		revision := os.Getenv("PANE_PERF_COMMIT")
		if revision == "" {
			revision = "HEAD"
		}
		var err error
		measurePerf(t, scenario, "load_ns", func() {
			changes, err = repo.commitChangesWithoutSyntax(revision)
			if err != nil {
				t.Fatal(err)
			}
		})
		measurePerf(t, scenario, "cancel_load_after_30ms_ns", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancelRepo := repo
			cancelRepo.context = ctx
			done := make(chan struct{})
			go func() {
				_, _ = cancelRepo.commitChangesWithoutSyntax(revision)
				close(done)
			}()
			time.Sleep(30 * time.Millisecond)
			cancel()
			<-done
		})
		measurePerf(t, scenario, "stream_first_chunk_ns", func() {
			ctx, cancel := context.WithCancel(context.Background())
			stream := repo.streamCommit(ctx, revision, startupSyntaxLines)
			chunk, ok := <-stream
			if !ok || chunk.err != nil || len(chunk.changes) == 0 {
				cancel()
				t.Fatalf("first stream chunk: %#v", chunk)
			}
			cancel()
			for range stream {
			}
		})
		measurePerf(t, scenario, "stream_total_ns", func() {
			for chunk := range repo.streamCommit(context.Background(), revision, startupSyntaxLines) {
				if chunk.err != nil {
					t.Fatal(chunk.err)
				}
			}
		})
		var gitCommands atomic.Int64
		repo.commandHook = func([]string) { gitCommands.Add(1) }
		if _, err := repo.commitChangesWithoutSyntax(revision); err != nil {
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
			changes = append(changes, change{path: fmt.Sprintf("file-%04d.txt", i), diff: "@@ -0,0 +1,100 @@\n" + strings.Repeat("+a representative line of text\n", 100)})
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
	measurePerf(t, scenario, "picker_key_ns", func() { picker.handlePickerKey("j", nil) })
	measurePerf(t, scenario, "picker_render_ns", func() { _ = picker.View() })
	measurePerf(t, scenario, "wrap_toggle_ns", func() { _, _ = view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}) })
	measurePerf(t, scenario, "whitespace_toggle_ns", func() {
		toggle := view
		toggle.repo.ignoreSpace = false
		_, _ = toggle.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	})
	if len(changes) > 0 {
		c := changes[0]
		key := contextKey{commit: view.history.current.hash, path: c.path}
		demand := view
		demand.syntaxDemand = &syntaxDemand{loaded: make(map[contextKey]demandSyntax), pending: make(map[contextKey]bool)}
		measurePerf(t, scenario, "syntax_demand_apply_ns", func() {
			demand.syntaxDemand.loaded = make(map[contextKey]demandSyntax)
			demand.applySyntaxDemandLoaded(syntaxDemandLoaded{results: []demandResult{{key: key, diff: c.diff, syntaxDiff: c.diff}}})
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
	measureSyntaxPerf(t)
	measureRefreshPerf(t)
	measureSyntheticScenarios(t)
	measureViewScenarios(t)
}

func measureViewScenarios(t *testing.T) {
	changes := make([]change, 25)
	for i := range changes {
		changes[i] = change{path: fmt.Sprintf("file-%02d.go", i), diff: "@@ -0,0 +1,10 @@\n" + strings.Repeat("+var value = 1\n", 10)}
	}
	rangeModel := model{width: 160, height: 50, wrap: true, screen: "sequence"}
	rangeModel.sequence.commits = make([]commitInfo, 100)
	rangeModel.sequence.changes = make([][]change, 100)
	rangeModel.sequence.states = make([]sequenceLoadState, 100)
	for i := range rangeModel.sequence.commits {
		rangeModel.sequence.commits[i] = commitInfo{hash: fmt.Sprintf("%08x", i)}
		rangeModel.sequence.states[i] = sequenceReady
		rangeModel.sequence.changes[i] = changes
	}
	rangeModel.rebuildSequence()
	measurePerf(t, "synthetic-range", "rebuild_ns", rangeModel.rebuildSequence)
	measurePerf(t, "synthetic-range", "view_ns", func() { _ = rangeModel.View() })
	recordPerf(t, "synthetic-range", "commits", int64(len(rangeModel.sequence.commits)))

	baseModel := model{width: 160, height: 50, wrap: true, screen: "base", history: historyState{changes: changes, current: commitInfo{hash: "base"}}}
	baseModel.rebuildCommit()
	measurePerf(t, "synthetic-base", "build_ns", baseModel.rebuildCommit)
	measurePerf(t, "synthetic-base", "view_ns", func() { _ = baseModel.View() })

	historyModel := model{width: 160, height: 50, screen: "history"}
	historyModel.history.commits = make([]commitInfo, 500)
	for i := range historyModel.history.commits {
		historyModel.history.commits[i] = commitInfo{hash: fmt.Sprintf("%08x", i), subject: fmt.Sprintf("commit %d", i)}
	}
	measurePerf(t, "synthetic-history", "view_ns", func() { _ = historyModel.View() })
	historyModel.history.filter = "commit 4"
	measurePerf(t, "synthetic-history", "filtered_view_ns", func() { _ = historyModel.View() })
	recordPerf(t, "synthetic-history", "commits", 500)
}

func measureSyntheticScenarios(t *testing.T) {
	scenarios := map[string][]change{
		"huge-file": func() []change {
			return []change{{path: "huge.txt", diff: "@@ -0,0 +1,100000 @@\n" + strings.Repeat("+a line in a very large file\n", 100000)}}
		}(),
		"long-lines": func() []change {
			changes := make([]change, 100)
			for i := range changes {
				changes[i] = change{path: fmt.Sprintf("minified-%02d.js", i), diff: "@@ -0,0 +1 @@\n+" + strings.Repeat("abcdefghij", 2000)}
			}
			return changes
		}(),
		"many-small": func() []change {
			changes := make([]change, 25)
			for i := range changes {
				changes[i] = change{path: fmt.Sprintf("small-%02d.go", i), diff: "@@ -0,0 +1,10 @@\n" + strings.Repeat("+var value = 1\n", 10)}
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
		m := model{width: 160, height: 50, wrap: true, snapshot: snapshot{unstaged: changes}}
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
	if _, err := repo.commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		writeFile(t, repo, path, "package main\n"+strings.Repeat("var value = 2\n", 50))
	}
	cache, _, err := repo.loadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, paths[0], "package main\nvar value = 3\n")
	var commands atomic.Int64
	repo.commandHook = func([]string) { commands.Add(1) }
	next, unchanged, err := repo.loadCached(cache)
	if err != nil || unchanged || len(next.snapshot.unstaged) != files {
		t.Fatal("refresh fixture did not load changed files")
	}
	recordPerf(t, "synthetic-refresh", "git_commands", commands.Load())
	m := model{width: 160, height: 50, wrap: true}
	m.snapshot = cache.snapshot
	measurePerf(t, "synthetic-refresh", "full_rebuild_ns", func() { m.rebuild() })
	m.snapshot = cache.snapshot
	nextChanges := next.snapshot
	m.previousChanges = append([]change(nil), cache.snapshot.unstaged...)
	measurePerf(t, "synthetic-refresh", "spliced_refresh_ns", func() {
		m.snapshot = cache.snapshot
		m.previousChanges = append(m.previousChanges[:0], cache.snapshot.unstaged...)
		m.snapshot = nextChanges
		m.refreshRows()
		m.snapshot = cache.snapshot
	})
	measurePerf(t, "synthetic-refresh", "unchanged_refresh_ns", func() {
		updated, _ := m.Update(loadedUnchanged{generation: m.generation, cache: cache})
		m = updated.(model)
	})
}

func measureSyntaxPerf(t *testing.T) {
	t.Helper()
	repo := testRepo(t)
	for i := 0; i < syntaxBudgetFiles; i++ {
		writeFile(t, repo, fmt.Sprintf("file-%02d.go", i), "package main\nvar value = 1\n")
	}
	if _, err := repo.commit("initial"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < syntaxBudgetFiles; i++ {
		writeFile(t, repo, fmt.Sprintf("file-%02d.go", i), "package main\nvar value = 2\n")
	}
	if _, err := repo.commit("changes"); err != nil {
		t.Fatal(err)
	}
	changes, err := repo.commitChangesWithoutSyntax("HEAD")
	if err != nil || len(changes) != syntaxBudgetFiles {
		t.Fatalf("syntax fixture: %d changes, %v", len(changes), err)
	}
	var commands atomic.Int64
	repo.commandHook = func([]string) { commands.Add(1) }
	key := contextKey{commit: "HEAD"}
	measurePerf(t, "synthetic-syntax", "syntax_load_ns", func() { _ = repo.syntaxDiffs(changes, key) })
	commands.Store(0)
	_ = repo.syntaxDiffs(changes, key)
	recordPerf(t, "synthetic-syntax", "syntax_git_commands", commands.Load())
	targets := make([]syntaxTarget, 3)
	for i := range targets {
		targets[i] = syntaxTarget{key: contextKey{commit: "HEAD", path: changes[i].path}, change: changes[i]}
	}
	m := model{repo: repo, screen: "commit", history: historyState{current: commitInfo{hash: "HEAD"}}}
	measurePerf(t, "synthetic-syntax", "demand_load_ns", func() { _ = m.loadSyntaxTargets(targets)() })
	commands.Store(0)
	_ = m.loadSyntaxTargets(targets)()
	recordPerf(t, "synthetic-syntax", "demand_git_commands", commands.Load())
	largeDiff := "diff --git a/large.go b/large.go\nnew file mode 100644\n@@ -0,0 +1,2000 @@\n" + strings.Repeat("+var value = 1 // comment\n", 2000)
	measurePerf(t, "synthetic-syntax", "whole_file_tokenize_ns", func() {
		syntaxCache.Lock()
		syntaxCache.entries = make(map[syntaxKey]syntaxResult)
		syntaxCache.Unlock()
		syntaxLines("large.go", largeDiff)
	})
}

func BenchmarkWorkingTreeRefresh(b *testing.B) {
	base := make([]change, 1000)
	for i := range base {
		base[i] = change{path: fmt.Sprintf("file-%04d.go", i), diff: "@@ -0,0 +1,50 @@\n" + strings.Repeat("+var value = 2\n", 50)}
	}
	next := append([]change(nil), base...)
	next[0].diff = "@@ -0,0 +1,51 @@\n" + strings.Repeat("+var value = 3\n", 51)
	m := model{width: 160, height: 50, wrap: true, snapshot: snapshot{unstaged: base}}
	m.rebuild()
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.snapshot = snapshot{unstaged: next}
			m.rebuild()
		}
	})
	b.Run("splice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.snapshot = snapshot{unstaged: next}
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
func measureCommitRelease(t *testing.T, scenario string, changes []change) {
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

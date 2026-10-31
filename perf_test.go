package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
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
	scenario := "synthetic-many"
	if dir := os.Getenv("PANE_PERF_REPO"); dir != "" {
		scenario = "commit"
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
	} else {
		for i := 0; i < 500; i++ {
			changes = append(changes, change{path: fmt.Sprintf("file-%04d.txt", i), diff: "@@ -0,0 +1,100 @@\n" + strings.Repeat("+a representative line of text\n", 100)})
		}
	}
	m := model{width: 160, height: 50, wrap: true, screen: "commit", history: historyState{changes: changes}}
	measurePerf(t, scenario, "build_ns", func() { m.rebuildCommit() })
	view := m.commitModel()
	measurePerf(t, scenario, "view_ns", func() { _ = view.View() })
	measurePerf(t, scenario, "search_miss_ns", func() { _, _ = view.findMatch("unlikelymissingpattern", -1, 1) })
	measurePerf(t, scenario, "search_smartcase_miss_ns", func() { _, _ = view.findMatch("UnlikelyMissingPattern", -1, 1) })
	measurePerf(t, scenario, "resize_height_ns", func() { _, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 51}) })
	measurePerf(t, scenario, "resize_width_ns", func() { _, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 50}) })
	measurePerf(t, scenario, "resize_width_apply_ns", func() { _, _ = m.Update(resizeRebuild{request: m.resizeRequest}) })
	recordPerf(t, scenario, "rows", int64(len(view.rows)))
	recordPerf(t, scenario, "row_bytes", int64(unsafe.Sizeof(row{})))
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
	if _, err := fmt.Fprintf(file, "%s\t%s\t%s\t%s\t%d\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(revision)), scenario, metric, value); err != nil {
		t.Fatal(err)
	}
}

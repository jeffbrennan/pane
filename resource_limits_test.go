package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jeffbrennan/pane/internal/git"
)

func TestBlurredPollingBacksOffAndResets(t *testing.T) {
	m := model{blurred: true}
	if got := m.blurredRefreshInterval(); got != blurredRefreshTicks {
		t.Fatalf("initial interval %d", got)
	}
	m.backOffBlurredRefresh()
	if got := m.blurredRefreshInterval(); got != blurredRefreshTicks*2 {
		t.Fatalf("first backoff %d", got)
	}
	m.backOffBlurredRefresh()
	m.backOffBlurredRefresh()
	m.backOffBlurredRefresh()
	if got := m.blurredRefreshInterval(); got != blurredRefreshMax {
		t.Fatalf("interval did not cap: %d", got)
	}
	m.resetBlurredRefresh()
	if got := m.blurredRefreshInterval(); got != blurredRefreshTicks {
		t.Fatalf("reset interval %d", got)
	}
	focused := model{}
	focused.backOffBlurredRefresh()
	if focused.blurredInterval != 0 {
		t.Fatal("a focused model should not back off")
	}
}

func TestWorktreesShareStatsCache(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.Command("worktree", "add", "-b", "side", worktree); err != nil {
		t.Fatal(err)
	}
	rootDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(r.Root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(rootDir) })
	mainRepo, err := git.OpenRepository()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(worktree); err != nil {
		t.Fatal(err)
	}
	sideRepo, err := git.OpenRepository()
	if err != nil {
		t.Fatal(err)
	}
	if sideRepo.Root == mainRepo.Root {
		t.Fatal("worktree root was not detected")
	}
	if sideRepo.Stats == nil || mainRepo.Stats == nil || sideRepo.Stats.Path != mainRepo.Stats.Path {
		t.Fatalf("stats caches differ: %#v %#v", mainRepo.Stats, sideRepo.Stats)
	}
}

func TestReleaseLargeCommitRows(t *testing.T) {
	m := model{history: historyState{rows: make([]row, 500001), changes: []Change{{Path: "file"}}}}
	m.releaseLargeCommitRows()
	if len(m.history.rows) != 0 || m.history.changes != nil {
		t.Fatalf("rows=%d changes=%d", len(m.history.rows), len(m.history.changes))
	}
	t.Setenv("PANE_KEEP_COMMIT_ROWS", "1")
	kept := model{history: historyState{rows: make([]row, 500001), changes: []Change{{Path: "file"}}}}
	kept.releaseLargeCommitRows()
	if len(kept.history.rows) != 500001 {
		t.Fatal("PANE_KEEP_COMMIT_ROWS did not retain the rows")
	}
}

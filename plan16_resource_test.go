package main

import (
	"os"
	"path/filepath"
	"testing"
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
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.command("worktree", "add", "-b", "side", worktree); err != nil {
		t.Fatal(err)
	}
	rootDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(r.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(rootDir) })
	mainRepo, err := openRepository()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(worktree); err != nil {
		t.Fatal(err)
	}
	sideRepo, err := openRepository()
	if err != nil {
		t.Fatal(err)
	}
	if sideRepo.root == mainRepo.root {
		t.Fatal("worktree root was not detected")
	}
	if sideRepo.stats == nil || mainRepo.stats == nil || sideRepo.stats.path != mainRepo.stats.path {
		t.Fatalf("stats caches differ: %#v %#v", mainRepo.stats, sideRepo.stats)
	}
}

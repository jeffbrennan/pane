package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func branchByName(entries []BranchEntry, name string) *BranchEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func TestBranchesListMarksCurrentAndWorktrees(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("branch", "side"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.Command("worktree", "add", worktree, "side"); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Branches(false)
	if err != nil {
		t.Fatal(err)
	}
	main := branchByName(entries, "main")
	side := branchByName(entries, "side")
	if main == nil || !main.Current || main.Worktree != "" {
		t.Fatalf("main entry: %#v", main)
	}
	if side == nil || side.Current || evalPath(t, side.Worktree) != evalPath(t, worktree) {
		t.Fatalf("side entry: %#v", side)
	}
}

func TestWorktreePorcelainAndPathConvention(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	first := filepath.Join(parent, "one")
	second := filepath.Join(parent, "two")
	if _, err := r.Command("worktree", "add", "-b", "one", first); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("worktree", "add", "-b", "two", second); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Worktrees()
	if err != nil || len(entries) != 3 {
		t.Fatalf("worktrees: %#v %v", entries, err)
	}
	common := evalPath(t, filepath.Dir(parent))
	if got := commonWorktreeDir(entries); got != common {
		t.Fatalf("common dir %q, want %q", got, common)
	}
	if got := WorktreePathFor(entries, r.Root, "feature/x"); got != filepath.Join(common, "feature-x") {
		t.Fatalf("path convention %q", got)
	}
	if got := WorktreeDisplayPath(entries, evalPath(t, first)); !strings.HasSuffix(got, string(filepath.Separator)+"one") && got != "one" {
		t.Fatalf("display path %q", got)
	}
	if _, err := r.Command("worktree", "remove", first); err != nil {
		t.Fatal(err)
	}
	entries, _ = r.Worktrees()
	if len(entries) != 2 {
		t.Fatalf("remove left %d worktrees", len(entries))
	}
}

func TestWorktreeStatusCountsDirtyFiles(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	dirty, _, err := r.StatusCount(r.Root)
	if err != nil || dirty != 0 {
		t.Fatalf("clean: %d %v", dirty, err)
	}
	writeFile(t, r, "file.txt", "two\n")
	writeFile(t, r, "new.txt", "new\n")
	dirty, activity, err := r.StatusCount(r.Root)
	if err != nil || dirty != 2 {
		t.Fatalf("dirty: %d %v", dirty, err)
	}
	if activity.IsZero() {
		t.Fatal("no activity time")
	}
	future := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(r.Root, "new.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	_, activity, err = r.StatusCount(r.Root)
	if err != nil {
		t.Fatal(err)
	}
	if activity.Before(future) {
		t.Fatalf("activity %v did not use the newest changed path %v", activity, future)
	}
}

func TestStatusCountTreatsRenamesAsOneChange(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "old.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("mv", "old.txt", "new.txt"); err != nil {
		t.Fatal(err)
	}
	dirty, activity, err := r.StatusCount(r.Root)
	if err != nil || dirty != 1 {
		t.Fatalf("rename: %d %v", dirty, err)
	}
	if activity.IsZero() {
		t.Fatal("no activity for rename")
	}
}

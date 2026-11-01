package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func branchByName(entries []branchEntry, name string) *branchEntry {
	for i := range entries {
		if entries[i].name == name {
			return &entries[i]
		}
	}
	return nil
}

func TestBranchesListMarksCurrentAndWorktrees(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("branch", "side"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.command("worktree", "add", worktree, "side"); err != nil {
		t.Fatal(err)
	}
	entries, err := r.branches(false)
	if err != nil {
		t.Fatal(err)
	}
	main := branchByName(entries, "main")
	side := branchByName(entries, "side")
	if main == nil || !main.current || main.worktree != "" {
		t.Fatalf("main entry: %#v", main)
	}
	if side == nil || side.current || evalPath(t, side.worktree) != evalPath(t, worktree) {
		t.Fatalf("side entry: %#v", side)
	}
}

func TestSwitchBranch(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("branch", "other"); err != nil {
		t.Fatal(err)
	}
	entries, err := r.branches(false)
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 80, height: 20, screen: "branches", repo: r, branch: branchState{entries: entries}}
	updated, cmd := m.switchBranch(*branchByName(entries, "other"))
	m = updated.(model)
	if cmd == nil {
		t.Fatal("switch produced no command")
	}
	if msg, ok := cmd().(branchActionDone); !ok || msg.err != nil {
		t.Fatalf("switch failed: %#v", msg)
	}
	head, err := r.command("symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != "other" {
		t.Fatalf("HEAD after switch: %q %v", head, err)
	}
}

func TestSwitchRefusesCheckedOutElsewhere(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("branch", "side"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.command("worktree", "add", worktree, "side"); err != nil {
		t.Fatal(err)
	}
	entries, _ := r.branches(false)
	m := model{screen: "branches", repo: r, branch: branchState{entries: entries}}
	updated, cmd := m.switchBranch(*branchByName(entries, "side"))
	m = updated.(model)
	if cmd != nil || !strings.Contains(m.flash, "Checked out in") {
		t.Fatalf("switch was not refused: %q", m.flash)
	}
}

func TestCreateAndDeleteBranches(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	m := model{screen: "branches", repo: r}
	updated, cmd := m.createBranch("feature/x", "HEAD")
	if cmd == nil {
		t.Fatal("create produced no command")
	}
	if msg := cmd().(branchActionDone); msg.err != nil {
		t.Fatalf("create: %v", msg.err)
	}
	if out, err := r.command("symbolic-ref", "--short", "HEAD"); err != nil || strings.TrimSpace(string(out)) != "feature/x" {
		t.Fatalf("HEAD %q %v", out, err)
	}
	if _, err := r.command("switch", "main"); err != nil {
		t.Fatal(err)
	}

	entries, _ := r.branches(false)
	merged := *branchByName(entries, "feature/x")
	_ = updated
	updated, cmd = m.deleteBranch(merged, false)
	if cmd == nil {
		t.Fatal("delete produced no command")
	}
	if msg := cmd().(branchActionDone); msg.err != nil {
		t.Fatalf("delete merged: %v", msg.err)
	}
	if out, _ := r.command("rev-parse", "--verify", "--quiet", "refs/heads/feature/x"); len(out) != 0 {
		t.Fatal("branch still exists")
	}
}

func TestDeleteUnmergedRequiresForce(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("switch", "-c", "unmerged"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "other.txt", "two\n")
	if _, err := r.commit("diverged"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("switch", "main"); err != nil {
		t.Fatal(err)
	}
	entries, _ := r.branches(false)
	entry := *branchByName(entries, "unmerged")
	m := model{screen: "branches", repo: r}
	_, cmd := m.deleteBranch(entry, false)
	if msg := cmd().(branchActionDone); msg.err == nil {
		t.Fatal("unmerged branch deleted without force")
	}
	_, cmd = m.deleteBranch(entry, true)
	if msg := cmd().(branchActionDone); msg.err != nil {
		t.Fatalf("forced delete: %v", msg.err)
	}
}

func TestDeleteRefusesCurrentAndWorktreeBranches(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("branch", "side"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.command("worktree", "add", worktree, "side"); err != nil {
		t.Fatal(err)
	}
	m := model{screen: "branches", repo: r}
	updated, cmd := m.confirmDeleteBranch(branchEntry{name: "main", current: true}, false)
	m = updated.(model)
	if cmd != nil || m.modal != "" || !strings.Contains(m.flash, "checked out") {
		t.Fatalf("current branch: %q %q", m.flash, m.modal)
	}
	updated, cmd = m.confirmDeleteBranch(branchEntry{name: "side", worktree: worktree}, false)
	m = updated.(model)
	if cmd != nil || m.modal != "" || !strings.Contains(m.flash, "Checked out") {
		t.Fatalf("worktree branch: %q %q", m.flash, m.modal)
	}
}

func TestWorktreePorcelainAndPathConvention(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	first := filepath.Join(parent, "one")
	second := filepath.Join(parent, "two")
	if _, err := r.command("worktree", "add", "-b", "one", first); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("worktree", "add", "-b", "two", second); err != nil {
		t.Fatal(err)
	}
	entries, err := r.worktrees()
	if err != nil || len(entries) != 3 {
		t.Fatalf("worktrees: %#v %v", entries, err)
	}
	common := evalPath(t, filepath.Dir(parent))
	if got := commonWorktreeDir(entries); got != common {
		t.Fatalf("common dir %q, want %q", got, common)
	}
	if got := worktreePathFor(entries, r.root, "feature/x"); got != filepath.Join(common, "feature-x") {
		t.Fatalf("path convention %q", got)
	}
	if got := worktreeDisplayPath(entries, evalPath(t, first)); !strings.HasSuffix(got, string(filepath.Separator)+"one") && got != "one" {
		t.Fatalf("display path %q", got)
	}
	if _, err := r.command("worktree", "remove", first); err != nil {
		t.Fatal(err)
	}
	entries, _ = r.worktrees()
	if len(entries) != 2 {
		t.Fatalf("remove left %d worktrees", len(entries))
	}
}

func TestWorktreeAddRemoveRefusals(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wt")
	m := model{screen: "worktrees", repo: r}
	updated, cmd := m.promptNewWorktree("", "", false)
	m = updated.(model)
	m.input = path + "  newbranch"
	updated, cmd = m.createWorktree()
	m = updated.(model)
	if cmd == nil {
		t.Fatal("create worktree produced no command")
	}
	if msg := cmd().(worktreeActionDone); msg.err != nil {
		t.Fatalf("create worktree: %v", msg.err)
	}
	entries, _ := r.worktrees()
	entry := *worktreeEntryFor(entries, evalPath(t, path))
	_, cmd = m.removeWorktree(entry, false)
	if msg := cmd().(worktreeActionDone); msg.err != nil {
		t.Fatalf("remove clean worktree: %v", msg.err)
	}

	// A dirty worktree is refused without force.
	dirty := filepath.Join(t.TempDir(), "dirty")
	if _, err := r.command("worktree", "add", "-b", "dirty", dirty); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty, "file.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, _ = r.worktrees()
	dirtyEntry := *worktreeEntryFor(entries, evalPath(t, dirty))
	_, cmd = m.removeWorktree(dirtyEntry, false)
	if msg := cmd().(worktreeActionDone); msg.err == nil {
		t.Fatal("dirty worktree removed without force")
	}
	_, cmd = m.removeWorktree(dirtyEntry, true)
	if msg := cmd().(worktreeActionDone); msg.err != nil {
		t.Fatalf("force remove: %v", msg.err)
	}
}

func TestSwitchWorktreeKeepsSessionToggles(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.command("worktree", "add", "-b", "side", worktree); err != nil {
		t.Fatal(err)
	}
	rootDir, _ := os.Getwd()
	if err := os.Chdir(r.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(rootDir) })
	m := model{width: 100, height: 30, wrap: false, lightTheme: true, themeAuto: false, screen: "worktrees", repo: r}
	updated, cmd := m.switchWorktree(worktree)
	next := updated.(model)
	if evalPath(t, next.repo.root) != evalPath(t, worktree) || next.wrap || !next.lightTheme || next.themeAuto || next.screen != "" {
		t.Fatalf("session state: %#v", next)
	}
	if cmd == nil || !next.loading {
		t.Fatal("switch did not start loading")
	}
}

func TestWorktreeStatusCountsDirtyFiles(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	dirty, _, err := r.statusCount(r.root)
	if err != nil || dirty != 0 {
		t.Fatalf("clean: %d %v", dirty, err)
	}
	writeFile(t, r, "file.txt", "two\n")
	writeFile(t, r, "new.txt", "new\n")
	dirty, activity, err := r.statusCount(r.root)
	if err != nil || dirty != 2 {
		t.Fatalf("dirty: %d %v", dirty, err)
	}
	if activity.IsZero() {
		t.Fatal("no activity time")
	}
}

func TestHistoryDecoratesOtherWorktreeHeads(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.command("worktree", "add", "-b", "side", filepath.Join(t.TempDir(), "wt")); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history(0, historyPage)
	if err != nil {
		t.Fatal(err)
	}
	heads := map[string]string{commits[0].hash: "side"}
	m := model{width: 160, height: 20, screen: "history", repo: r,
		history: historyState{commits: commits}, worktreeHeads: heads}
	if view := m.View(); !strings.Contains(view, "⌂ side") {
		t.Fatalf("missing worktree decoration: %q", view)
	}
}

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func evalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func branchByName(entries []git.BranchEntry, name string) *git.BranchEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func TestSwitchBranch(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("branch", "other"); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Branches(false)
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
	head, err := r.Command("symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != "other" {
		t.Fatalf("HEAD after switch: %q %v", head, err)
	}
}

func TestSwitchRefusesCheckedOutElsewhere(t *testing.T) {
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
	entries, _ := r.Branches(false)
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
	if _, err := r.Commit("initial"); err != nil {
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
	if out, err := r.Command("symbolic-ref", "--short", "HEAD"); err != nil || strings.TrimSpace(string(out)) != "feature/x" {
		t.Fatalf("HEAD %q %v", out, err)
	}
	if _, err := r.Command("switch", "main"); err != nil {
		t.Fatal(err)
	}

	entries, _ := r.Branches(false)
	merged := *branchByName(entries, "feature/x")
	_ = updated
	updated, cmd = m.deleteBranch(merged, false)
	if cmd == nil {
		t.Fatal("delete produced no command")
	}
	if msg := cmd().(branchActionDone); msg.err != nil {
		t.Fatalf("delete merged: %v", msg.err)
	}
	if out, _ := r.Command("rev-parse", "--verify", "--quiet", "refs/heads/feature/x"); len(out) != 0 {
		t.Fatal("branch still exists")
	}
}

func TestDeleteUnmergedRequiresForce(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("switch", "-c", "unmerged"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "other.txt", "two\n")
	if _, err := r.Commit("diverged"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("switch", "main"); err != nil {
		t.Fatal(err)
	}
	entries, _ := r.Branches(false)
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
	m := model{screen: "branches", repo: r}
	updated, cmd := m.confirmDeleteBranch(git.BranchEntry{Name: "main", Current: true}, false)
	m = updated.(model)
	if cmd != nil || m.modal != "" || !strings.Contains(m.flash, "checked out") {
		t.Fatalf("current branch: %q %q", m.flash, m.modal)
	}
	updated, cmd = m.confirmDeleteBranch(git.BranchEntry{Name: "side", Worktree: worktree}, false)
	m = updated.(model)
	if cmd != nil || m.modal != "" || !strings.Contains(m.flash, "Checked out") {
		t.Fatalf("worktree branch: %q %q", m.flash, m.modal)
	}
}

func TestWorktreeAddRemoveRefusals(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
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
	entries, _ := r.Worktrees()
	entry := *worktreeEntryFor(entries, evalPath(t, path))
	_, cmd = m.removeWorktree(entry, false)
	if msg := cmd().(worktreeActionDone); msg.err != nil {
		t.Fatalf("remove clean worktree: %v", msg.err)
	}

	// A dirty worktree is refused without force.
	dirty := filepath.Join(t.TempDir(), "dirty")
	if _, err := r.Command("worktree", "add", "-b", "dirty", dirty); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty, "file.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, _ = r.Worktrees()
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
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "wt")
	if _, err := r.Command("worktree", "add", "-b", "side", worktree); err != nil {
		t.Fatal(err)
	}
	rootDir, _ := os.Getwd()
	if err := os.Chdir(r.Root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(rootDir) })
	m := model{width: 100, height: 30, wrap: false, lightTheme: true, themeAuto: false, screen: "worktrees", repo: r}
	updated, cmd := m.switchWorktree(worktree)
	next := updated.(model)
	if evalPath(t, next.repo.Root) != evalPath(t, worktree) || next.wrap || !next.lightTheme || next.themeAuto || next.screen != "" {
		t.Fatalf("session state: %#v", next)
	}
	if cmd == nil || !next.loading {
		t.Fatal("switch did not start loading")
	}
}

func TestHistoryDecoratesOtherWorktreeHeads(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "one\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Command("worktree", "add", "-b", "side", filepath.Join(t.TempDir(), "wt")); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, git.HistoryPage)
	if err != nil {
		t.Fatal(err)
	}
	heads := map[string]string{commits[0].Hash: "side"}
	m := model{width: 160, height: 20, screen: "history", repo: r,
		history: historyState{commits: commits}, worktreeHeads: heads}
	if view := m.View(); !strings.Contains(view, "⌂ side") {
		t.Fatalf("missing worktree decoration: %q", view)
	}
}

func TestBranchAndWorktreeScreensRender(t *testing.T) {
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
	m := model{width: 120, height: 20, repo: r}
	updated, cmd := m.openBranches()
	m = updated.(model)
	m, _ = applyBatch(t, m, cmd)
	view := m.View()
	if !strings.Contains(view, "branches") || !strings.Contains(view, "main") || !strings.Contains(view, "side") {
		t.Fatalf("branch view: %q", view)
	}
	updated, cmd = m.handleBranchKey(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	m, _ = applyBatch(t, m, cmd)
	view = m.View()
	if m.screen != "worktrees" || !strings.Contains(view, "worktrees") || !strings.Contains(view, "side") {
		t.Fatalf("worktree view: %q", view)
	}
}

func TestWorktreeForceDialogNamesDirtyCount(t *testing.T) {
	m := model{width: 100, height: 30, modal: "worktree-delete",
		worktrees: worktreeState{
			confirm:      git.WorktreeEntry{Path: "/tmp/wt"},
			confirmForce: true,
			status:       map[string]worktreeStatus{"/tmp/wt": {dirty: 7, loaded: true}},
		}}
	view := m.View()
	if !strings.Contains(view, "7 changed files will be lost") {
		t.Fatalf("force dialog: %q", view)
	}
}

func TestBranchAndWorktreeFiltersUsePickerMatcher(t *testing.T) {
	branches := model{branch: branchState{entries: []git.BranchEntry{{Name: "feature/x"}, {Name: "main"}}, filter: "fx"}}
	matches := branches.filteredBranches()
	if len(matches) != 1 || matches[0].Name != "feature/x" {
		t.Fatalf("branch subsequence filter: %#v", matches)
	}
	worktrees := model{worktrees: worktreeState{
		entries: []git.WorktreeEntry{{Path: "/tmp/pane-feature", Branch: "feature"}, {Path: "/tmp/other", Branch: "main"}},
		filter:  "pfe",
	}}
	sorted := worktrees.sortedWorktrees()
	if len(sorted) != 1 || sorted[0].Branch != "feature" {
		t.Fatalf("worktree subsequence filter: %#v", sorted)
	}
}

func TestPruneHintOnlyWhenPrunable(t *testing.T) {
	m := model{width: 120, height: 20, worktrees: worktreeState{entries: []git.WorktreeEntry{{Path: "/tmp/one", Branch: "main"}}}}
	if view := m.worktreeView(); strings.Contains(view, "prune") {
		t.Fatalf("prune hint shown without a prunable worktree: %q", view)
	}
	m.worktrees.entries = append(m.worktrees.entries, git.WorktreeEntry{Path: "/tmp/gone", Prunable: true})
	if view := m.worktreeView(); !strings.Contains(view, "prune") {
		t.Fatalf("prune hint missing: %q", view)
	}
}

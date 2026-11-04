package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

// isolateGitConfig keeps the user's global settings, such as pull.rebase, out
// of these tests and returns the global config file pane may write to.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[init]\n\tdefaultBranch = main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	return config
}

// sharedRemote makes a repository tracking origin/main and a second clone of
// the same remote standing in for another machine.
func sharedRemote(t *testing.T) (git.Repo, git.Repo) {
	t.Helper()
	r, branch := pushFixture(t, "origin")
	if branch != "main" {
		t.Fatalf("fixture branch is %q", branch)
	}
	if _, err := r.Action("push", "--set-upstream", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	remote, err := r.CommandStdout("remote", "get-url", "origin")
	if err != nil {
		t.Fatal(err)
	}
	other := git.Repo{Root: filepath.Join(t.TempDir(), "other")}
	if _, err := r.Action("clone", strings.TrimSpace(string(remote)), other.Root); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "user.email", "other@example.com"}, {"config", "user.name", "Other"}} {
		if _, err := other.Action(args...); err != nil {
			t.Fatal(err)
		}
	}
	return r, other
}

func commit(t *testing.T, r git.Repo, path, message string) {
	t.Helper()
	writeFile(t, r, path, message+"\n")
	if _, err := r.Commit(message); err != nil {
		t.Fatal(err)
	}
}

func revParse(t *testing.T, r git.Repo, rev string) string {
	t.Helper()
	out, err := r.CommandStdout("rev-parse", rev)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func press(t *testing.T, m model, key string) (model, tea.Cmd) {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	if key == "enter" {
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	}
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}

// run feeds a command's message back into the model, as Bubble Tea would.
func run(t *testing.T, m model, cmd tea.Cmd) (model, tea.Msg) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	return updated.(model), msg
}

// pushThrough presses p and runs the upstream probe and the push it starts.
func pushThrough(t *testing.T, m model) model {
	t.Helper()
	m, cmd := press(t, m, "p")
	updated, cmd := m.Update(cmd())
	m, _ = run(t, updated.(model), cmd)
	return m
}

func TestPullDivergedOffersMergeOrRebase(t *testing.T) {
	isolateGitConfig(t)
	r, other := sharedRemote(t)
	commit(t, other, "theirs.txt", "theirs")
	if _, err := other.Action("push"); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "mine.txt", "mine")

	m, cmd := press(t, model{width: 100, height: 30, repo: r}, "P")
	m, msg := run(t, m, cmd)
	if _, ok := msg.(pullFailed); !ok || m.modal != "pull-issue" || m.pull.Kind != git.PullDiverged {
		t.Fatalf("diverged pull: %#v modal=%q", msg, m.modal)
	}
	if m.pull.Ahead != 1 || m.pull.Behind != 1 {
		t.Fatalf("counts: ahead %d behind %d", m.pull.Ahead, m.pull.Behind)
	}
	if view := plain(m.View()); !strings.Contains(view, "Merge or rebase?") || !strings.Contains(view, "have diverged") {
		t.Fatalf("dialog: %q", view)
	}
	m, cmd = press(t, m, "r")
	if _, msg = run(t, m, cmd); msg.(completed).err != nil {
		t.Fatalf("rebase pull: %v", msg.(completed).err)
	}
	if parent := revParse(t, r, "HEAD~1"); parent != revParse(t, r, "origin/main") {
		t.Fatal("local commit was not rebased onto origin/main")
	}
}

func TestPullDivergedAlwaysSetsPullRebase(t *testing.T) {
	config := isolateGitConfig(t)
	r, other := sharedRemote(t)
	commit(t, other, "theirs.txt", "theirs")
	if _, err := other.Action("push"); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "mine.txt", "mine")
	m, cmd := press(t, model{width: 100, height: 30, repo: r}, "P")
	m, _ = run(t, m, cmd)
	m, cmd = press(t, m, "M")
	if _, msg := run(t, m, cmd); msg.(completed).err != nil {
		t.Fatalf("merge pull: %v", msg.(completed).err)
	}
	if parents := strings.Fields(revParse(t, r, "HEAD^@")); len(parents) != 2 {
		t.Fatalf("expected a merge commit, parents %v", parents)
	}
	if out, _ := os.ReadFile(config); !strings.Contains(string(out), "rebase = false") {
		t.Fatalf("pull.rebase not saved globally: %s", out)
	}
}

func TestPullUntrackedOffersSameNamedBranch(t *testing.T) {
	isolateGitConfig(t)
	r, other := sharedRemote(t)
	if _, err := other.Action("switch", "-c", "feat"); err != nil {
		t.Fatal(err)
	}
	commit(t, other, "feat.txt", "feat")
	if _, err := other.Action("push", "origin", "feat"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("switch", "-c", "feat"); err != nil {
		t.Fatal(err)
	}

	m, cmd := press(t, model{width: 100, height: 30, repo: r}, "P")
	m, _ = run(t, m, cmd)
	if m.modal != "pull-issue" || m.pull.Kind != git.PullUntracked || m.pull.Upstream != "origin/feat" {
		t.Fatalf("untracked pull: modal=%q %#v", m.modal, m.pull)
	}
	if view := plain(m.View()); !strings.Contains(view, "Track origin/feat?") {
		t.Fatalf("dialog: %q", view)
	}
	m, cmd = press(t, m, "enter")
	if _, msg := run(t, m, cmd); msg.(completed).err != nil {
		t.Fatalf("track and pull: %v", msg.(completed).err)
	}
	if got := upstream(t, r); got != "origin/feat" {
		t.Fatalf("upstream %q", got)
	}
	if revParse(t, r, "HEAD") != revParse(t, other, "HEAD") {
		t.Fatal("feat was not pulled")
	}
}

func TestPullWithoutRemoteBranchKeepsGitError(t *testing.T) {
	isolateGitConfig(t)
	r, _ := sharedRemote(t)
	if _, err := r.Action("switch", "-c", "local-only"); err != nil {
		t.Fatal(err)
	}
	_, cmd := press(t, model{width: 100, height: 30, repo: r}, "P")
	if done, ok := cmd().(completed); !ok || done.err == nil {
		t.Fatalf("pull with nothing to track should stay an error: %#v", done)
	}
}

func TestPushAfterAmendOffersForceWithLease(t *testing.T) {
	isolateGitConfig(t)
	r, _ := sharedRemote(t)
	commit(t, r, "mine.txt", "mine")
	if _, err := r.Action("push"); err != nil {
		t.Fatal(err)
	}
	pushed := revParse(t, r, "HEAD")
	if _, err := r.Action("commit", "--amend", "-m", "mine, amended"); err != nil {
		t.Fatal(err)
	}

	m := pushThrough(t, model{width: 100, height: 30, repo: r})
	if m.modal != "push-rejected" || m.pushIssue.Kind != git.PushRewritten || m.pushIssue.RemoteTip != pushed {
		t.Fatalf("amended push: modal=%q %#v", m.modal, m.pushIssue)
	}
	if view := plain(m.View()); !strings.Contains(view, "Force push main?") || !strings.Contains(view, "mine") || !strings.Contains(view, "--force-with-lease") {
		t.Fatalf("dialog: %q", view)
	}
	if _, cmd := press(t, m, "enter"); cmd != nil {
		t.Fatal("enter must not force push")
	}
	m, cmd := press(t, m, "f")
	if _, msg := run(t, m, cmd); msg.(completed).err != nil {
		t.Fatalf("force push: %v", msg.(completed).err)
	}
	if revParse(t, r, "origin/main") != revParse(t, r, "HEAD") {
		t.Fatal("remote was not replaced")
	}
}

func TestForcePushLeaseStopsWhenRemoteMoved(t *testing.T) {
	isolateGitConfig(t)
	r, other := sharedRemote(t)
	commit(t, r, "mine.txt", "mine")
	if _, err := r.Action("push"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "--amend", "-m", "mine, amended"); err != nil {
		t.Fatal(err)
	}
	_, issue, _ := r.Push()
	if issue == nil || issue.Kind != git.PushRewritten {
		t.Fatalf("expected a rewritten push: %#v", issue)
	}
	if _, err := other.Action("pull"); err != nil {
		t.Fatal(err)
	}
	commit(t, other, "theirs.txt", "theirs")
	if _, err := other.Action("push"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ForcePush(*issue); err == nil {
		t.Fatal("force push overwrote a commit pushed after the dialog opened")
	}
}

func TestPushBehindOffersPullThenPush(t *testing.T) {
	isolateGitConfig(t)
	r, other := sharedRemote(t)
	if _, err := r.Action("config", "pull.rebase", "false"); err != nil {
		t.Fatal(err)
	}
	commit(t, other, "theirs.txt", "theirs")
	if _, err := other.Action("push"); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "mine.txt", "mine")

	m := pushThrough(t, model{width: 100, height: 30, repo: r})
	if m.modal != "push-rejected" || m.pushIssue.Kind != git.PushBehind || len(m.pushIssue.Replaced) != 1 {
		t.Fatalf("behind push: modal=%q %#v", m.modal, m.pushIssue)
	}
	if view := plain(m.View()); !strings.Contains(view, "Pull before pushing?") || !strings.Contains(view, "theirs") {
		t.Fatalf("dialog: %q", view)
	}
	m, cmd := press(t, m, "enter")
	if _, msg := run(t, m, cmd); msg.(completed).err != nil {
		t.Fatalf("pull and push: %v", msg.(completed).err)
	}
	if revParse(t, r, "origin/main") != revParse(t, r, "HEAD") {
		t.Fatal("push did not follow the pull")
	}
}

func TestPushBehindStopsAtDivergedPull(t *testing.T) {
	isolateGitConfig(t)
	r, other := sharedRemote(t)
	commit(t, other, "theirs.txt", "theirs")
	if _, err := other.Action("push"); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "mine.txt", "mine")
	m := pushThrough(t, model{width: 100, height: 30, repo: r})
	m, cmd := press(t, m, "enter")
	m, _ = run(t, m, cmd)
	if m.modal != "pull-issue" || m.pull.Kind != git.PullDiverged {
		t.Fatalf("pull before push should ask how to reconcile: modal=%q %#v", m.modal, m.pull)
	}
}

func TestDeleteCurrentUnmergedBranchOffersForceAfterSwitching(t *testing.T) {
	isolateGitConfig(t)
	r, _ := sharedRemote(t)
	if _, err := r.Action("switch", "-c", "feat"); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "feat.txt", "feat")
	snapshot, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{screen: "branches", repo: r, width: 100, height: 30, snapshot: snapshot}
	updated, _ := m.confirmDeleteBranch(git.BranchEntry{Name: "feat", Current: true}, false)
	m, cmd := press(t, updated.(model), "y")
	m, _ = run(t, m, cmd)
	if m.modal != "branch-delete" || !m.branch.confirmForce || m.branch.leaveTo != "" {
		t.Fatalf("force offer: modal=%q force=%v leaveTo=%q", m.modal, m.branch.confirmForce, m.branch.leaveTo)
	}
	if view := plain(m.View()); !strings.Contains(view, "1 commit on feat is not in main.") {
		t.Fatalf("dialog: %q", view)
	}
	m, cmd = press(t, m, "y")
	if _, msg := run(t, m, cmd); msg.(branchActionDone).err != nil {
		t.Fatalf("forced delete: %v", msg.(branchActionDone).err)
	}
	if out, _ := r.Command("rev-parse", "--verify", "--quiet", "refs/heads/feat"); len(out) != 0 {
		t.Fatal("feat still exists")
	}
}

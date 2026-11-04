package git

import (
	"fmt"
	"strconv"
	"strings"
)

// PullIssueKind names a pull failure pane knows how to resolve.
type PullIssueKind int

const (
	// PullGone means the upstream was deleted on the remote, usually after its
	// pull request was merged.
	PullGone PullIssueKind = iota + 1
	// PullUntracked means the branch has no upstream but the remote has a
	// branch of the same name.
	PullUntracked
	// PullDiverged means local and upstream both have commits and Git has no
	// configured way to reconcile them.
	PullDiverged
)

// PullIssue describes a failed pull and what pane can offer instead.
type PullIssue struct {
	Kind     PullIssueKind
	Branch   string
	Upstream string
	Remote   string
	// Target is the default branch offered when the upstream is gone.
	Target string
	// Merged reports that the branch tip is already in the default branch.
	// Squash and rebase merges leave it false, so pane cannot tell those apart
	// from a branch deleted without merging.
	Merged bool
	// Ahead and Behind count the commits on each side of a diverged pull.
	Ahead, Behind int
}

// Pull runs git pull with args. When the failure is one pane can resolve, it
// also returns the issue.
func (r Repo) Pull(args ...string) (string, *PullIssue, error) {
	out, err := r.Action(append([]string{"pull"}, args...)...)
	if err == nil {
		return out, nil, nil
	}
	if issue, ok := r.pullIssue(err.Error()); ok {
		return "", &issue, err
	}
	return "", nil, err
}

func (r Repo) pullIssue(pullError string) (PullIssue, bool) {
	var issue PullIssue
	branch, err := r.CommandStdout("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return issue, false
	}
	issue.Branch = strings.TrimSpace(string(branch))
	switch {
	case strings.Contains(pullError, "There is no tracking information"):
		return r.untrackedIssue(issue)
	case strings.Contains(pullError, "reconcile divergent branches") || strings.Contains(pullError, "Not possible to fast-forward"):
		return r.divergedIssue(issue)
	}
	return r.goneIssue(issue, pullError)
}

func (r Repo) goneIssue(issue PullIssue, pullError string) (PullIssue, bool) {
	issue.Remote = r.config("branch." + issue.Branch + ".remote")
	merge := r.config("branch." + issue.Branch + ".merge")
	if issue.Remote == "" || issue.Remote == "." || merge == "" {
		return issue, false
	}
	track, _ := r.CommandStdout("for-each-ref", "--format=%(upstream:track)", "refs/heads/"+issue.Branch)
	if !strings.Contains(pullError, "no such ref was fetched") && strings.TrimSpace(string(track)) != "[gone]" {
		return issue, false
	}
	issue.Kind = PullGone
	issue.Upstream = issue.Remote + "/" + strings.TrimPrefix(merge, "refs/heads/")
	issue.Target = r.defaultBranch(issue.Remote)
	if issue.Target == "" || issue.Target == issue.Branch {
		return issue, false
	}
	base := issue.Remote + "/" + issue.Target
	if !r.refExists("refs/remotes/" + base) {
		base = issue.Target
	}
	_, err := r.CommandStdout("merge-base", "--is-ancestor", issue.Branch, base)
	issue.Merged = err == nil
	return issue, true
}

// untrackedIssue only applies when the remote has a branch of the same name;
// otherwise there is nothing to pull and Git's error stands.
func (r Repo) untrackedIssue(issue PullIssue) (PullIssue, bool) {
	remotes, err := r.CommandStdout("remote")
	if err != nil {
		return issue, false
	}
	issue.Remote = pushRemote(r, issue.Branch, strings.Fields(string(remotes)))
	if issue.Remote == "" {
		return issue, false
	}
	if !r.refExists("refs/remotes/" + issue.Remote + "/" + issue.Branch) {
		if _, err := r.CommandStdout("ls-remote", "--exit-code", "--heads", issue.Remote, "refs/heads/"+issue.Branch); err != nil {
			return issue, false
		}
	}
	issue.Kind = PullUntracked
	issue.Upstream = issue.Remote + "/" + issue.Branch
	return issue, true
}

func (r Repo) divergedIssue(issue PullIssue) (PullIssue, bool) {
	upstream, err := r.CommandStdout("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return issue, false
	}
	issue.Kind = PullDiverged
	issue.Upstream = strings.TrimSpace(string(upstream))
	if out, err := r.CommandStdout("rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
		if counts := strings.Fields(string(out)); len(counts) == 2 {
			issue.Ahead, _ = strconv.Atoi(counts[0])
			issue.Behind, _ = strconv.Atoi(counts[1])
		}
	}
	return issue, true
}

// defaultBranch prefers the remote's HEAD, then a main or master branch.
func (r Repo) defaultBranch(remote string) string {
	if out, err := r.CommandStdout("symbolic-ref", "--short", "-q", "refs/remotes/"+remote+"/HEAD"); err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(string(out)), remote+"/"); name != "" {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if r.refExists("refs/heads/"+name) || r.refExists("refs/remotes/"+remote+"/"+name) {
			return name
		}
	}
	return ""
}

// SwitchToDefault leaves a branch whose upstream is gone for the default
// branch, ready to pull.
func (r Repo) SwitchToDefault(issue PullIssue) error {
	return r.switchTo(issue.Target, issue.Remote)
}

// TrackUpstream makes the same-named remote branch the current branch's
// upstream, fetching it first when it has never been fetched.
func (r Repo) TrackUpstream(issue PullIssue) error {
	if !r.refExists("refs/remotes/" + issue.Upstream) {
		if _, err := r.Action("fetch", issue.Remote, issue.Branch); err != nil {
			return err
		}
	}
	_, err := r.Action("branch", "--set-upstream-to="+issue.Upstream, issue.Branch)
	return err
}

// SetPullRebase changes the user's global Git configuration. Pane only calls
// it after an explicit choice in the diverged pull dialog.
func (r Repo) SetPullRebase(rebase bool) error {
	_, err := r.CommandStdout("config", "--global", "pull.rebase", strconv.FormatBool(rebase))
	return err
}

// switchTo checks out a local branch, creating it to track remote when only
// the remote-tracking branch exists.
func (r Repo) switchTo(target, remote string) error {
	args := []string{"switch", target}
	if !r.refExists("refs/heads/"+target) && remote != "" {
		args = []string{"switch", "--track", remote + "/" + target}
	}
	_, err := r.Action(args...)
	return err
}

// branchRemote is the remote a branch tracks, or origin when it tracks none.
func (r Repo) branchRemote(branch string) string {
	if remote := r.config("branch." + branch + ".remote"); remote != "" && remote != "." {
		return remote
	}
	return "origin"
}

// DefaultBranchFor names the branch pane leaves for when deleting branch
// while it is checked out. It is empty when there is nowhere to go.
func (r Repo) DefaultBranchFor(branch string) string {
	target := r.defaultBranch(r.branchRemote(branch))
	if target == branch {
		return ""
	}
	return target
}

// LeaveAndDeleteBranch switches from the checked-out branch to target, then
// deletes branch. Staged or unstaged changes to tracked files stop it before
// anything changes, so they are never carried onto target.
func (r Repo) LeaveAndDeleteBranch(branch, target string, force bool) (string, error) {
	out, err := r.CommandStdout("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return "", fmt.Errorf("commit or stash changes before deleting the checked out branch")
	}
	if err := r.switchTo(target, r.branchRemote(branch)); err != nil {
		return "", err
	}
	if _, err := r.DeleteBranch(branch, force); err != nil {
		return "", fmt.Errorf("switched to %s, but %w", target, err)
	}
	return "Switched to " + target + " and deleted " + branch, nil
}

// DeleteBranch runs git branch -d, or -D when forced.
func (r Repo) DeleteBranch(branch string, force bool) (string, error) {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return r.Action("branch", flag, branch)
}

// UnmergedDelete reports whether err is git branch -d refusing an unmerged
// branch, with how many of its commits are missing from the checked-out
// branch, which it also names.
func (r Repo) UnmergedDelete(branch string, err error) (int, string, bool) {
	if err == nil || !strings.Contains(err.Error(), "is not fully merged") {
		return 0, "", false
	}
	head := "HEAD"
	if out, err := r.CommandStdout("symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		head = strings.TrimSpace(string(out))
	}
	out, countErr := r.CommandStdout("rev-list", "--count", "HEAD.."+branch)
	if countErr != nil {
		return 0, head, true
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count, head, true
}

func (r Repo) config(key string) string {
	out, err := r.CommandStdout("config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (r Repo) refExists(ref string) bool {
	_, err := r.CommandStdout("rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

package git

import (
	"fmt"
	"strings"
)

// GoneUpstream describes a branch whose upstream was deleted on the remote and
// the default branch pane offers to switch to instead.
type GoneUpstream struct {
	Branch   string
	Upstream string
	Remote   string
	Target   string
	// Merged reports that the branch tip is already in the default branch.
	// Squash and rebase merges leave it false, so pane cannot tell those apart
	// from a branch deleted without merging.
	Merged bool
}

// Pull runs git pull. When the pull fails because the upstream branch no
// longer exists, it also reports the default branch to switch to.
func (r Repo) Pull() (string, *GoneUpstream, error) {
	out, err := r.Action("pull")
	if err == nil {
		return out, nil, nil
	}
	if gone, ok := r.goneUpstream(err.Error()); ok {
		return "", &gone, err
	}
	return "", nil, err
}

func (r Repo) goneUpstream(pullError string) (GoneUpstream, bool) {
	var gone GoneUpstream
	branch, err := r.CommandStdout("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return gone, false
	}
	gone.Branch = strings.TrimSpace(string(branch))
	gone.Remote = r.config("branch." + gone.Branch + ".remote")
	merge := r.config("branch." + gone.Branch + ".merge")
	if gone.Remote == "" || gone.Remote == "." || merge == "" {
		return gone, false
	}
	track, _ := r.CommandStdout("for-each-ref", "--format=%(upstream:track)", "refs/heads/"+gone.Branch)
	if !strings.Contains(pullError, "no such ref was fetched") && strings.TrimSpace(string(track)) != "[gone]" {
		return gone, false
	}
	gone.Upstream = gone.Remote + "/" + strings.TrimPrefix(merge, "refs/heads/")
	gone.Target = r.defaultBranch(gone.Remote)
	if gone.Target == "" || gone.Target == gone.Branch {
		return gone, false
	}
	base := gone.Remote + "/" + gone.Target
	if !r.refExists("refs/remotes/" + base) {
		base = gone.Target
	}
	_, err = r.CommandStdout("merge-base", "--is-ancestor", gone.Branch, base)
	gone.Merged = err == nil
	return gone, true
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

// SwitchAndPull leaves a branch whose upstream is gone for the default branch
// and brings it up to date.
func (r Repo) SwitchAndPull(gone GoneUpstream) (string, error) {
	if err := r.switchTo(gone.Target, gone.Remote); err != nil {
		return "", err
	}
	if _, err := r.Action("pull"); err != nil {
		return "", err
	}
	return "Switched to " + gone.Target + " and pulled", nil
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
	flag := "-d"
	if force {
		flag = "-D"
	}
	if _, err := r.Action("branch", flag, branch); err != nil {
		return "", fmt.Errorf("switched to %s, but %w", target, err)
	}
	return "Switched to " + target + " and deleted " + branch, nil
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

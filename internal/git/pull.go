package git

import (
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
	args := []string{"switch", gone.Target}
	if !r.refExists("refs/heads/" + gone.Target) {
		args = []string{"switch", "--track", gone.Remote + "/" + gone.Target}
	}
	if _, err := r.Action(args...); err != nil {
		return "", err
	}
	if _, err := r.Action("pull"); err != nil {
		return "", err
	}
	return "Switched to " + gone.Target + " and pulled", nil
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

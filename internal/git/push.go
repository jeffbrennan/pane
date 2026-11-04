package git

import (
	"slices"
	"strings"
)

// PushProbe describes what a plain push would do right now. An empty Branch
// means HEAD is detached, which pane leaves to Git's own error.
type PushProbe struct {
	Branch   string
	Upstream string
	Remote   string
	Remotes  []string
}

// ProbePush resolves the current branch, its upstream, and the remote a new
// upstream should track.
func (r Repo) ProbePush() (PushProbe, error) {
	var probe PushProbe
	branch, err := r.CommandStdout("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return probe, nil
	}
	probe.Branch = strings.TrimSpace(string(branch))
	if upstream, err := r.CommandStdout("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		probe.Upstream = strings.TrimSpace(string(upstream))
		return probe, nil
	}
	remotes, err := r.CommandStdout("remote")
	if err != nil {
		return probe, err
	}
	probe.Remotes = strings.Fields(string(remotes))
	probe.Remote = pushRemote(r, probe.Branch, probe.Remotes)
	return probe, nil
}

// pushRemote follows Git's own order: a branch push remote, then the
// repository default, then origin, then the only remote. Several remotes with
// no configured choice leave the remote empty so pane can ask.
func pushRemote(r Repo, branch string, remotes []string) string {
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault"} {
		if out, err := r.CommandStdout("config", "--get", key); err == nil {
			if remote := strings.TrimSpace(string(out)); remote != "" {
				return remote
			}
		}
	}
	if slices.Contains(remotes, "origin") {
		return "origin"
	}
	if len(remotes) == 1 {
		return remotes[0]
	}
	return ""
}

// SetPushAutoSetupRemote changes the user's global Git configuration. Pane
// only calls it after an explicit choice in the push dialog.
func (r Repo) SetPushAutoSetupRemote() error {
	if _, err := r.CommandStdout("config", "--global", "push.autoSetupRemote", "true"); err != nil {
		return err
	}
	return nil
}

// PushIssueKind names a rejected push pane knows how to resolve.
type PushIssueKind int

const (
	// PushRewritten means the remote tip is a commit this branch once had, so
	// the rejection comes from rewriting local history, as an amend does.
	PushRewritten PushIssueKind = iota + 1
	// PushBehind means the remote has commits this branch never had, usually
	// pushed from somewhere else.
	PushBehind
)

// PushIssue describes a rejected push.
type PushIssue struct {
	Kind         PushIssueKind
	Branch       string
	Remote       string
	RemoteBranch string
	// RemoteTip is the commit a forced push expects to replace; the lease
	// fails if the remote has moved past it since.
	RemoteTip string
	// Replaced lists, newest first, the remote commits a forced push removes.
	Replaced []string
}

// Upstream names the remote branch the way Git prints it.
func (p PushIssue) Upstream() string {
	return p.Remote + "/" + p.RemoteBranch
}

// Push runs git push with args. A push rejected because the remote moved is
// classified so pane can offer a forced push or a pull.
func (r Repo) Push(args ...string) (string, *PushIssue, error) {
	out, err := r.Action(append([]string{"push"}, args...)...)
	if err == nil {
		return out, nil, nil
	}
	text := err.Error()
	if !strings.Contains(text, "[rejected]") || (!strings.Contains(text, "(non-fast-forward)") && !strings.Contains(text, "(fetch first)")) {
		return "", nil, err
	}
	if issue, ok := r.pushIssue(); ok {
		return "", &issue, err
	}
	return "", nil, err
}

func (r Repo) pushIssue() (PushIssue, bool) {
	var issue PushIssue
	branch, err := r.CommandStdout("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return issue, false
	}
	issue.Branch = strings.TrimSpace(string(branch))
	issue.Remote = r.config("branch." + issue.Branch + ".remote")
	issue.RemoteBranch = strings.TrimPrefix(r.config("branch."+issue.Branch+".merge"), "refs/heads/")
	if issue.Remote == "" || issue.Remote == "." || issue.RemoteBranch == "" {
		return issue, false
	}
	// A "fetch first" rejection means the remote tip is not here yet.
	if _, err := r.Action("fetch", issue.Remote, issue.RemoteBranch); err != nil {
		return issue, false
	}
	tip, err := r.CommandStdout("rev-parse", "--verify", "--quiet", "refs/remotes/"+issue.Upstream()+"^{commit}")
	if err != nil {
		return issue, false
	}
	issue.RemoteTip = strings.TrimSpace(string(tip))
	if out, err := r.CommandStdout("log", "--format=%h %s", "HEAD.."+issue.RemoteTip); err == nil && len(strings.TrimSpace(string(out))) > 0 {
		issue.Replaced = strings.Split(strings.TrimSpace(string(out)), "\n")
	}
	issue.Kind = PushBehind
	if r.hadCommit(issue.Branch, issue.RemoteTip) {
		issue.Kind = PushRewritten
	}
	return issue, true
}

// hadCommit reports whether commit was ever part of branch according to its
// reflog: rev-list prints nothing when commit is reachable from an old tip.
func (r Repo) hadCommit(branch, commit string) bool {
	tips, err := r.CommandStdout("log", "--walk-reflogs", "--format=%H", "-n", "200", "refs/heads/"+branch)
	if err != nil || len(strings.TrimSpace(string(tips))) == 0 {
		return false
	}
	args := []string{"rev-list", "-n", "1", commit, "--not"}
	args = append(args, strings.Fields(string(tips))...)
	out, err := r.CommandStdout(args...)
	return err == nil && len(strings.TrimSpace(string(out))) == 0
}

// ForcePush replaces the remote branch with the local one, but only while the
// remote still points at the tip the user was shown.
func (r Repo) ForcePush(issue PushIssue) (string, *PushIssue, error) {
	lease := "--force-with-lease=refs/heads/" + issue.RemoteBranch + ":" + issue.RemoteTip
	return r.Push(lease, issue.Remote, "refs/heads/"+issue.Branch+":refs/heads/"+issue.RemoteBranch)
}

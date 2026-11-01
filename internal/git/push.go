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

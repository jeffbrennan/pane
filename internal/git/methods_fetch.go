package git

import (
	"os/exec"
	"strings"
)

func (r Repo) Fetch() (string, error) {
	out, err := r.Command("remote")
	if err != nil {
		return "", GitError(out, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return "No remotes configured", nil
	}
	branch, err := r.Command("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", GitError(branch, err)
		}
	}
	remote := ""
	if err == nil {
		prefix := "branch." + strings.TrimSpace(string(branch)) + "."
		merge, mergeErr := r.Command("config", "--get", prefix+"merge")
		if mergeErr != nil {
			if exit, ok := mergeErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return "", GitError(merge, mergeErr)
			}
		} else if strings.TrimSpace(string(merge)) != "" {
			configured, configErr := r.Command("config", "--get", prefix+"remote")
			if configErr != nil {
				if exit, ok := configErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
					return "", GitError(configured, configErr)
				}
			} else {
				remote = strings.TrimSpace(string(configured))
			}
		}
	}
	args := []string{"fetch", "--all", "--prune"}
	if remote != "" && remote != "." {
		args = []string{"fetch", "--prune", "--", remote}
	}
	if _, err := r.Action(args...); err != nil {
		return "", err
	}
	return "Fetch complete", nil
}

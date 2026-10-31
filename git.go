package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type change struct {
	path     string
	diff     string
	conflict bool
}

type snapshot struct {
	unstaged []change
	staged   []change
	branch   branchStatus
}

type repository struct{ root string }

func openRepository() (repository, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return repository{}, fmt.Errorf("not inside a Git working tree")
	}
	return repository{root: strings.TrimSuffix(string(out), "\n")}, nil
}

func (r repository) command(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1", "GIT_PAGER=cat", "GIT_EDITOR=true")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return cmd.CombinedOutput()
}

func (r repository) load() (snapshot, error) {
	out, err := r.command("status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return snapshot{}, gitError(out, err)
	}
	var result snapshot
	result.branch, err = r.branchStatus()
	if err != nil {
		return snapshot{}, err
	}
	for _, entry := range bytes.Split(out, []byte{0}) {
		if len(entry) < 4 {
			continue
		}
		status, path := string(entry[:2]), string(entry[3:])
		switch status {
		case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
			c := change{path: path, diff: "Merge conflict: resolve outside pane.", conflict: true}
			result.unstaged = append(result.unstaged, c)
			continue
		}
		if status == "??" {
			diff, err := r.untrackedDiff(path)
			if err != nil {
				return snapshot{}, err
			}
			result.unstaged = append(result.unstaged, change{path: path, diff: diff})
			continue
		}
		if status[0] != ' ' {
			diff, err := r.diff(path, true)
			if err != nil {
				return snapshot{}, err
			}
			result.staged = append(result.staged, change{path: path, diff: diff})
		}
		if status[1] != ' ' {
			diff, err := r.diff(path, false)
			if err != nil {
				return snapshot{}, err
			}
			result.unstaged = append(result.unstaged, change{path: path, diff: diff})
		}
	}
	return result, nil
}

func (r repository) diff(path string, staged bool) (string, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := r.command(args...)
	if err != nil {
		return "", gitError(out, err)
	}
	if len(out) == 0 {
		return "No diff available (possibly a conflict or submodule change).", nil
	}
	return string(out), nil
}

func (r repository) untrackedDiff(path string) (string, error) {
	out, err := r.command("diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", os.DevNull, path)
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", gitError(out, err)
		}
	}
	return string(out), nil
}

func (r repository) stage(path string, staged bool) error {
	var out []byte
	var err error
	if !staged {
		out, err = r.command("add", "-A", "--", path)
	} else if _, headErr := r.command("rev-parse", "--verify", "HEAD"); headErr != nil {
		out, err = r.command("rm", "--cached", "-r", "--ignore-unmatch", "--", path)
	} else {
		out, err = r.command("restore", "--staged", "--", path)
	}
	if err != nil {
		return gitError(out, err)
	}
	return nil
}

func (r repository) action(args ...string) (string, error) {
	out, err := r.command(args...)
	if err != nil {
		return "", gitError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r repository) commit(message string) (string, error) {
	out, err := r.command("diff", "--cached", "--quiet", "--exit-code")
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 {
			return "", gitError(out, err)
		}
	} else {
		conflicts, conflictErr := r.command("diff", "--name-only", "--diff-filter=U", "-z")
		if conflictErr != nil {
			return "", gitError(conflicts, conflictErr)
		}
		if len(conflicts) > 0 {
			return "", fmt.Errorf("resolve merge conflicts outside pane before committing")
		}
		out, err = r.command("add", "--all")
		if err != nil {
			return "", gitError(out, err)
		}
	}
	return r.action("commit", "-m", message)
}

func gitError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}

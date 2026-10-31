package main

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (r repository) fetch() (string, error) {
	out, err := r.command("remote")
	if err != nil {
		return "", gitError(out, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return "No remotes configured", nil
	}
	branch, err := r.command("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", gitError(branch, err)
		}
	}
	remote := ""
	if err == nil {
		prefix := "branch." + strings.TrimSpace(string(branch)) + "."
		merge, mergeErr := r.command("config", "--get", prefix+"merge")
		if mergeErr != nil {
			if exit, ok := mergeErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return "", gitError(merge, mergeErr)
			}
		} else if strings.TrimSpace(string(merge)) != "" {
			configured, configErr := r.command("config", "--get", prefix+"remote")
			if configErr != nil {
				if exit, ok := configErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
					return "", gitError(configured, configErr)
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
	if _, err := r.action(args...); err != nil {
		return "", err
	}
	return "Fetch complete", nil
}

func (m model) startFetch() (tea.Model, tea.Cmd) {
	m.busy = true
	m.activeAction = "fetch"
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		message, err := repo.fetch()
		return completed{action: "fetch", message: message, err: err}
	}
}

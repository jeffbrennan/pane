package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func (r repository) amendMessage(hash, message string) (string, error) {
	out, err := r.command("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", gitError(out, err)
	}
	if strings.TrimSpace(string(out)) != hash {
		return "", fmt.Errorf("HEAD changed; refresh history and try again")
	}
	out, err = r.command("remote")
	if err != nil {
		return "", gitError(out, err)
	}
	for _, remote := range strings.Fields(string(out)) {
		urls := []string{remote}
		pushURLs, urlErr := r.command("remote", "get-url", "--push", "--all", remote)
		if urlErr != nil {
			return "", fmt.Errorf("cannot verify push URLs for remote %q: %w", remote, gitError(pushURLs, urlErr))
		}
		urls = append(urls, strings.Split(strings.TrimSuffix(string(pushURLs), "\n"), "\n")...)
		for _, url := range urls {
			if err := r.verifyUnpublished(hash, remote, url); err != nil {
				return "", err
			}
		}
	}
	out, err = r.command("rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(out)) != hash {
		return "", fmt.Errorf("HEAD changed; refresh history and try again")
	}
	out, err = r.commandInput(strings.NewReader(message), "commit", "--amend", "--only", "--file=-")
	if err != nil {
		return "", gitError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r repository) verifyUnpublished(hash, remote, url string) error {
	refs, err := r.command("ls-remote", "--refs", "--heads", "--tags", url)
	if err != nil {
		return fmt.Errorf("cannot verify remote %q: %w", remote, gitError(refs, err))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Dereference tags and require their history to be available locally.
		target, resolveErr := r.command("rev-parse", "--verify", fields[0]+"^{commit}")
		if resolveErr != nil {
			return fmt.Errorf("cannot verify remote %q; fetch its branches and tags first", remote)
		}
		out, ancestorErr := r.command("merge-base", "--is-ancestor", hash, strings.TrimSpace(string(target)))
		if ancestorErr == nil {
			return fmt.Errorf("commit is already reachable from remote %q", remote)
		}
		if exit, ok := ancestorErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return fmt.Errorf("cannot verify remote %q: %w", remote, gitError(out, ancestorErr))
		}
	}
	return nil
}

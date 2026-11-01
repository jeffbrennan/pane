package git

import (
	"fmt"
	"os/exec"
	"strings"
)

func (r Repo) AmendMessage(hash, message string) (string, error) {
	out, err := r.Command("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", GitError(out, err)
	}
	if strings.TrimSpace(string(out)) != hash {
		return "", fmt.Errorf("HEAD changed; refresh history and try again")
	}
	out, err = r.Command("remote")
	if err != nil {
		return "", GitError(out, err)
	}
	for _, remote := range strings.Fields(string(out)) {
		urls := []string{remote}
		pushURLs, urlErr := r.Command("remote", "get-url", "--push", "--all", remote)
		if urlErr != nil {
			return "", fmt.Errorf("cannot verify push URLs for remote %q: %w", remote, GitError(pushURLs, urlErr))
		}
		urls = append(urls, strings.Split(strings.TrimSuffix(string(pushURLs), "\n"), "\n")...)
		for _, url := range urls {
			if err := r.VerifyUnpublished(hash, remote, url); err != nil {
				return "", err
			}
		}
	}
	out, err = r.Command("rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(out)) != hash {
		return "", fmt.Errorf("HEAD changed; refresh history and try again")
	}
	out, err = r.CommandInput(strings.NewReader(message), "commit", "--amend", "--only", "--file=-")
	if err != nil {
		return "", GitError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r Repo) VerifyUnpublished(hash, remote, url string) error {
	refs, err := r.Command("ls-remote", "--refs", "--heads", "--tags", url)
	if err != nil {
		return fmt.Errorf("cannot verify remote %q: %w", remote, GitError(refs, err))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Dereference tags and require their history to be available locally.
		target, resolveErr := r.Command("rev-parse", "--verify", fields[0]+"^{commit}")
		if resolveErr != nil {
			return fmt.Errorf("cannot verify remote %q; fetch its branches and tags first", remote)
		}
		out, ancestorErr := r.Command("merge-base", "--is-ancestor", hash, strings.TrimSpace(string(target)))
		if ancestorErr == nil {
			return fmt.Errorf("commit is already reachable from remote %q", remote)
		}
		if exit, ok := ancestorErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return fmt.Errorf("cannot verify remote %q: %w", remote, GitError(out, ancestorErr))
		}
	}
	return nil
}

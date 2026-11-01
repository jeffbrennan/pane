package git

import (
	"bytes"
	"fmt"
	"strings"
)

// DefaultBase picks the upstream branch, then origin's default branch, for
// `pane -b` with no revision.
func (r Repo) DefaultBase() (string, error) {
	if out, err := r.Command("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" && name != "@{upstream}" {
			return name, nil
		}
	}
	if out, err := r.Command("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("no upstream branch; pass a base revision, for example pane -b main")
}

// openBaseFrom starts a base review from a history commit to the working tree.

func (r Repo) MergeBase(revision string) (string, error) {
	commit, err := r.ResolveCommit(revision)
	if err != nil {
		return "", fmt.Errorf("invalid base %q: %w", revision, err)
	}
	out, err := r.Command("merge-base", commit.Hash, "HEAD")
	if err != nil {
		return "", fmt.Errorf("no common ancestor with %s: %w", revision, GitError(out, err))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r Repo) UntrackedChanges() ([]Change, error) {
	out, err := r.Command("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, GitError(out, err)
	}
	var untracked []string
	for _, path := range bytes.Split(out, []byte{0}) {
		if len(path) > 0 {
			untracked = append(untracked, string(path))
		}
	}
	added, err := parallelDiffs(untracked, r.untrackedDiff)
	if err != nil {
		return nil, err
	}
	for i := range added {
		added[i].Untracked = true
	}
	return added, nil
}

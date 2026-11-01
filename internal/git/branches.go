package git

import (
	"bytes"
	"strings"
)

func (r Repo) Branches(includeRemotes bool) ([]BranchEntry, error) {
	format := "--format=%(refname)%00%(refname:short)%00%(upstream:short)%00%(upstream:track)%00%(committerdate:iso-strict)%00%(subject)%0a"
	refs := []string{"refs/heads"}
	if includeRemotes {
		refs = append(refs, "refs/remotes")
	}
	args := append([]string{"for-each-ref", "--sort=-committerdate", format}, refs...)
	out, err := r.CommandStdout(args...)
	if err != nil {
		return nil, CommandError(out, err)
	}
	current := ""
	if head, err := r.Command("symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		current = strings.TrimSpace(string(head))
	}
	worktrees := r.worktreeBranches()
	var entries []BranchEntry
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := bytes.Split([]byte(line), []byte{0})
		if len(fields) < 6 {
			continue
		}
		entry := BranchEntry{
			Ref: string(fields[0]), Name: string(fields[1]), Upstream: string(fields[2]),
			Track: strings.TrimSpace(string(fields[3])), Date: string(fields[4]), Subject: string(fields[5]),
		}
		entry.Remote = strings.HasPrefix(entry.Ref, "refs/remotes/")
		if entry.Remote && strings.HasSuffix(entry.Name, "/HEAD") {
			continue
		}
		entry.Current = !entry.Remote && entry.Name == current
		if entry.Current {
			entry.Worktree = ""
		} else if path, ok := worktrees[entry.Name]; ok && !entry.Remote {
			entry.Worktree = path
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

type BranchEntry struct {
	Ref, Name, Upstream, Track, Date, Subject, Worktree string
	Current, Remote                                     bool
}

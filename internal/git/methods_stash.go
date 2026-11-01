package git

import (
	"bytes"
	"fmt"
	"strings"
)

func (r Repo) Stashes() ([]StashEntry, error) {
	out, err := r.Command("stash", "list", "-z", "--format=%gd%x00%H%x00%cI%x00%gs")
	if err != nil {
		return nil, GitError(out, err)
	}
	fields := bytes.Split(out, []byte{0})
	var entries []StashEntry
	for i := 0; i < len(fields) && len(fields[i]) > 0; i += 4 {
		if i+3 >= len(fields) {
			return nil, fmt.Errorf("unexpected Git stash format")
		}
		entries = append(entries, StashEntry{Ref: string(fields[i]), Hash: string(fields[i+1]), Date: string(fields[i+2]), Message: string(fields[i+3])})
	}
	return entries, nil
}

func (r Repo) StashChanges(hash string) ([]Change, error) {
	changes, err := r.CommitChanges(hash)
	if err != nil {
		return nil, err
	}
	parents, err := r.Command("rev-list", "--parents", "-n", "1", hash)
	if err != nil {
		return nil, GitError(parents, err)
	}
	fields := strings.Fields(string(parents))
	if len(fields) >= 4 {
		untracked, err := r.CommitChanges(fields[3])
		if err != nil {
			return nil, err
		}
		for i := range untracked {
			untracked[i].Untracked = true
		}
		changes = append(changes, untracked...)
	}
	return changes, nil
}

func (r Repo) StashPush() (string, error) {
	_, entries, err := r.status()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "Nothing to stash", nil
	}
	out, err := r.Action("stash", "push", "--include-untracked")
	if err != nil {
		return "", err
	}
	if strings.Contains(out, "No local changes to save") {
		return "Nothing to stash", nil
	}
	label := "files"
	if len(entries) == 1 {
		label = "file"
	}
	return fmt.Sprintf("Stashed %d %s", len(entries), label), nil
}

func (r Repo) StashAction(operation string, target StashEntry) (string, error) {
	entries, err := r.Stashes()
	if err != nil {
		return "", err
	}
	valid := false
	for _, entry := range entries {
		if entry.Ref == target.Ref && entry.Hash == target.Hash {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("stash list changed; reopen the picker and try again")
	}
	_, err = r.Action("stash", operation, target.Ref)
	if err != nil {
		return "", err
	}
	if operation == "pop" {
		return "Popped " + target.Ref, nil
	}
	return "Dropped " + target.Ref, nil
}

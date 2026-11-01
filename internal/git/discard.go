package git

import (
	"fmt"
	"os"
	"path/filepath"
)

func (r Repo) Discard(request DiscardRequest) error {
	current, err := r.Load()
	if err != nil {
		return err
	}
	valid := false
	for _, c := range current.Unstaged {
		if c.Path == request.Change.Path && c.Diff == request.Change.Diff && c.Untracked == request.Change.Untracked && c.UntrackedHashValid == request.Change.UntrackedHashValid && c.UntrackedHash == request.Change.UntrackedHash && !c.Conflict {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("file changed since confirmation; refresh and try again")
	}
	if request.Change.Untracked {
		return os.Remove(filepath.Join(r.Root, request.Change.Path))
	}
	if request.Whole {
		out, err := r.Command("restore", "--worktree", "--", request.Change.Path)
		if err != nil {
			return CommandError(out, err)
		}
		return nil
	}
	return r.applyPatch(request.Patch, "--reverse")
}

type DiscardRequest struct {
	Change Change
	Patch  string
	Whole  bool
}

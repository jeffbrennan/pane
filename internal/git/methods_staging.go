package git

import (
	"fmt"
	"strings"

	"github.com/jeffbrennan/pane/internal/syntax"
)

func (r Repo) StageHunk(c Change, patch string, staged bool) error {
	current, err := r.diffsForChanges([]Change{c}, staged)
	if err != nil {
		return err
	}
	if len(current) != 1 || current[0].Diff != c.Diff {
		return fmt.Errorf("file changed; refresh and try again")
	}
	if c.OldPath != "" {
		patch, err = renameContentPatch(patch)
		if err != nil {
			return err
		}
	}
	args := []string{"--cached"}
	if staged {
		args = append(args, "--reverse")
	}
	if err := r.applyPatch(patch, args...); err != nil {
		return fmt.Errorf("could not apply hunk; refresh and try again: %w", err)
	}
	return nil
}

func renameContentPatch(patch string) (string, error) {
	var destination string
	lines := strings.SplitAfter(patch, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "+++ ") {
			destination = strings.TrimSuffix(strings.TrimPrefix(line, "+++ "), "\n")
		}
		if syntax.MatchHunkHeader(line) != nil && destination != "" {
			old := destination
			if strings.HasPrefix(old, `"b/`) {
				old = `"a/` + old[3:]
			} else if strings.HasPrefix(old, "b/") {
				old = "a/" + old[2:]
			} else {
				break
			}
			return "--- " + old + "\n+++ " + destination + "\n" + strings.Join(lines[i:], ""), nil
		}
	}
	return "", fmt.Errorf("could not prepare renamed hunk; refresh and try again")
}

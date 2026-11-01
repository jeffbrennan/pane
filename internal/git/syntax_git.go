package git

import (
	"strings"

	"github.com/jeffbrennan/pane/internal/syntax"
)

// The Git layer loads and warms syntax for a set of changes. syntax.go itself
// works on plain paths and diffs, so it stays independent of repository and
// change.

func (r Repo) LoadSyntaxDiffs(changes []Change, key Context) {
	for i, diff := range r.SyntaxDiffs(changes, key) {
		changes[i].SyntaxDiff = diff
	}
	markSyntaxSkipped(changes, syntaxBudgetEnd(changes))
}

// syntaxBudgetEnd returns the index of the first of changes past the syntax
// budget, or len(changes) if all fit.
func syntaxBudgetEnd(changes []Change) int {
	files, lines := 0, 0
	for i, c := range changes {
		if !highlightable(c) {
			continue
		}
		files++
		lines += strings.Count(c.Diff, "\n")
		if files > SyntaxBudgetFiles || lines > SyntaxBudgetLines {
			return i
		}
	}
	return len(changes)
}

// markSyntaxSkipped flags the highlightable changes from end on.
func markSyntaxSkipped(changes []Change, end int) {
	for i := end; i < len(changes); i++ {
		changes[i].SyntaxSkipped = highlightable(changes[i])
	}
}

func highlightable(c Change) bool {
	return syntax.Lexable(c.Path) && !c.Conflict
}

// Highlighting stops after this many files or diff lines in one call. Change
// sets within it highlight all at once; past it, files highlight as they near
// the viewport (see syntaxDemand), so a huge one neither spawns Git once per
// file nor tokenizes for seconds.
const (
	SyntaxBudgetFiles = 20
	SyntaxBudgetLines = 5000
)

// SyntaxDiffs loads the full-context diffs that highlighting tokenizes. It
// only needs paths, so callers can run it alongside the display diffs. Files
// past the syntax budget are left plain; see syntaxBudgetEnd.
func (r Repo) SyntaxDiffs(changes []Change, key Context) []string {
	diffs := make([]string, len(changes))
	var indexes []int
	var selected []Change
	for i := range changes[:syntaxBudgetEnd(changes)] {
		if highlightable(changes[i]) {
			indexes = append(indexes, i)
			selected = append(selected, changes[i])
		}
	}
	for i, result := range r.FullContexts(selected, key) {
		if result.Err == nil && len(result.Diff) <= 2*1024*1024 {
			diffs[indexes[i]] = result.Diff
		}
	}
	return diffs
}

func WarmSyntax(changes []Change) {
	files := make([]syntax.File, 0, len(changes))
	for _, c := range changes {
		if c.SyntaxDiff != "" {
			files = append(files, syntax.File{Path: c.Path, Diff: c.SyntaxDiff, Display: c.Diff})
		}
	}
	syntax.Warm(files)
}

// Context identifies the view a diff load belongs to.
type Context struct {
	Path, Commit string
	Staged, Base bool
}

func (r Repo) FullContext(key Context, base, oldPath string) (string, error) {
	if strings.Contains(base, "\nnew file mode ") {
		return base, nil
	}
	paths := []string{key.Path}
	if oldPath != "" {
		paths = append(paths, oldPath)
	}
	out, err := r.Command(r.fullContextArgs(key, paths)...)
	if err != nil {
		return "", CommandError(out, err)
	}
	return string(out), nil
}

func (r Repo) fullContextArgs(key Context, paths []string) []string {
	args := append([]string{"diff"}, r.diffOptions()...)
	args = append(args, "--unified=1000000")
	if key.Base {
		args = append(args, key.Commit)
	} else if key.Commit != "" {
		args = append([]string{"show", "--first-parent", "--format="}, r.diffOptions()...)
		args = append(args, "--unified=1000000", key.Commit)
	} else if key.Staged {
		args = append(args, "--cached")
	}
	return append(append(args, "--"), paths...)
}

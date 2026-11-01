package main

import (
	"strings"

	"github.com/jeffbrennan/pane/internal/syntax"
)

// The Git layer loads and warms syntax for a set of changes. syntax.go itself
// works on plain paths and diffs, so it stays independent of repository and
// change.

func (r repository) loadSyntaxDiffs(changes []change, key contextKey) {
	for i, diff := range r.syntaxDiffs(changes, key) {
		changes[i].syntaxDiff = diff
	}
	markSyntaxSkipped(changes, syntaxBudgetEnd(changes))
}

// syntaxBudgetEnd returns the index of the first of changes past the syntax
// budget, or len(changes) if all fit.
func syntaxBudgetEnd(changes []change) int {
	files, lines := 0, 0
	for i, c := range changes {
		if !highlightable(c) {
			continue
		}
		files++
		lines += strings.Count(c.diff, "\n")
		if files > syntaxBudgetFiles || lines > syntaxBudgetLines {
			return i
		}
	}
	return len(changes)
}

// markSyntaxSkipped flags the highlightable changes from end on.
func markSyntaxSkipped(changes []change, end int) {
	for i := end; i < len(changes); i++ {
		changes[i].syntaxSkipped = highlightable(changes[i])
	}
}

func highlightable(c change) bool {
	return syntax.Lexable(c.path) && !c.conflict
}

// Highlighting stops after this many files or diff lines in one call. Change
// sets within it highlight all at once; past it, files highlight as they near
// the viewport (see syntaxDemand), so a huge one neither spawns Git once per
// file nor tokenizes for seconds.
const (
	syntaxBudgetFiles = 20
	syntaxBudgetLines = 5000
)

// syntaxDiffs loads the full-context diffs that highlighting tokenizes. It
// only needs paths, so callers can run it alongside the display diffs. Files
// past the syntax budget are left plain; see syntaxBudgetEnd.
func (r repository) syntaxDiffs(changes []change, key contextKey) []string {
	diffs := make([]string, len(changes))
	var indexes []int
	var selected []change
	for i := range changes[:syntaxBudgetEnd(changes)] {
		if highlightable(changes[i]) {
			indexes = append(indexes, i)
			selected = append(selected, changes[i])
		}
	}
	for i, result := range r.fullContexts(selected, key) {
		if result.err == nil && len(result.diff) <= 2*1024*1024 {
			diffs[indexes[i]] = result.diff
		}
	}
	return diffs
}

func warmSyntax(changes []change) {
	files := make([]syntax.File, 0, len(changes))
	for _, c := range changes {
		if c.syntaxDiff != "" {
			files = append(files, syntax.File{Path: c.path, Diff: c.syntaxDiff, Display: c.diff})
		}
	}
	syntax.Warm(files)
}

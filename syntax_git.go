package main

import (
	"runtime"
	"strings"
	"sync"
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
	return syntaxLexer(c.path) != nil && !c.conflict
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

// syntaxFile is one file's tokenizing work: the full-context diff and the
// display diff that says which lines are on screen.
type syntaxFile struct {
	path, diff, display string
}

// warmSyntax tokenizes the displayed lines of changes ahead of time, off the
// UI goroutine, one file per core. The file under the cursor gets its whole
// content tokenized later; that covers context expansion and runs whose
// lead-in missed an opening comment, without tokenizing every file.
func warmSyntax(changes []change) {
	files := make([]syntaxFile, 0, len(changes))
	for _, c := range changes {
		if c.syntaxDiff != "" {
			files = append(files, syntaxFile{path: c.path, diff: c.syntaxDiff, display: c.diff})
		}
	}
	warmSyntaxFiles(files)
}

func warmSyntaxFiles(files []syntaxFile) {
	var wg sync.WaitGroup
	limit := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, file := range files {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer func() { <-limit; wg.Done() }()
			syntaxLinesFor(file.path, file.diff, file.display)
		}()
	}
	wg.Wait()
}

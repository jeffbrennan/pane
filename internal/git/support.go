package git

import (
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Context identifies the view a diff load belongs to.
type Context struct {
	Path, Commit string
	Staged, Base bool
}

// Exported limits the UI shares with the Git layer.
const (
	HistoryPage        = historyPage
	StartupSyntaxLines = startupSyntaxLines
	SyntaxBudgetFiles  = syntaxBudgetFiles
	SyntaxBudgetLines  = syntaxBudgetLines
)

// parallelEach runs f for every index with bounded concurrency, preserving
// input order in the results the caller writes.
func parallelEach(n int, f func(int)) {
	workers := min(n, runtime.GOMAXPROCS(0))
	if workers <= 1 {
		for i := range n {
			f(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				f(i)
			}
		}()
	}
	wg.Wait()
}

var submoduleHeader = regexp.MustCompile(`^Submodule (.+) ([0-9a-f]+)\.\.\.?([0-9a-f]+)(?: \(([^)]*)\))?:?$`)

// SubmoduleLine parses a submodule summary line into its path and display
// text.
func SubmoduleLine(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "Submodule ") {
		return "", "", false
	}
	if match := submoduleHeader.FindStringSubmatch(line); match != nil {
		text := "Submodule " + match[2] + " → " + match[3]
		if match[4] != "" {
			text += " (" + match[4] + ")"
		}
		return match[1], text, true
	}
	for _, suffix := range []string{" contains modified content", " contains untracked content"} {
		if path, ok := strings.CutSuffix(strings.TrimPrefix(line, "Submodule "), suffix); ok {
			return path, "Submodule" + suffix, true
		}
	}
	return "", "", false
}

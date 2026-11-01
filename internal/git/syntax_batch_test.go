package git

import (
	"strings"
	"testing"
)

func TestSyntaxDiffsStopAtBudget(t *testing.T) {
	huge := "@@ -0,0 +1 @@\n" + strings.Repeat("+x\n", syntaxBudgetLines)
	// The first file is over the budget, so neither file asks Git for context.
	diffs := Repo{}.SyntaxDiffs([]Change{{Path: "a.go", Diff: huge}, {Path: "b.go", Diff: "@@ -0,0 +1 @@\n+x\n"}}, Context{})
	if diffs[0] != "" || diffs[1] != "" {
		t.Fatalf("highlighted past the budget: %q", diffs)
	}
}

package ui

import (
	"fmt"
	"strings"

	"github.com/jeffbrennan/pane/internal/git"
)

// collapsedPaths returns the collapsed files for the model's current context,
// so row building can stop at their headers instead of laying out every line.
func (m model) collapsedPaths(changes []git.Change) map[string]bool {
	if len(m.collapsedFiles) == 0 {
		return nil
	}
	var collapsed map[string]bool
	for _, c := range changes {
		key := git.Context{Path: c.Path, Commit: m.contextCommit, Staged: m.staged, Base: m.contextBase}
		if m.collapsedFiles[key] {
			if collapsed == nil {
				collapsed = make(map[string]bool)
			}
			collapsed[c.Path] = true
		}
	}
	return collapsed
}

func (m *model) collapseGenerated(changes []git.Change) {
	for _, c := range changes {
		if !c.Generated {
			continue
		}
		key := git.Context{Path: c.Path, Commit: m.contextCommit, Staged: m.staged, Base: m.contextBase}
		if m.generatedHandled == nil {
			m.generatedHandled = make(map[git.Context]bool)
		}
		if m.generatedHandled[key] {
			continue
		}
		m.generatedHandled[key] = true
		if m.collapsedFiles == nil {
			m.collapsedFiles = make(map[git.Context]bool)
		}
		m.collapsedFiles[key] = true
	}
}

func generatedLabel(r row) string {
	added, deleted := r.generatedCounts()
	label := "  generated"
	if added > 0 || deleted > 0 {
		label += fmt.Sprintf(" · +%d −%d", added, deleted)
	}
	return label
}

func countGeneratedLines(diff string) (int, int) {
	added, deleted := 0, 0
	inHunk := false
	for rest := diff; rest != ""; {
		line, next, _ := strings.Cut(rest, "\n")
		rest = next
		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case '+':
			added++
		case '-':
			deleted++
		}
	}
	return added, deleted
}

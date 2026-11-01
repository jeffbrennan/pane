package main

import (
	"github.com/jeffbrennan/pane/internal/git"

	"fmt"
	"strings"
)

func generatedByPath(path string) bool { return git.GeneratedByPath(path) }

func (m *model) collapseGenerated(changes []Change) {
	for _, c := range changes {
		if !c.Generated {
			continue
		}
		key := contextKey{path: c.Path, commit: m.contextCommit, staged: m.staged, base: m.contextBase}
		if m.generatedHandled == nil {
			m.generatedHandled = make(map[contextKey]bool)
		}
		if m.generatedHandled[key] {
			continue
		}
		m.generatedHandled[key] = true
		if m.collapsedFiles == nil {
			m.collapsedFiles = make(map[contextKey]bool)
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

package main

import (
	"fmt"
	"strings"
)

// Built-in generated and lock file names, for trees without .gitattributes.
var generatedNames = map[string]bool{
	"bun.lock":          true,
	"bun.lockb":         true,
	"package-lock.json": true,
	"go.sum":            true,
	"Cargo.lock":        true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"composer.lock":     true,
	"poetry.lock":       true,
	"Gemfile.lock":      true,
}

var generatedSuffixes = []string{".lock", ".snap", ".min.js", ".min.css"}

func generatedByPath(path string) bool {
	base := path[strings.LastIndex(path, "/")+1:]
	if generatedNames[base] {
		return true
	}
	for _, suffix := range generatedSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

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

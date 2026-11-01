package main

import (
	"bytes"
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

// generatedPaths marks built-in generated files and asks Git once for the
// .gitattributes linguist-generated and -diff markers on the rest.
func (r repository) generatedPaths(paths []string) map[string]bool {
	generated := make(map[string]bool)
	var candidates []string
	for _, path := range paths {
		if generatedByPath(path) {
			generated[path] = true
			continue
		}
		candidates = append(candidates, path)
	}
	if len(candidates) == 0 {
		return generated
	}
	input := strings.Join(candidates, "\x00") + "\x00"
	out, err := r.commandInput(strings.NewReader(input), "check-attr", "-z", "--stdin", "linguist-generated", "diff")
	if err != nil {
		return generated
	}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i+2 < len(fields); i += 3 {
		path, attr, value := string(fields[i]), string(fields[i+1]), string(fields[i+2])
		if path == "" {
			continue
		}
		switch attr {
		case "linguist-generated":
			if value == "set" || value == "true" || value == "yes" {
				generated[path] = true
			}
		case "diff":
			// `-diff` (unset) marks content Git should not show as a diff.
			if value == "unset" || value == "false" {
				generated[path] = true
			}
		}
	}
	return generated
}

// collapseGenerated seeds the collapsed-file map for generated files the
// session has not seen before, so a file the reader opened stays open.
func (m *model) collapseGenerated(changes []change) {
	for _, c := range changes {
		if !c.generated {
			continue
		}
		key := contextKey{path: c.path, commit: m.contextCommit, staged: m.staged, base: m.contextBase}
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

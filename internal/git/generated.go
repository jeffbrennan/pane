package git

import (
	"bytes"
	"strings"
)

// PathMarks are the .gitattributes classifications pane cares about, read in
// one check-attr process.
type PathMarks struct {
	Generated     map[string]bool
	Vendored      map[string]bool
	Documentation map[string]bool
}

func newPathMarks() PathMarks {
	return PathMarks{
		Generated:     make(map[string]bool),
		Vendored:      make(map[string]bool),
		Documentation: make(map[string]bool),
	}
}

// PathMarks marks built-in generated files and asks Git once for the
// linguist-generated, linguist-vendored, linguist-documentation and -diff
// markers on the rest.
func (r Repo) PathMarks(paths []string) PathMarks {
	marks := newPathMarks()
	var candidates []string
	for _, path := range paths {
		if GeneratedByPath(path) {
			marks.Generated[path] = true
			continue
		}
		candidates = append(candidates, path)
	}
	if len(candidates) == 0 {
		return marks
	}
	input := strings.Join(candidates, "\x00") + "\x00"
	out, err := r.commandInput(strings.NewReader(input), "check-attr", "-z", "--stdin",
		"linguist-generated", "linguist-vendored", "linguist-documentation", "diff")
	if err != nil {
		return marks
	}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i+2 < len(fields); i += 3 {
		path, attr, value := string(fields[i]), string(fields[i+1]), string(fields[i+2])
		if path == "" {
			continue
		}
		set := value == "set" || value == "true" || value == "yes"
		switch attr {
		case "linguist-generated":
			if set {
				marks.Generated[path] = true
			}
		case "linguist-vendored":
			if set {
				marks.Vendored[path] = true
			}
		case "linguist-documentation":
			if set {
				marks.Documentation[path] = true
			}
		case "diff":
			// `-diff` (unset) marks content Git should not show as a diff.
			if value == "unset" || value == "false" {
				marks.Generated[path] = true
			}
		}
	}
	return marks
}

// GeneratedPaths is the generated subset of PathMarks.
func (r Repo) GeneratedPaths(paths []string) map[string]bool {
	return r.PathMarks(paths).Generated
}

// TrackedPaths lists the paths present at HEAD, so callers can drop files
// that were deleted inside a history window.
func (r Repo) TrackedPaths() (map[string]bool, error) {
	out, err := r.CommandStdout("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	tracked := make(map[string]bool)
	for _, field := range bytes.Split(out, []byte{0}) {
		if len(field) > 0 {
			tracked[string(field)] = true
		}
	}
	return tracked, nil
}

// collapseGenerated seeds the collapsed-file map for generated files the
// session has not seen before, so a file the reader opened stays open.

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

func GeneratedByPath(path string) bool {
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

package main

import (
	"bufio"
	"io"
	"strings"
)

type patchReader struct {
	reader  *bufio.Reader
	pending string
	path    string
	err     error
}

func newPatchReader(reader io.Reader) *patchReader {
	return &patchReader{reader: bufio.NewReader(reader)}
}

// next returns one complete section. Unrecognized paths are returned empty so
// the caller can fall back to a per-path Git query after the stream ends.
func (p *patchReader) next() (string, string, error) {
	var section strings.Builder
	for {
		line := p.pending
		p.pending = ""
		if line == "" && p.err == nil {
			line, p.err = p.reader.ReadString('\n')
		}
		if line == "" {
			if section.Len() == 0 {
				return "", "", p.err
			}
			text := section.String()
			return patchSectionPath(p.path, text), text, nil
		}
		header := strings.TrimSuffix(line, "\n")
		path, boundary := "", false
		if strings.HasPrefix(header, "diff ") {
			path, boundary = patchPath(header), true
		} else if submodulePath, _, ok := submoduleLine(header); ok && p.path != submodulePath {
			path, boundary = submodulePath, true
		}
		if boundary && section.Len() > 0 {
			p.pending = line
			text := section.String()
			return patchSectionPath(p.path, text), text, nil
		}
		if boundary {
			p.path = path
		}
		section.WriteString(line)
	}
}

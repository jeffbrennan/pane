package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
var submoduleHeader = regexp.MustCompile(`^Submodule (.+) ([0-9a-f]+)\.\.\.?([0-9a-f]+)(?: \(([^)]*)\))?:?$`)

func submoduleLine(line string) (string, string, bool) {
	if match := submoduleHeader.FindStringSubmatch(line); match != nil {
		text := "Submodule " + match[2] + " → " + match[3]
		if match[4] != "" {
			text += " (" + match[4] + ")"
		}
		return match[1], text, true
	}
	for _, suffix := range []string{" contains modified content", " contains untracked content"} {
		if path, ok := strings.CutSuffix(strings.TrimPrefix(line, "Submodule "), suffix); ok && strings.HasPrefix(line, "Submodule ") {
			return path, "Submodule" + suffix, true
		}
	}
	return "", "", false
}

func matchHunkHeader(line string) []string {
	if !strings.HasPrefix(line, "@@ ") {
		return nil
	}
	return hunkHeader.FindStringSubmatch(line)
}

func buildRows(changes []change, width int, wrap bool) []row {
	return buildRowsExpanded(changes, width, wrap, nil)
}

func buildRowsExpanded(changes []change, width int, wrap bool, expanded map[string]bool) []row {
	return buildRowsNumbered(changes, width, wrap, expanded, 0)
}

// buildRowsNumbered builds rows whose line numbers are at least gutterWidth
// wide, so rows rebuilt for one file line up with the rest of the diff.
func buildRowsNumbered(changes []change, width int, wrap bool, expanded map[string]bool, gutterWidth int) []row {
	return buildRowsDisplay(changes, width, wrap, expanded, gutterWidth, false)
}

func displayPath(path string, shortened bool) string {
	if !shortened {
		return clean(path)
	}
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return clean(path)
	}
	return "…/" + clean(strings.Join(parts[len(parts)-2:], "/"))
}

func buildRowsDisplay(changes []change, width int, wrap bool, expanded map[string]bool, gutterWidth int, shortPaths bool) []row {
	var rows []row
	// Share one line number width across files so the number column stays
	// put when moving between them.
	for _, c := range changes {
		gutterWidth = max(gutterWidth, lineNumberWidth(strings.Split(c.diff, "\n")))
	}
	for i, c := range changes {
		oldSyntax, newSyntax := syntaxLines(c.path, c.syntaxDiff)
		if i > 0 {
			rows = append(rows, row{path: c.path, line: -2, kind: 's'})
		}
		deletedFile := strings.Contains(c.diff, "\ndeleted file mode ") && !strings.Contains(c.diff, "\nnew file mode ")
		deletedLines := 0
		if deletedFile {
			inHunk := false
			for _, line := range strings.Split(c.diff, "\n") {
				if matchHunkHeader(line) != nil {
					inHunk = true
				} else if inHunk && strings.HasPrefix(line, "-") {
					deletedLines++
				}
			}
		}
		lines := strings.Split(strings.TrimSuffix(c.diff, "\n"), "\n")
		name := displayPath(c.path, shortPaths)
		if c.oldPath != "" {
			name = displayPath(c.oldPath, shortPaths)
		}
		_, _, submodule := submoduleLine(strings.SplitN(c.diff, "\n", 2)[0])
		header := row{path: c.path, oldPath: c.oldPath, line: -1, text: " " + name, kind: 'f', numberWidth: gutterWidth, submodule: submodule}
		if deletedFile && !expanded[c.path] {
			header.deletedLines = deletedLines
		}
		if c.oldPath == "" {
			rows = append(rows, header)
		} else {
			removed, added := renamePathSpans(c.oldPath, c.path, shortPaths)
			parts := []struct {
				prefix, path string
				spans        []syntaxSpan
			}{{"  ", displayPath(c.oldPath, shortPaths), removed}, {"→ ", displayPath(c.path, shortPaths), added}}
			offset := 0
			for side, item := range parts {
				text := item.prefix + item.path
				header.renameAdded = side == 1
				var spans []syntaxSpan
				for _, span := range item.spans {
					spans = append(spans, syntaxSpan{start: span.start + len(item.prefix), end: span.end + len(item.prefix)})
				}
				byteOffset := 0
				for _, part := range wrapLine(text, max(1, width-5)) {
					header.text, header.offset = " "+part, offset
					header.inline = clipSyntax(spans, byteOffset, byteOffset+len(part))
					for i := range header.inline {
						header.inline[i].start++
						header.inline[i].end++
					}
					if offset > 0 {
						header.kind = 'F'
					}
					rows = append(rows, header)
					byteOffset += len(part)
					offset += max(1, runewidth.StringWidth(part))
				}
			}
		}
		if deletedFile {
			label := "  Deleted file"
			if deletedLines > 0 {
				if expanded[c.path] {
					label += " · e collapse"
				} else {
					label += " · e expand"
				}
			}
			rows = append(rows, row{path: c.path, line: 0, text: label, kind: '@'})
			if !expanded[c.path] {
				continue
			}
		}
		inline := inlineChanges(lines)
		oldLine, newLine, hunks := 0, 0, 0
		inHunk, hasContent := false, false
		var metadata []string
		for i, line := range lines {
			if match := matchHunkHeader(line); match != nil {
				oldLine, _ = strconv.Atoi(match[1])
				newLine, _ = strconv.Atoi(match[3])
				if hunks > 0 {
					rows = append(rows, row{path: c.path, line: i, target: newLine, text: "  ···", kind: '@'})
				}
				hunks++
				inHunk = true
				continue
			}
			if !inHunk {
				switch {
				case submodule:
					if _, text, ok := submoduleLine(line); ok {
						metadata = append(metadata, clean(text))
					} else if strings.TrimSpace(line) != "" {
						metadata = append(metadata, clean(strings.TrimSpace(line)))
					}
				case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
					metadata = append(metadata, "Binary file changed")
				case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "):
					metadata = append(metadata, clean(line))
				case strings.HasPrefix(line, "new file mode "):
					metadata = append(metadata, "New empty file")
				case strings.HasPrefix(line, "deleted file mode "):
					metadata = append(metadata, "Deleted empty file")
				case !strings.HasPrefix(c.diff, "diff --git ") && line != "":
					metadata = append(metadata, clean(line))
				}
				continue
			}
			if len(line) == 0 {
				continue
			}
			kind := line[0]
			if kind == '\\' {
				rows = append(rows, row{path: c.path, line: i, target: max(1, newLine-1), text: "  " + clean(line[2:]), kind: '@'})
				continue
			}
			if kind != ' ' && kind != '+' && kind != '-' {
				continue
			}
			number, target := newLine, max(1, newLine)
			if kind == '-' {
				number = oldLine
			}
			if kind != '+' {
				oldLine++
			}
			if kind != '-' {
				newLine++
			}
			gutter := fmt.Sprintf("%*d %c ", gutterWidth, number, kind)
			if deletedFile && kind == '-' {
				kind = 'd'
			}
			text := clean(strings.ReplaceAll(line[1:], "\t", "    "))
			parts := []string{text}
			if wrap {
				parts = wrapLine(text, max(1, width-len(gutter)))
			}
			offset := 0
			byteOffset := 0
			for partIndex, part := range parts {
				prefix := gutter
				if partIndex > 0 {
					prefix = strings.Repeat(" ", len(gutter))
				}
				lineSyntax, syntaxOK := newSyntax[number]
				if kind == '-' || kind == 'd' {
					lineSyntax, syntaxOK = oldSyntax[number]
				}
				var spans []syntaxSpan
				if syntaxOK && lineSyntax.text == text {
					spans = clipSyntax(lineSyntax.spans, byteOffset, byteOffset+len(part))
				}
				rows = append(rows, row{path: c.path, line: i, offset: offset, target: target, sourceLine: number, gutter: prefix, text: part, kind: kind, syntax: spans, inline: clipSyntax(inline[i], byteOffset, byteOffset+len(part))})
				byteOffset += len(part)
				for _, r := range part {
					offset += runewidth.RuneWidth(r)
				}
			}
			hasContent = true
		}
		if !hasContent {
			if c.oldPath != "" && strings.Contains(c.diff, "\nsimilarity index 100%\n") {
				metadata = append(metadata, "Renamed, no content changes")
			}
			for _, text := range metadata {
				if deletedFile && text == "Deleted empty file" {
					continue
				}
				if (text == "New empty file" || text == "Deleted empty file") && containsBinary(metadata) {
					continue
				}
				rows = append(rows, row{path: c.path, line: len(lines), text: "  " + text, kind: '@', submodule: submodule})
			}
		} else {
			for _, text := range metadata {
				if strings.HasPrefix(text, "old mode ") || strings.HasPrefix(text, "new mode ") {
					rows = append(rows, row{path: c.path, line: len(lines), text: "  " + text, kind: '@'})
				}
			}
		}
	}
	for i := range rows {
		rows[i].numberWidth = gutterWidth
	}
	indexRowHeaders(rows)
	return rows
}

func indexRowHeaders(rows []row) {
	headerIndex := 0
	for i := range rows {
		if i > 0 && rows[i].commitIndex != rows[i-1].commitIndex {
			headerIndex = 0
		}
		if rows[i].kind == 'f' {
			headerIndex = i + 1
		} else {
			rows[i].headerIndex = headerIndex
		}
	}
}

func containsBinary(metadata []string) bool {
	for _, text := range metadata {
		if text == "Binary file changed" {
			return true
		}
	}
	return false
}

func lineNumberWidth(lines []string) int {
	largest := 0
	for _, line := range lines {
		match := matchHunkHeader(line)
		if match == nil {
			continue
		}
		for _, pair := range [][2]string{{match[1], match[2]}, {match[3], match[4]}} {
			start, _ := strconv.Atoi(pair[0])
			count := 1
			if pair[1] != "" {
				count, _ = strconv.Atoi(pair[1])
			}
			largest = max(largest, start+count-1)
		}
	}
	return max(3, len(strconv.Itoa(largest)))
}

package main

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mattn/go-runewidth"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
var submoduleHeader = regexp.MustCompile(`^Submodule (.+) ([0-9a-f]+)\.\.\.?([0-9a-f]+)(?: \(([^)]*)\))?:?$`)

func submoduleLine(line string) (string, string, bool) {
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
	rows, _ := buildRowsDisplay(changes, width, wrap, expanded, gutterWidth, false)
	return rows
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

func buildRowsDisplay(changes []change, width int, wrap bool, expanded map[string]bool, gutterWidth int, shortPaths bool) ([]row, int) {
	// Share one line number width across files so the number column stays
	// put when moving between them.
	widths := make([]int, len(changes))
	parallelEach(len(changes), func(i int) {
		widths[i] = lineNumberWidth(strings.Split(changes[i].diff, "\n"))
	})
	for _, width := range widths {
		gutterWidth = max(gutterWidth, width)
	}
	// Files lay out independently, so large change sets build in parallel.
	parts := make([][]row, len(changes))
	parallelEach(len(changes), func(i int) {
		parts[i] = changeRows(changes[i], i > 0, width, wrap, expanded[changes[i].path], gutterWidth, shortPaths)
	})
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	rows := make([]row, 0, total)
	for i, part := range parts {
		rows = append(rows, part...)
		parts[i] = nil
	}
	indexRowHeaders(rows)
	return rows, gutterWidth
}

// parallelEach calls f for each index in [0, n), one goroutine per core.
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

// changeRows lays out one file; separated adds the blank row that divides it
// from the file before.
func changeRows(c change, separated bool, width int, wrap bool, expanded bool, gutterWidth int, shortPaths bool) []row {
	// Most lines take one row; sizing for them avoids regrowing large files.
	rows := make([]row, 0, strings.Count(c.diff, "\n")+3)
	oldSyntax, newSyntax := syntaxLinesFor(c.path, c.syntaxDiff, c.diff)
	if separated {
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
	header := row{path: c.path, oldPath: c.oldPath, line: -1, text: " " + name, kind: 'f', submodule: submodule}
	if deletedFile && !expanded {
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
				offset += max(1, stringWidth(part))
			}
		}
	}
	if deletedFile {
		label := "  Deleted file"
		if deletedLines > 0 {
			if expanded {
				label += " · e collapse"
			} else {
				label += " · e expand"
			}
		}
		rows = append(rows, row{path: c.path, line: 0, text: label, kind: '@'})
		if !expanded {
			return rows
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
		if deletedFile && kind == '-' {
			kind = 'd'
		}
		text := clean(strings.ReplaceAll(line[1:], "\t", "    "))
		parts := []string{text}
		if wrap {
			parts = wrapLine(text, max(1, width-gutterWidth-3))
		}
		offset := 0
		byteOffset := 0
		for _, part := range parts {
			lineSyntax, syntaxOK := newSyntax[number]
			if kind == '-' || kind == 'd' {
				lineSyntax, syntaxOK = oldSyntax[number]
			}
			var spans []syntaxSpan
			if syntaxOK && lineSyntax.text == text {
				spans = clipSyntax(lineSyntax.spans, byteOffset, byteOffset+len(part))
			}
			rows = append(rows, row{path: c.path, line: i, offset: offset, target: target, sourceLine: number, text: part, kind: kind, syntax: spans, inline: clipSyntax(inline[i], byteOffset, byteOffset+len(part))})
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

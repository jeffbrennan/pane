package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

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
	var rows []row
	for i, c := range changes {
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
		header := row{path: c.path, line: -1, text: " " + clean(c.path), kind: 'f'}
		if deletedFile && !expanded[c.path] {
			header.deletedLines = deletedLines
		}
		rows = append(rows, header)
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
		lines := strings.Split(strings.TrimSuffix(c.diff, "\n"), "\n")
		gutterWidth := lineNumberWidth(lines)
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
			for partIndex, part := range parts {
				prefix := gutter
				if partIndex > 0 {
					prefix = strings.Repeat(" ", len(gutter))
				}
				rows = append(rows, row{path: c.path, line: i, offset: offset, target: target, gutter: prefix, text: part, kind: kind})
				for _, r := range part {
					offset += runewidth.RuneWidth(r)
				}
			}
			hasContent = true
		}
		if !hasContent {
			for _, text := range metadata {
				if deletedFile && text == "Deleted empty file" {
					continue
				}
				if (text == "New empty file" || text == "Deleted empty file") && containsBinary(metadata) {
					continue
				}
				rows = append(rows, row{path: c.path, line: len(lines), text: "  " + text, kind: '@'})
			}
		} else {
			for _, text := range metadata {
				if strings.HasPrefix(text, "old mode ") || strings.HasPrefix(text, "new mode ") {
					rows = append(rows, row{path: c.path, line: len(lines), text: "  " + text, kind: '@'})
				}
			}
		}
	}
	return rows
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

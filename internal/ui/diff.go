package ui

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

func buildRows(changes []git.Change, width int, wrap bool) []row {
	return buildRowsExpanded(changes, width, wrap, nil)
}

func buildRowsExpanded(changes []git.Change, width int, wrap bool, expanded map[string]bool) []row {
	return buildRowsNumbered(changes, width, wrap, expanded, 0)
}

// buildRowsNumbered builds rows whose line numbers are at least gutterWidth
// wide, so rows rebuilt for one file line up with the rest of the diff.
func buildRowsNumbered(changes []git.Change, width int, wrap bool, expanded map[string]bool, gutterWidth int) []row {
	rows, _ := buildRowsDisplay(changes, width, wrap, expanded, gutterWidth, false)
	return rows
}

func displayPath(path string, shortened bool) string {
	if !shortened {
		return syntax.Clean(path)
	}
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return syntax.Clean(path)
	}
	return "…/" + syntax.Clean(strings.Join(parts[len(parts)-2:], "/"))
}

func buildRowsDisplay(changes []git.Change, width int, wrap bool, expanded map[string]bool, gutterWidth int, shortPaths bool) ([]row, int) {
	return buildRowsDisplayReuse(changes, width, wrap, expanded, gutterWidth, shortPaths, nil)
}

func buildRowsDisplayReuse(changes []git.Change, width int, wrap bool, expanded map[string]bool, gutterWidth int, shortPaths bool, reuse []row) ([]row, int) {
	// Share one line number width across files so the number column stays
	// put when moving between them.
	widths := make([]int, len(changes))
	parallelEach(len(changes), func(i int) {
		widths[i] = lineNumberWidthText(changes[i].Diff)
	})
	for _, width := range widths {
		gutterWidth = max(gutterWidth, width)
	}
	// Files lay out independently, so large change sets build in parallel.
	parts := make([][]row, len(changes))
	parallelEach(len(changes), func(i int) {
		parts[i] = changeRows(changes[i], i > 0, width, wrap, expanded[changes[i].Path], gutterWidth, shortPaths)
	})
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	// A single file needs no concatenation. Returning its rows directly avoids
	// copying every row, which matters most for a huge one-file diff.
	if len(parts) == 1 {
		indexRowHeaders(parts[0])
		return parts[0], gutterWidth
	}
	var rows []row
	if cap(reuse) >= total {
		rows = reuse[:0]
	} else {
		rows = make([]row, 0, total+total/64)
	}
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
func changeRows(c git.Change, separated bool, width int, wrap bool, expanded bool, gutterWidth int, shortPaths bool) []row {
	deletedFile := strings.Contains(c.Diff, "\ndeleted file mode ") && !strings.Contains(c.Diff, "\nnew file mode ")
	// A collapsed deleted file only needs its headers, so do not size the row
	// slice for the file's whole contents.
	capacity := 16
	if !deletedFile || expanded {
		capacity = strings.Count(c.Diff, "\n") + 3
	}
	// Most lines take one row; sizing for them avoids regrowing large files.
	rows := make([]row, 0, capacity)
	if separated {
		rows = append(rows, row{path: c.Path, lineIndex: -2, kind: 's'})
	}
	deletedLines := 0
	if deletedFile {
		deletedLines = countDeletedLines(c.Diff)
	}
	trimmed := strings.TrimSuffix(c.Diff, "\n")
	lineCount := strings.Count(trimmed, "\n") + 1
	name := displayPath(c.Path, shortPaths)
	if c.OldPath != "" {
		name = displayPath(c.OldPath, shortPaths)
	}
	_, _, submodule := git.SubmoduleLine(strings.SplitN(c.Diff, "\n", 2)[0])
	header := row{path: c.Path, oldPath: c.OldPath, lineIndex: -1, text: " " + name, kind: 'f', submodule: submodule, generated: c.Generated}
	if c.Generated {
		added, deleted := countGeneratedLines(c.Diff)
		header.setGeneratedCounts(added, deleted)
	}
	if deletedFile && !expanded {
		header.deletedLines = int32(deletedLines)
	}
	if c.OldPath == "" {
		rows = append(rows, header)
	} else {
		removed, added := syntax.RenamePathSpans(c.OldPath, c.Path, shortPaths)
		parts := []struct {
			prefix, path string
			spans        []syntax.Span
		}{{"  ", displayPath(c.OldPath, shortPaths), removed}, {"→ ", displayPath(c.Path, shortPaths), added}}
		offset := 0
		for side, item := range parts {
			text := item.prefix + item.path
			header.renameAdded = side == 1
			var spans []syntax.Span
			for _, span := range item.spans {
				spans = append(spans, syntax.Span{Start: span.Start + len(item.prefix), End: span.End + len(item.prefix)})
			}
			byteOffset := 0
			for _, part := range wrapLine(text, max(1, width-5)) {
				header.text, header.offset = " "+part, int32(offset)
				inline := syntax.Clip(spans, byteOffset, byteOffset+len(part))
				for i := range inline {
					inline[i].Start++
					inline[i].End++
				}
				header.setInlineSpans(inline)
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
		rows = append(rows, row{path: c.Path, text: label, kind: '@'})
		if !expanded {
			return rows
		}
	}
	oldSyntax, newSyntax := syntax.LinesFor(c.Path, c.SyntaxDiff, c.Diff)
	oldLine, newLine, hunks := 0, 0, 0
	inHunk, hasContent := false, false
	var metadata []string
	remaining := trimmed
	for i := 0; i < lineCount; i++ {
		line, next, _ := strings.Cut(remaining, "\n")
		remaining = next
		if match := syntax.MatchHunkHeader(line); match != nil {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[3])
			if hunks > 0 {
				rows = append(rows, (row{path: c.Path, target: newLine, text: "  ···", kind: '@'}).withLineNumber(i))
			}
			hunks++
			inHunk = true
			continue
		}
		if !inHunk {
			switch {
			case submodule:
				if _, text, ok := git.SubmoduleLine(line); ok {
					metadata = append(metadata, syntax.Clean(text))
				} else if strings.TrimSpace(line) != "" {
					metadata = append(metadata, syntax.Clean(strings.TrimSpace(line)))
				}
			case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
				metadata = append(metadata, "Binary file changed")
			case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "):
				metadata = append(metadata, syntax.Clean(line))
			case strings.HasPrefix(line, "new file mode "):
				metadata = append(metadata, "New empty file")
			case strings.HasPrefix(line, "deleted file mode "):
				metadata = append(metadata, "Deleted empty file")
			case !strings.HasPrefix(c.Diff, "diff --git ") && line != "":
				metadata = append(metadata, syntax.Clean(line))
			}
			continue
		}
		if len(line) == 0 {
			continue
		}
		kind := line[0]
		if kind == '\\' {
			rows = append(rows, (row{path: c.Path, target: max(1, newLine-1), text: "  " + syntax.Clean(line[2:]), kind: '@'}).withLineNumber(i))
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
		text := syntax.Clean(strings.ReplaceAll(line[1:], "\t", "    "))
		lineSyntax, syntaxOK := newSyntax[number]
		if kind == '-' || kind == 'd' {
			lineSyntax, syntaxOK = oldSyntax[number]
		}
		syntaxMatches := syntaxOK && lineSyntax.Text == text
		limit := max(1, width-gutterWidth-3)
		// A line that already fits stays one row, which avoids allocating a
		// parts slice for the common case.
		if !wrap || (printableASCII(text) && len(text) <= limit) {
			var spans []syntax.Span
			if syntaxMatches {
				spans = syntax.Clip(lineSyntax.Spans, 0, len(text))
			}
			rows = append(rows, (row{path: c.Path, target: target, sourceLine: number, text: text, kind: kind, spans: newRowSpans(spans, nil, nil), syntaxPresent: spans != nil}).withLineNumber(i))
		} else {
			offset := 0
			byteOffset := 0
			for _, part := range wrapLine(text, limit) {
				var spans []syntax.Span
				if syntaxMatches {
					spans = syntax.Clip(lineSyntax.Spans, byteOffset, byteOffset+len(part))
				}
				rows = append(rows, (row{path: c.Path, offset: int32(offset), target: target, sourceLine: number, text: part, kind: kind, spans: newRowSpans(spans, nil, nil), syntaxPresent: spans != nil}).withLineNumber(i))
				byteOffset += len(part)
				offset += stringWidth(part)
			}
		}
		hasContent = true
	}
	if !hasContent {
		if c.OldPath != "" && strings.Contains(c.Diff, "\nsimilarity index 100%\n") {
			metadata = append(metadata, "Renamed, no content changes")
		}
		for _, text := range metadata {
			if deletedFile && text == "Deleted empty file" {
				continue
			}
			if (text == "New empty file" || text == "Deleted empty file") && containsBinary(metadata) {
				continue
			}
			rows = append(rows, (row{path: c.Path, text: "  " + text, kind: '@', submodule: submodule}).withLineNumber(lineCount))
		}
	} else {
		for _, text := range metadata {
			if strings.HasPrefix(text, "old mode ") || strings.HasPrefix(text, "new mode ") {
				rows = append(rows, (row{path: c.Path, text: "  " + text, kind: '@'}).withLineNumber(lineCount))
			}
		}
	}
	return rows
}

// rowSplice replaces src[at:end] with rows when a file is re-wrapped.
type rowSplice struct {
	at, end int
	rows    []row
}

// planReflow re-wraps only the lines whose cells change at a new content width.
// Rows keep their identity otherwise, so a resize touches a small fraction of
// the rows it would rebuild. It reports false when a row carries syntax spans,
// which can only be regenerated from the raw diff.
func planReflow(src []row, contentWidth, gutterWidth int, wrap bool) ([]rowSplice, bool) {
	limit := max(1, contentWidth-gutterWidth-3)
	headerLimit := max(1, contentWidth-5)
	var splices []rowSplice
	for i := 0; i < len(src); {
		r := src[i]
		if r.spans != nil || r.syntaxPresent {
			return nil, false
		}
		switch r.kind {
		case 'f', 'F':
			end := i + 1
			for end < len(src) && (src[end].kind == 'f' || src[end].kind == 'F') {
				if src[end].spans != nil || src[end].syntaxPresent {
					return nil, false
				}
				end++
			}
			if end == i+1 && len(r.text) <= headerLimit {
				i = end
				continue
			}
			text := r.text
			if end > i+1 {
				var joined strings.Builder
				for k := i; k < end; k++ {
					joined.WriteString(strings.TrimPrefix(src[k].text, " "))
				}
				text = joined.String()
			}
			splices = append(splices, rowSplice{at: i, end: end, rows: wrapHeaderRows(r, text, headerLimit)})
			i = end
		case ' ', '+', '-', 'd':
			if !wrap {
				i++
				continue
			}
			end := i + 1
			for end < len(src) && src[end].offset > 0 {
				if src[end].spans != nil || src[end].syntaxPresent {
					return nil, false
				}
				end++
			}
			if end == i+1 && len(r.text) <= limit {
				i = end
				continue
			}
			text := r.text
			if end > i+1 {
				var joined strings.Builder
				for k := i; k < end; k++ {
					joined.WriteString(src[k].text)
				}
				text = joined.String()
			}
			splices = append(splices, rowSplice{at: i, end: end, rows: wrapContentRows(r, text, limit)})
			i = end
		default:
			i++
		}
	}
	return splices, true
}

func wrapContentRows(base row, text string, limit int) []row {
	parts := wrapLine(text, limit)
	rows := make([]row, 0, len(parts))
	offset := 0
	for _, part := range parts {
		clone := base
		clone.text, clone.offset = part, int32(offset)
		rows = append(rows, clone)
		offset += stringWidth(part)
	}
	return rows
}

func wrapHeaderRows(base row, text string, limit int) []row {
	parts := wrapLine(text, limit)
	rows := make([]row, 0, len(parts))
	offset := 0
	for _, part := range parts {
		clone := base
		clone.text, clone.offset, clone.kind = " "+part, int32(offset), 'f'
		if offset > 0 {
			clone.kind = 'F'
		}
		rows = append(rows, clone)
		offset += max(1, stringWidth(part))
	}
	return rows
}

// filePlan is the row output for one changed file after a width change: either
// the existing rows plus the splices that re-wrap them, or a rebuilt part.
type filePlan struct {
	part    []row
	splices []rowSplice
	rebuild bool
}

func (p filePlan) length() int {
	length := len(p.part)
	for _, splice := range p.splices {
		length += len(splice.rows) - (splice.end - splice.at)
	}
	return length
}

// directions reports whether the plan can be written over its own rows from the
// front or from the back. Only the splices change the row count, so comparing
// the running totals at each splice is enough.
func (p filePlan) directions() (forward, backward bool) {
	forward, backward = true, true
	input, output, previous := 0, 0, 0
	for _, splice := range p.splices {
		input += splice.end - previous
		output += splice.at - previous + len(splice.rows)
		previous = splice.end
		forward = forward && output <= input
		backward = backward && output >= input
	}
	return forward, backward
}

// assembleRowPlans joins per-file plans into one row slice. Reusing the old
// rows is only safe while the destination stays behind (or ahead of) every row
// that has not been read yet, so it picks a direction that holds or allocates.
func assembleRowPlans(plans []filePlan, reuse []row) []row {
	total := 0
	forward, backward := true, true
	for _, plan := range plans {
		total += plan.length()
		planForward, planBackward := plan.directions()
		forward = forward && planForward
		backward = backward && planBackward
	}
	reverse := false
	var rows []row
	if cap(reuse) >= total && (forward || backward) {
		rows = reuse[:total]
		reverse = !forward
	} else {
		rows = make([]row, total)
	}
	at := 0
	if reverse {
		offsets := make([]int, len(plans))
		for i, plan := range plans {
			offsets[i] = at
			at += plan.length()
		}
		for i := len(plans) - 1; i >= 0; i-- {
			writeRowPlan(rows, plans[i], offsets[i], true)
		}
		return rows
	}
	for _, plan := range plans {
		writeRowPlan(rows, plan, at, false)
		at += plan.length()
	}
	return rows
}

// writeRowPlan copies one plan's rows into dst starting at at. Writing
// backwards keeps a growing file from overwriting rows it has not read yet.
func writeRowPlan(dst []row, plan filePlan, at int, reverse bool) {
	if len(plan.splices) == 0 {
		copy(dst[at:], plan.part)
		return
	}
	type piece struct {
		offset int
		rows   []row
	}
	pieces := make([]piece, 0, 2*len(plan.splices)+1)
	previous, offset := 0, 0
	for _, splice := range plan.splices {
		pieces = append(pieces, piece{offset, plan.part[previous:splice.at]})
		offset += splice.at - previous
		pieces = append(pieces, piece{offset, splice.rows})
		offset += len(splice.rows)
		previous = splice.end
	}
	pieces = append(pieces, piece{offset, plan.part[previous:]})
	if reverse {
		for i := len(pieces) - 1; i >= 0; i-- {
			copy(dst[at+pieces[i].offset:], pieces[i].rows)
		}
		return
	}
	for _, p := range pieces {
		copy(dst[at+p.offset:], p.rows)
	}
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
			rows[i].setHeaderRow(headerIndex)
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

func countDeletedLines(diff string) int {
	count, inHunk := 0, false
	for rest := diff; rest != ""; {
		line, next, _ := strings.Cut(rest, "\n")
		rest = next
		if syntax.MatchHunkHeader(line) != nil {
			inHunk = true
		} else if inHunk && strings.HasPrefix(line, "-") {
			count++
		}
	}
	return count
}

func lineNumberWidthText(diff string) int {
	largest := 0
	for rest := diff; rest != ""; {
		line, next, _ := strings.Cut(rest, "\n")
		rest = next
		match := syntax.MatchHunkHeader(line)
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

func lineNumberWidth(lines []string) int {
	largest := 0
	for _, line := range lines {
		match := syntax.MatchHunkHeader(line)
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

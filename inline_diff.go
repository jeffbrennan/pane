package main

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func inlineChanges(lines []string) map[int][]syntaxSpan {
	result := make(map[int][]syntaxSpan)
	inHunk := false
	for i := 0; i < len(lines); {
		if matchHunkHeader(lines[i]) != nil {
			inHunk = true
			i++
			continue
		}
		if !inHunk || !strings.HasPrefix(lines[i], "-") {
			i++
			continue
		}
		oldStart := i
		for i < len(lines) && strings.HasPrefix(lines[i], "-") {
			i++
		}
		newStart := i
		for i < len(lines) && strings.HasPrefix(lines[i], "+") {
			i++
		}
		count := newStart - oldStart
		if count == 0 || count != i-newStart || count > 32 {
			continue
		}
		for j := 0; j < count; j++ {
			old := clean(strings.ReplaceAll(lines[oldStart+j][1:], "\t", "    "))
			new := clean(strings.ReplaceAll(lines[newStart+j][1:], "\t", "    "))
			removed, added := changedSpans(old, new)
			result[oldStart+j], result[newStart+j] = removed, added
		}
	}
	return result
}

func changedSpans(old, new string) ([]syntaxSpan, []syntaxSpan) {
	if len(old)+len(new) > 4096 {
		return nil, nil
	}
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 30 * time.Millisecond
	diffs := dmp.DiffMain(old, new, false)
	diffs = dmp.DiffCleanupSemantic(diffs)
	common := 0
	for _, diff := range diffs {
		if diff.Type == diffmatchpatch.DiffEqual {
			common += utf8.RuneCountInString(strings.TrimSpace(diff.Text))
		}
	}
	if common == 0 || common*3 < max(utf8.RuneCountInString(old), utf8.RuneCountInString(new)) {
		return nil, nil
	}
	var removed, added []syntaxSpan
	oldOffset, newOffset := 0, 0
	for _, diff := range diffs {
		size := len(diff.Text)
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			oldOffset += size
			newOffset += size
		case diffmatchpatch.DiffDelete:
			removed = append(removed, syntaxSpan{start: oldOffset, end: oldOffset + size})
			oldOffset += size
		case diffmatchpatch.DiffInsert:
			added = append(added, syntaxSpan{start: newOffset, end: newOffset + size})
			newOffset += size
		}
	}
	return removed, added
}

func renamePathSpans(old, new string, shortened bool) ([]syntaxSpan, []syntaxSpan) {
	old, new = clean(old), clean(new)
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 5 * time.Millisecond
	diffs := dmp.DiffMain(old, new, false)
	var removed, added []syntaxSpan
	oldOffset, newOffset := 0, 0
	for _, diff := range diffs {
		size := len(diff.Text)
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			oldOffset += size
			newOffset += size
		case diffmatchpatch.DiffDelete:
			removed = append(removed, syntaxSpan{start: oldOffset, end: oldOffset + size})
			oldOffset += size
		case diffmatchpatch.DiffInsert:
			added = append(added, syntaxSpan{start: newOffset, end: newOffset + size})
			newOffset += size
		}
	}
	return visiblePathSpans(old, removed, shortened), visiblePathSpans(new, added, shortened)
}

func visiblePathSpans(path string, spans []syntaxSpan, shortened bool) []syntaxSpan {
	parts := strings.Split(path, "/")
	if !shortened || len(parts) <= 2 {
		return spans
	}
	suffix := strings.Join(parts[len(parts)-2:], "/")
	start := len(path) - len(suffix)
	var visible []syntaxSpan
	for _, span := range spans {
		if span.start < start {
			if len(visible) == 0 || visible[0].start != 0 {
				visible = append(visible, syntaxSpan{start: 0, end: len("…")})
			}
		}
		if span.end > start {
			visible = append(visible, syntaxSpan{start: max(span.start, start) - start + len("…/"), end: span.end - start + len("…/")})
		}
	}
	return visible
}

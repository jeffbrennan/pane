package syntax

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Word-diff results are pure functions of the two line strings, and a scroll
// frame re-renders almost the same visible rows as the frame before it. A
// small FIFO cache keeps that work from repeating without retaining diffs.
const inlineSpanCacheLimit = 1024

type inlineSpanKey struct {
	old, new string
}

type inlineSpanPair struct {
	removed, added []Span
}

var inlineSpanCache = struct {
	sync.Mutex
	entries map[inlineSpanKey]inlineSpanPair
	keys    [inlineSpanCacheLimit]inlineSpanKey
	next    int
}{entries: make(map[inlineSpanKey]inlineSpanPair, inlineSpanCacheLimit)}

func InlineChanges(lines []string) map[int][]Span {
	result := make(map[int][]Span)
	inHunk := false
	for i := 0; i < len(lines); {
		if MatchHunkHeader(lines[i]) != nil {
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
			old := Clean(strings.ReplaceAll(lines[oldStart+j][1:], "\t", "    "))
			new := Clean(strings.ReplaceAll(lines[newStart+j][1:], "\t", "    "))
			removed, added := ChangedSpans(old, new)
			result[oldStart+j], result[newStart+j] = removed, added
		}
	}
	return result
}

func ChangedSpans(old, new string) ([]Span, []Span) {
	if len(old)+len(new) > 4096 {
		return nil, nil
	}
	key := inlineSpanKey{old, new}
	inlineSpanCache.Lock()
	cached, ok := inlineSpanCache.entries[key]
	inlineSpanCache.Unlock()
	if ok {
		return cached.removed, cached.added
	}
	removed, added := changedSpans(old, new)
	inlineSpanCache.Lock()
	if _, exists := inlineSpanCache.entries[key]; !exists {
		if len(inlineSpanCache.entries) >= inlineSpanCacheLimit {
			delete(inlineSpanCache.entries, inlineSpanCache.keys[inlineSpanCache.next])
		}
		inlineSpanCache.keys[inlineSpanCache.next] = key
		inlineSpanCache.next = (inlineSpanCache.next + 1) % inlineSpanCacheLimit
		inlineSpanCache.entries[key] = inlineSpanPair{removed, added}
	}
	inlineSpanCache.Unlock()
	return removed, added
}

func changedSpans(old, new string) ([]Span, []Span) {
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
	var removed, added []Span
	oldOffset, newOffset := 0, 0
	for _, diff := range diffs {
		size := len(diff.Text)
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			oldOffset += size
			newOffset += size
		case diffmatchpatch.DiffDelete:
			removed = append(removed, Span{Start: oldOffset, End: oldOffset + size})
			oldOffset += size
		case diffmatchpatch.DiffInsert:
			added = append(added, Span{Start: newOffset, End: newOffset + size})
			newOffset += size
		}
	}
	return removed, added
}

func RenamePathSpans(old, new string, shortened bool) ([]Span, []Span) {
	old, new = Clean(old), Clean(new)
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 5 * time.Millisecond
	diffs := dmp.DiffMain(old, new, false)
	var removed, added []Span
	oldOffset, newOffset := 0, 0
	for _, diff := range diffs {
		size := len(diff.Text)
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			oldOffset += size
			newOffset += size
		case diffmatchpatch.DiffDelete:
			removed = append(removed, Span{Start: oldOffset, End: oldOffset + size})
			oldOffset += size
		case diffmatchpatch.DiffInsert:
			added = append(added, Span{Start: newOffset, End: newOffset + size})
			newOffset += size
		}
	}
	return VisiblePathSpans(old, removed, shortened), VisiblePathSpans(new, added, shortened)
}

func VisiblePathSpans(path string, spans []Span, shortened bool) []Span {
	parts := strings.Split(path, "/")
	if !shortened || len(parts) <= 2 {
		return spans
	}
	suffix := strings.Join(parts[len(parts)-2:], "/")
	start := len(path) - len(suffix)
	var visible []Span
	for _, span := range spans {
		if span.Start < start {
			if len(visible) == 0 || visible[0].Start != 0 {
				visible = append(visible, Span{Start: 0, End: len("…")})
			}
		}
		if span.End > start {
			visible = append(visible, Span{Start: max(span.Start, start) - start + len("…/"), End: span.End - start + len("…/")})
		}
	}
	return visible
}

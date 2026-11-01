package main

import (
	"math"
	"strconv"
	"testing"
)

func TestRowSpansAreSparseAndCopyOnWrite(t *testing.T) {
	var original row
	original.setSyntaxSpans([]syntaxSpan{})
	if original.spans != nil || original.syntaxSpans() == nil {
		t.Fatal("empty syntax should preserve its presence without allocating span storage")
	}
	original.setInlineSpans([]syntaxSpan{{Start: 1, End: 2}})
	copy := original
	copy.setSyntaxSpans([]syntaxSpan{{Start: 0, End: 1, Color: "red"}})
	if len(original.syntaxSpans()) != 0 || len(copy.syntaxSpans()) != 1 || original.spans == copy.spans {
		t.Fatal("updating copied row syntax changed the original")
	}
	copy.setInlineSpans([]syntaxSpan{{Start: 3, End: 4}})
	if original.inlineSpans()[0].Start != 1 || copy.inlineSpans()[0].Start != 3 {
		t.Fatal("updating copied inline spans changed the original")
	}
	copy.setSyntaxSpans(nil)
	if copy.syntaxSpans() != nil || len(copy.inlineSpans()) != 1 {
		t.Fatal("clearing syntax lost inline spans")
	}
}

func TestHighlightRowsDoesNotModifyCopiedRows(t *testing.T) {
	diff := "diff --git a/file.go b/file.go\n@@ -1 +1 @@\n-var total = 1\n+var total = 2\n"
	original := buildRows([]Change{{Path: "file.go", Diff: diff}}, 80, false)
	for i := range original {
		if original[i].kind == '+' {
			original[i].setInlineSpans([]syntaxSpan{{Start: 4, End: 9}})
		}
	}
	copied := append([]row(nil), original...)
	highlightRows(copied, "file.go", 0, diff, diff)
	found := false
	for i := range original {
		if original[i].kind != '+' {
			continue
		}
		if len(original[i].inlineSpans()) == 0 || len(copied[i].syntaxSpans()) == 0 {
			t.Fatal("fixture did not produce both inline and syntax spans")
		}
		if original[i].syntaxSpans() != nil || len(copied[i].inlineSpans()) == 0 {
			t.Fatal("highlighting a copy changed the original or lost inline spans")
		}
		found = true
	}
	if !found {
		t.Fatal("missing added row")
	}
}

func TestRowIndexBoundaries(t *testing.T) {
	values := []int{math.MinInt32, math.MinInt32 + 1, -2, -1, 0, 1, math.MaxInt32}
	if strconv.IntSize > 32 {
		high := int64(math.MaxInt32) + 1
		low := int64(math.MinInt32) - 1
		values = append(values, int(low), int(high), int(high+12345))
	}
	for _, line := range values {
		for _, header := range values {
			var r row
			r.setLineNumber(line)
			r.setHeaderRow(header)
			if gotLine, gotHeader := r.lineNumber(), r.headerRow(); gotLine != line || gotHeader != header {
				t.Fatalf("line/header = %d/%d, want %d/%d", gotLine, gotHeader, line, header)
			}
			if line > math.MinInt32 && line <= math.MaxInt32 && header > math.MinInt32 && header <= math.MaxInt32 && r.spans != nil {
				t.Fatal("compact indexes unexpectedly allocated overflow storage")
			}
		}
	}
}

func TestWideRowIndexesSurviveSpanChangesAndCopies(t *testing.T) {
	var original row
	original.setLineNumber(math.MinInt32)
	original.setHeaderRow(math.MinInt32)
	original.setSyntaxSpans([]syntaxSpan{{Start: 0, End: 1, Color: "red"}})
	original.setInlineSpans([]syntaxSpan{{Start: 1, End: 2}})
	copy := original
	copy.setLineNumber(math.MinInt32 + 1)
	copy.setHeaderRow(math.MaxInt32)
	copy.setSyntaxSpans(nil)
	if original.lineNumber() != math.MinInt32 || original.headerRow() != math.MinInt32 || len(original.syntaxSpans()) != 1 {
		t.Fatal("updating the copy changed the original")
	}
	if copy.lineNumber() != math.MinInt32+1 || copy.headerRow() != math.MaxInt32 || copy.syntaxSpans() != nil || len(copy.inlineSpans()) != 1 {
		t.Fatal("compact indexes or spans changed unexpectedly")
	}
	if copy.wideIndexes() != nil {
		t.Fatal("unused overflow storage was retained")
	}
	if strconv.IntSize > 32 {
		copy = original
		low := int64(math.MinInt32) - 1
		copy.setHeaderRow(int(low))
		if original.headerRow() != math.MinInt32 || copy.headerRow() != int(low) || copy.lineNumber() != math.MinInt32 {
			t.Fatal("overflow updates were not isolated")
		}
	}
}

func TestLargeSourceLinesAndOutOfRangeHeaders(t *testing.T) {
	if strconv.IntSize <= 32 {
		t.Skip("source line exceeds native int range")
	}
	large := int64(math.MaxInt32) + 1
	diff := "@@ -0,0 +" + strconv.FormatInt(large, 10) + " @@\n+large line\n"
	rows := buildRows([]Change{{Path: "file", Diff: diff}}, 80, false)
	m := model{width: 80, height: 10, gutterWidth: 10, rows: rows}
	m.sourceLineJump(int(large))
	if got := m.rows[m.pos]; got.sourceLine != int(large) || got.target != int(large) {
		t.Fatalf("large source line was truncated: %#v", got)
	}
	m.rows[m.pos].setHeaderRow(int(large))
	if m.fileHeaderAt(m.pos) != nil {
		t.Fatal("out-of-range header should not refer to a visible row")
	}
}

package main

import (
	"strings"
	"testing"
)

func TestInlineChanges(t *testing.T) {
	diff := "@@ -1 +1 @@\n-Hello, café world!\n+Hello, café there!\n"
	rows := buildRows([]change{{path: "notes.md", diff: diff}}, 80, true)
	m := model{width: 60, wrap: true, rows: rows}
	for _, tt := range []struct {
		index int
		word  string
		color string
	}{{1, "world", "\x1b[48;2;100;38;43m"}, {2, "there", "\x1b[48;2;42;83;52m"}} {
		inline := m.inlineSpansForRow(tt.index)
		row := rows[tt.index]
		if len(inline) != 1 || row.text[inline[0].start:inline[0].end] != tt.word {
			t.Fatalf("incorrect inline ranges: %#v", row)
		}
		if rendered := m.renderNumberedDiffRow(row, tt.index); !strings.Contains(rendered, tt.color+tt.word) {
			t.Fatalf("missing inline highlight: %q", rendered)
		}
	}
}

func TestInlineChangesSkipUnpairedAndUnrelatedLines(t *testing.T) {
	for _, diff := range []string{
		"@@ -1 +1,2 @@\n-one\n+one edited\n+extra\n",
		"@@ -1 +1 @@\n-completely different\n+unrelated text\n",
	} {
		m := model{width: 80, wrap: true}
		m.rows = buildRows([]change{{path: "file.txt", diff: diff}}, 80, true)
		for i, r := range m.rows {
			if len(m.inlineSpansForRow(i)) > 0 {
				t.Fatalf("unexpected inline highlight: %#v", r)
			}
		}
	}
}

func TestInlineChangesStayBoundedOnHugeBlocks(t *testing.T) {
	var diff strings.Builder
	diff.WriteString("@@ -1,100 +1,100 @@\n")
	for i := 0; i < 100; i++ {
		diff.WriteString("-old line\n")
	}
	for i := 0; i < 100; i++ {
		diff.WriteString("+new line\n")
	}
	m := model{width: 80, wrap: true}
	m.rows = buildRows([]change{{path: "file.txt", diff: diff.String()}}, 80, true)
	for i := range m.rows {
		if spans := m.inlineSpansForRow(i); len(spans) > 0 {
			t.Fatalf("row %d paired a block beyond the inline limit", i)
		}
	}
}

func TestInlineChangesWrapAndScroll(t *testing.T) {
	diff := "@@ -1 +1 @@\n-hello beautiful world\n+hello beautiful earth\n"
	rows := buildRows([]change{{path: "file.txt", diff: diff}}, 20, true)
	m := model{width: 20, wrap: true, rows: rows}
	for i, r := range rows {
		if r.kind == '+' && strings.Contains(r.text, "earth") && len(m.inlineSpansForRow(i)) == 0 {
			t.Fatal("wrapped change lost inline highlighting")
		}
	}
	rows = buildRows([]change{{path: "file.txt", diff: diff}}, 80, false)
	m = model{width: 20, horizontal: 10, rows: rows}
	if rendered := m.renderNumberedDiffRow(rows[2], 2); !strings.Contains(rendered, "\x1b[48;2;42;83;52m") {
		t.Fatalf("scrolled change lost inline highlighting: %q", rendered)
	}
}

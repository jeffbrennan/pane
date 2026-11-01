package main

import (
	"strings"
	"testing"
)

func TestInlineChanges(t *testing.T) {
	diff := "@@ -1 +1 @@\n-Hello, café world!\n+Hello, café there!\n"
	rows := buildRows([]change{{path: "notes.md", diff: diff}}, 80, true)
	m := model{width: 60, wrap: true}
	for _, tt := range []struct {
		row   row
		word  string
		color string
	}{{rows[1], "world", "\x1b[48;2;100;38;43m"}, {rows[2], "there", "\x1b[48;2;42;83;52m"}} {
		inline := tt.row.inlineSpans()
		if len(inline) != 1 || tt.row.text[inline[0].start:inline[0].end] != tt.word {
			t.Fatalf("incorrect inline ranges: %#v", tt.row)
		}
		if rendered := m.renderDiffRow(tt.row, false); !strings.Contains(rendered, tt.color+tt.word) {
			t.Fatalf("missing inline highlight: %q", rendered)
		}
	}
}

func TestInlineChangesSkipUnpairedAndUnrelatedLines(t *testing.T) {
	for _, diff := range []string{
		"@@ -1 +1,2 @@\n-one\n+one edited\n+extra\n",
		"@@ -1 +1 @@\n-completely different\n+unrelated text\n",
	} {
		for _, r := range buildRows([]change{{path: "file.txt", diff: diff}}, 80, true) {
			if len(r.inlineSpans()) > 0 {
				t.Fatalf("unexpected inline highlight: %#v", r)
			}
		}
	}
}

func TestInlineChangesWrapAndScroll(t *testing.T) {
	diff := "@@ -1 +1 @@\n-hello beautiful world\n+hello beautiful earth\n"
	rows := buildRows([]change{{path: "file.txt", diff: diff}}, 20, true)
	for _, r := range rows {
		if r.kind == '+' && strings.Contains(r.text, "earth") && len(r.inlineSpans()) == 0 {
			t.Fatal("wrapped change lost inline highlighting")
		}
	}
	rows = buildRows([]change{{path: "file.txt", diff: diff}}, 80, false)
	m := model{width: 20, horizontal: 10}
	if rendered := m.renderDiffRow(rows[2], false); !strings.Contains(rendered, "\x1b[48;2;42;83;52m") {
		t.Fatalf("scrolled change lost inline highlighting: %q", rendered)
	}
}

package main

import "testing"

func TestRowSpansAreSparseAndCopyOnWrite(t *testing.T) {
	var original row
	original.setSyntaxSpans([]syntaxSpan{})
	if original.spans != nil || original.syntaxSpans() == nil {
		t.Fatal("empty syntax should preserve its presence without allocating span storage")
	}
	original.setInlineSpans([]syntaxSpan{{start: 1, end: 2}})
	copy := original
	copy.setSyntaxSpans([]syntaxSpan{{start: 0, end: 1, color: "red"}})
	if len(original.syntaxSpans()) != 0 || len(copy.syntaxSpans()) != 1 || original.spans == copy.spans {
		t.Fatal("updating copied row syntax changed the original")
	}
	copy.setInlineSpans([]syntaxSpan{{start: 3, end: 4}})
	if original.inlineSpans()[0].start != 1 || copy.inlineSpans()[0].start != 3 {
		t.Fatal("updating copied inline spans changed the original")
	}
	copy.setSyntaxSpans(nil)
	if copy.syntaxSpans() != nil || len(copy.inlineSpans()) != 1 {
		t.Fatal("clearing syntax lost inline spans")
	}
}

func TestHighlightRowsDoesNotModifyCopiedRows(t *testing.T) {
	diff := "diff --git a/file.go b/file.go\n@@ -1 +1 @@\n-var total = 1\n+var total = 2\n"
	original := buildRows([]change{{path: "file.go", diff: diff}}, 80, false)
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

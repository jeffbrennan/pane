package main

import (
	"strings"
	"testing"
)

func TestUnifiedDiffPresentation(t *testing.T) {
	diff := "diff --git a/x b/x\nindex 123..456 100644\n--- a/x\n+++ b/x\n@@ -10,3 +10,3 @@ section\n context\n-old\n+new\n +++literal\n@@ -30 +30 @@\n-removed\n+added\n\\ No newline at end of file\n"
	rows := buildRows([]change{{path: "x", diff: diff}}, 80, true)
	var displayed []string
	for _, row := range rows {
		displayed = append(displayed, row.gutter+row.text)
	}
	got := strings.Join(displayed, "\n")
	for _, expected := range []string{" 10   context", " 11 - old", " 11 + new", " 12   +++literal", "  ···", " 30 - removed", " 30 + added", "No newline at end of file"} {
		if !strings.Contains(got, expected) {
			t.Errorf("missing %q in %q", expected, got)
		}
	}
	for _, unwanted := range []string{"index 123", "--- a/x", "+++ b/x", "@@"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected %q in %q", unwanted, got)
		}
	}
}

func TestWrappedLinesKeepGutter(t *testing.T) {
	rows := buildRows([]change{{path: "x", diff: "@@ -0,0 +1 @@\n+abcdefgh\n"}}, 10, true)
	if len(rows) != 3 || rows[1].gutter != "  1 + " || rows[1].text != "abcd" || rows[2].gutter != "      " || rows[2].text != "efgh" {
		t.Fatalf("rows: %#v", rows)
	}
}

func TestNonTextChanges(t *testing.T) {
	tests := []struct{ diff, want string }{
		{"diff --git a/x b/x\nnew file mode 100644\nindex 000..abc\nBinary files /dev/null and b/x differ\n", "Binary file changed"},
		{"diff --git a/x b/x\nnew file mode 100644\nindex 000..abc\n", "New empty file"},
		{"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n", "old mode 100644"},
		{"Merge conflict: resolve outside pane.", "Merge conflict"},
	}
	for _, tt := range tests {
		rows := buildRows([]change{{path: "x", diff: tt.diff}}, 80, true)
		var text []string
		for _, row := range rows {
			text = append(text, row.text)
		}
		got := strings.Join(text, "\n")
		if !strings.Contains(got, tt.want) {
			t.Errorf("missing %q: %q", tt.want, got)
		}
		if strings.Contains(got, "Binary file changed") && strings.Contains(got, "empty file") {
			t.Errorf("binary shown as empty: %q", got)
		}
	}
}

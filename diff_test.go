package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUnifiedDiffPresentation(t *testing.T) {
	diff := "diff --git a/x b/x\nindex 123..456 100644\n--- a/x\n+++ b/x\n@@ -10,3 +10,3 @@ section\n context\n-old\n+new\n +++literal\n@@ -30 +30 @@\n-removed\n+added\n\\ No newline at end of file\n"
	rows := buildRows([]change{{path: "x", diff: diff}}, 80, true)
	var displayed []string
	for _, row := range rows {
		displayed = append(displayed, rowGutter(row)+row.text)
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

func TestToggleDeletedFileInDiffViews(t *testing.T) {
	diff := "diff --git a/x b/x\ndeleted file mode 100644\n--- a/x\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n"
	for _, screen := range []string{"", "commit"} {
		m := model{width: 80, height: 20, screen: screen, snapshot: snapshot{unstaged: []change{{path: "x", diff: diff}}}}
		if screen == "commit" {
			m.history.changes = m.snapshot.unstaged
			m.rebuildCommit()
		} else {
			m.rebuild()
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
		m = updated.(model)
		view := m.View()
		if !strings.Contains(view, "old") || !strings.Contains(view, "e collapse") {
			t.Errorf("screen %q did not expand deletion: %q", screen, view)
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
		m = updated.(model)
		if view = m.View(); strings.Contains(view, "old") || !strings.Contains(view, "e expand") {
			t.Errorf("screen %q did not collapse deletion: %q", screen, view)
		}
	}
}

func TestWrappedLinesKeepGutter(t *testing.T) {
	rows := buildRows([]change{{path: "x", diff: "@@ -0,0 +1 @@\n+abcdefgh\n"}}, 10, true)
	if len(rows) != 3 || rowGutter(rows[1]) != "  1 + " || rows[1].text != "abcd" || rowGutter(rows[2]) != "      " || rows[2].text != "efgh" {
		t.Fatalf("rows: %#v", rows)
	}
}

func TestRowGutterRendersLineKindsAndContinuations(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  row
		want string
	}{
		{"addition", row{kind: '+', numberWidth: 3, sourceLine: 42}, " 42 + "},
		{"context", row{kind: ' ', numberWidth: 4, sourceLine: 9}, "   9   "},
		{"deletion", row{kind: '-', numberWidth: 3, sourceLine: 0}, "  0 - "},
		{"deleted file", row{kind: 'd', numberWidth: 3, sourceLine: 3}, "  3 - "},
		{"continuation", row{kind: '+', numberWidth: 3, sourceLine: 42, offset: 4}, "      "},
		{"header", row{kind: 'f', numberWidth: 3}, ""},
		{"uninitialized", row{kind: '+'}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rowGutter(tc.row); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSourceLineJumpUsesDerivedGutter(t *testing.T) {
	changes := []change{{path: "file", diff: "@@ -10,2 +10,2 @@\n first\n-old\n+new\n"}}
	rows := buildRows(changes, 80, true)
	m := model{width: 80, height: 10, rows: rows}
	m.sourceLineJump(11)
	if got := m.rows[m.pos]; got.kind != '+' || got.sourceLine != 11 {
		t.Fatalf("source line jump selected %#v", got)
	}
}

func TestDeletedFilePresentation(t *testing.T) {
	diff := "diff --git a/x b/x\ndeleted file mode 100644\n--- a/x\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-first\n-second\n"
	rows := buildRows([]change{{path: "x", diff: diff}}, 80, true)
	if len(rows) != 2 || rows[1].text != "  Deleted file · e expand" {
		t.Fatalf("deleted file rows: %#v", rows)
	}
	m := model{width: 80, rows: rows}
	if m.fileEntries()[0].deleted != 2 {
		t.Fatal("collapsed deleted file lost its deletion counts")
	}
	rows = buildRowsExpanded([]change{{path: "x", diff: diff}}, 80, true, map[string]bool{"x": true})
	m.rows = rows
	if len(rows) != 4 || rows[1].text != "  Deleted file · e collapse" || rows[2].kind != 'd' || rows[3].kind != 'd' || strings.Contains(m.renderDiffRow(rows[2], false), "\x1b[31m") || m.fileEntries()[0].deleted != 2 {
		t.Fatalf("expanded deleted file rows: %#v", rows)
	}

	modified := buildRows([]change{{path: "x", diff: "diff --git a/x b/x\n@@ -1 +0,0 @@\n-first\n"}}, 80, true)
	if modified[1].kind != '-' || !strings.Contains(m.renderDiffRow(modified[1], false), "\x1b[48;2;65;37;41m") {
		t.Fatal("ordinary line removals should have a red tint")
	}
}

func TestFileTypeChangeIsNotFullRemoval(t *testing.T) {
	diff := "diff --git a/x b/x\ndeleted file mode 100644\n@@ -1 +0,0 @@\n-old\ndiff --git a/x b/x\nnew file mode 120000\n@@ -0,0 +1 @@\n+new\n"
	rows := buildRows([]change{{path: "x", diff: diff}}, 80, true)
	if rows[1].kind != '-' {
		t.Fatalf("type change treated as deletion: %#v", rows)
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

func TestSplitPatch(t *testing.T) {
	patch := "diff --git a/one b/one\nindex 1..2\n--- a/one\n+++ b/one\n@@ -1 +1 @@\n-diff --git a/x b/x\n+new\n" +
		"diff --git a/a b/c b/a b/c\n+++ b/a b/c\n+spaced\n" +
		"diff --git \"a/q\\\"t\" \"b/q\\\"t\"\n+quoted\n" +
		"diff --cc conflict\n+combined\n" +
		"diff --git a/link b/link\ndeleted file mode 100644\n" +
		"diff --git a/link b/link\nnew file mode 120000\n"
	patches := splitPatch(patch)
	if len(patches) != 3 {
		t.Fatalf("patches: %#v", patches)
	}
	if !strings.HasPrefix(patches["one"], "diff --git a/one b/one\n") || !strings.HasSuffix(patches["one"], "+new\n") {
		t.Fatalf("one: %q", patches["one"])
	}
	if patches["a b/c"] != "diff --git a/a b/c b/a b/c\n+++ b/a b/c\n+spaced\n" {
		t.Fatalf("spaced: %q", patches["a b/c"])
	}
	if !strings.Contains(patches["link"], "deleted file mode") || !strings.Contains(patches["link"], "new file mode") {
		t.Fatalf("typechange sections not joined: %q", patches["link"])
	}
}

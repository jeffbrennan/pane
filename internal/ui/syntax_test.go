package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
	"github.com/mattn/go-runewidth"
)

func TestSyntaxUsesFullOldAndNewContext(t *testing.T) {
	r := testRepo(t)
	prefix := "package main\n/*\n" + strings.Repeat("comment\n", 8)
	writeFile(t, r, "file.go", prefix+"old text\n*/\n")
	if err := r.Stage("file.go", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.go", prefix+"staged text\n*/\n")
	if err := r.Stage("file.go", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.go", prefix+"working text\n*/\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	assertCommentRows(t, s.Staged, "old text", "staged text")
	assertCommentRows(t, s.Unstaged, "staged text", "working text")
	if _, err := r.Action("commit", "-m", "change"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertCommentRows(t, changes, "old text", "staged text")
}

func TestWarmSyntaxDefersWholeFilesToCursor(t *testing.T) {
	diff := "diff --git a/file.go b/file.go\n@@ -1 +1 @@\n-var old = 1\n+var new = 2\n"
	c := git.Change{Path: "deferred-file.go", Diff: "@@ -1 +1 @@\n-var old = 1\n+var new = 2\n", SyntaxDiff: diff}
	syntax.ResetCache()
	git.WarmSyntax([]git.Change{c})
	entries := syntax.CacheSize()
	whole := syntax.CachesWholeFile(c.Path, diff)
	if entries == 0 || whole {
		t.Fatal("warmSyntax tokenized every whole file instead of only displayed lines")
	}
	m := model{width: 80, height: 10, rows: buildRows([]git.Change{c}, 80, true), snapshot: git.Snapshot{Unstaged: []git.Change{c}}, pos: 1}
	m.warmCursorSyntax()
	if m.syntaxWarmPath != c.Path {
		t.Fatal("whole-file warm was not scheduled for the cursor file")
	}
	m.syntaxWarmPath = c.Path
	m.warmCursorSyntax()
	if m.syntaxWarmPath != c.Path {
		t.Fatal("cursor warm state changed on a repeated call")
	}
}

func TestSyntaxDiffBackgrounds(t *testing.T) {
	diff := "@@ -1,2 +1,2 @@\n package main\n-var old = 1\n+var current = 2\n"
	rows := buildRows([]git.Change{{Path: "file.go", Diff: diff, SyntaxDiff: diff}}, 80, true)
	m := model{width: 40, wrap: true, search: searchState{query: "current"}}
	escapes := regexp.MustCompile("\\x1b\\[[0-9;]*m")
	for _, r := range rows {
		if r.kind != '+' && r.kind != '-' && r.kind != ' ' {
			continue
		}
		rendered := m.renderDiffRow(r, false)
		background := ""
		switch r.kind {
		case '+':
			background = "\x1b[48;2;30;55;40m"
		case '-':
			background = "\x1b[48;2;65;37;41m"
		}
		if background == "" {
			if strings.Contains(rendered, "\x1b[48;2;65;37;41m") || strings.Contains(rendered, "\x1b[48;2;30;55;40m") {
				t.Fatalf("context has diff background: %q", rendered)
			}
			continue
		}
		if !strings.Contains(rendered, background) || strings.Count(rendered, "\x1b[0m") != 1 || !strings.HasSuffix(rendered, "\x1b[0m") {
			t.Fatalf("background not preserved to end of row: %q", rendered)
		}
		if width := runewidth.StringWidth(escapes.ReplaceAllString(rendered, "")); width != m.width {
			t.Fatalf("background row width = %d, want %d: %q", width, m.width, rendered)
		}
	}
}

func assertCommentRows(t *testing.T, changes []git.Change, texts ...string) {
	t.Helper()
	rows := buildRows(changes, 80, true)
	for _, text := range texts {
		found := false
		for _, r := range rows {
			if r.text == text {
				found = true
				if len(r.syntaxSpans()) == 0 || r.syntaxSpans()[0].Color != "\x1b[90m" {
					t.Errorf("%q not highlighted as comment: %#v", text, r.syntaxSpans())
				}
			}
		}
		if !found {
			t.Errorf("missing row %q", text)
		}
	}
}

func TestSyntaxWrappingScrollingAndSearch(t *testing.T) {
	diff := "diff --git a/file.go b/file.go\nnew file mode 100644\n@@ -0,0 +1 @@\n+func example() { println(\"hello world\") }\n"
	c := git.Change{Path: "file.go", Diff: diff, SyntaxDiff: diff}
	rows := buildRows([]git.Change{c}, 20, true)
	found := false
	for _, r := range rows {
		if strings.Contains(r.text, "example") {
			m := model{width: 22, wrap: true, search: searchState{query: "example"}}
			rendered := m.renderDiffRow(r, false)
			if !strings.Contains(rendered, "\x1b[7mexample") || !strings.Contains(rendered, "\x1b[27m") || !strings.Contains(rendered, "\x1b[34m") {
				t.Fatalf("missing search or syntax highlighting: %q", rendered)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing wrapped function name")
	}
	rows = buildRows([]git.Change{c}, 80, false)
	m := model{width: 25, horizontal: 5, search: searchState{query: "example"}}
	rendered := m.renderDiffRow(rows[1], false)
	if !strings.Contains(rendered, "\x1b[7mexample") || !strings.Contains(rendered, "\x1b[27m") || !strings.Contains(rendered, "\x1b[34m") {
		t.Fatalf("missing scrolled search or syntax highlighting: %q", rendered)
	}
}

func TestSyntaxFallbackForMissingContextAndOtherLanguages(t *testing.T) {
	diff := "@@ -5 +5 @@\n-old\n+new\n"
	for _, c := range []git.Change{{Path: "file.go", Diff: diff, SyntaxDiff: diff}, {Path: "file.py", Diff: diff, SyntaxDiff: diff}, {Path: "file.txt", Diff: "@@ -0,0 +1 @@\n+plain text\n", SyntaxDiff: "@@ -0,0 +1 @@\n+plain text\n"}} {
		for _, r := range buildRows([]git.Change{c}, 80, true) {
			if r.syntaxSpans() != nil {
				t.Fatalf("unexpected syntax for %s: %#v", c.Path, r)
			}
		}
	}
}

func TestSQLMultilineSyntaxAcrossDiffViews(t *testing.T) {
	r := testRepo(t)
	prefix := "/*\n" + strings.Repeat("comment\n", 8)
	writeFile(t, r, "query.sql", prefix+"old text\n*/\nSELECT 1;\n")
	if err := r.Stage("query.sql", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "query.sql", prefix+"staged text\n*/\nSELECT 1;\n")
	if err := r.Stage("query.sql", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "query.sql", prefix+"working text\n*/\nSELECT 1;\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	assertCommentRows(t, s.Staged, "old text", "staged text")
	assertCommentRows(t, s.Unstaged, "staged text", "working text")
	if _, err := r.Action("commit", "-m", "change"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertCommentRows(t, changes, "old text", "staged text")
}

func TestPythonSyntaxTokensAndExtensions(t *testing.T) {
	diff := "@@ -0,0 +1,3 @@\n+def greet(name):\n+    return \"hello\"\n+    # comment\n"
	for _, path := range []string{"file.py", "file.pyi", "file.pyw"} {
		rows := buildRows([]git.Change{{Path: path, Diff: diff, SyntaxDiff: diff}}, 80, true)
		for _, test := range []struct {
			row   int
			text  string
			color string
		}{{1, "def", "\x1b[35m"}, {1, "greet", "\x1b[34m"}, {2, "hello", "\x1b[32m"}, {3, "# comment", "\x1b[90m"}} {
			found := false
			for _, span := range rows[test.row].syntaxSpans() {
				if span.Color == test.color && strings.Contains(rows[test.row].text[span.Start:span.End], test.text) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: missing highlighted %q in %#v", path, test.text, rows[test.row].syntaxSpans())
			}
		}
	}
}

func TestPythonMultilineSyntaxAcrossDiffViews(t *testing.T) {
	r := testRepo(t)
	prefix := "message = \"\"\"\n" + strings.Repeat("text\n", 8)
	writeFile(t, r, "file.py", prefix+"old text\n\"\"\"\n")
	if err := r.Stage("file.py", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.py", prefix+"staged text\n\"\"\"\n")
	if err := r.Stage("file.py", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.py", prefix+"working text\n\"\"\"\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	assertStringRows(t, s.Staged, "old text", "staged text")
	assertStringRows(t, s.Unstaged, "staged text", "working text")
	if _, err := r.Action("commit", "-m", "change"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertStringRows(t, changes, "old text", "staged text")
}

func TestUntrackedPythonSyntax(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new.py", "def example():\n    return 42\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	rows := buildRows(s.Unstaged, 80, true)
	if len(rows) < 2 || len(rows[1].syntaxSpans()) == 0 || rows[1].syntaxSpans()[0].Color != "\x1b[35m" {
		t.Fatalf("untracked Python file missing keyword highlighting: %#v", rows)
	}
}

func assertStringRows(t *testing.T, changes []git.Change, texts ...string) {
	t.Helper()
	rows := buildRows(changes, 80, true)
	for _, text := range texts {
		found := false
		for _, r := range rows {
			if r.text == text {
				found = true
				if len(r.syntaxSpans()) == 0 || r.syntaxSpans()[0].Color != "\x1b[32m" {
					t.Errorf("%q not highlighted as string: %#v", text, r.syntaxSpans())
				}
			}
		}
		if !found {
			t.Errorf("missing row %q", text)
		}
	}
}

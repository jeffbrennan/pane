package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestSyntaxUsesFullOldAndNewContext(t *testing.T) {
	r := testRepo(t)
	prefix := "package main\n/*\n" + strings.Repeat("comment\n", 8)
	writeFile(t, r, "file.go", prefix+"old text\n*/\n")
	if err := r.stage("file.go", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.go", prefix+"staged text\n*/\n")
	if err := r.stage("file.go", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.go", prefix+"working text\n*/\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	assertCommentRows(t, s.staged, "old text", "staged text")
	assertCommentRows(t, s.unstaged, "staged text", "working text")
	if _, err := r.action("commit", "-m", "change"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertCommentRows(t, changes, "old text", "staged text")
}

func TestSyntaxDiffBackgrounds(t *testing.T) {
	diff := "@@ -1,2 +1,2 @@\n package main\n-var old = 1\n+var current = 2\n"
	rows := buildRows([]change{{path: "file.go", diff: diff, syntaxDiff: diff}}, 80, true)
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
			background = "\x1b[48;2;34;40;35m"
		case '-':
			background = "\x1b[48;2;43;34;36m"
		}
		if background == "" {
			if strings.Contains(rendered, "\x1b[48;2;43;34;36m") || strings.Contains(rendered, "\x1b[48;2;34;40;35m") {
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

func assertCommentRows(t *testing.T, changes []change, texts ...string) {
	t.Helper()
	rows := buildRows(changes, 80, true)
	for _, text := range texts {
		found := false
		for _, r := range rows {
			if r.text == text {
				found = true
				if len(r.syntax) == 0 || r.syntax[0].color != "\x1b[90m" {
					t.Errorf("%q not highlighted as comment: %#v", text, r.syntax)
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
	c := change{path: "file.go", diff: diff, syntaxDiff: diff}
	rows := buildRows([]change{c}, 20, true)
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
	rows = buildRows([]change{c}, 80, false)
	m := model{width: 25, horizontal: 5, search: searchState{query: "example"}}
	rendered := m.renderDiffRow(rows[1], false)
	if !strings.Contains(rendered, "\x1b[7mexample") || !strings.Contains(rendered, "\x1b[27m") || !strings.Contains(rendered, "\x1b[34m") {
		t.Fatalf("missing scrolled search or syntax highlighting: %q", rendered)
	}
}

func TestSyntaxFallbackForMissingContextAndOtherLanguages(t *testing.T) {
	diff := "@@ -5 +5 @@\n-old\n+new\n"
	for _, c := range []change{{path: "file.go", diff: diff, syntaxDiff: diff}, {path: "file.py", diff: diff, syntaxDiff: diff}} {
		for _, r := range buildRows([]change{c}, 80, true) {
			if r.syntax != nil {
				t.Fatalf("unexpected syntax for %s: %#v", c.path, r)
			}
		}
	}
}

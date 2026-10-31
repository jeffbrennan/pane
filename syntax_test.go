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
	for _, c := range []change{{path: "file.go", diff: diff, syntaxDiff: diff}, {path: "file.py", diff: diff, syntaxDiff: diff}, {path: "file.txt", diff: "@@ -0,0 +1 @@\n+plain text\n", syntaxDiff: "@@ -0,0 +1 @@\n+plain text\n"}} {
		for _, r := range buildRows([]change{c}, 80, true) {
			if r.syntax != nil {
				t.Fatalf("unexpected syntax for %s: %#v", c.path, r)
			}
		}
	}
}

func TestDataEngineeringSyntax(t *testing.T) {
	tests := []struct {
		path, lexer, text string
	}{
		{"query.sql", "SQL", "SELECT 42 FROM events;"},
		{"main.rs", "Rust", "fn main() {}"},
		{"config.json", "JSON", "{\"name\": \"value\"}"},
		{"events.jsonl", "JSON", "{\"name\": \"value\"}"},
		{"events.ndjson", "JSON", "{\"name\": \"value\"}"},
		{"analysis.ipynb", "JSON", "{\"name\": \"value\"}"},
		{"schema.avsc", "JSON", "{\"name\": \"value\"}"},
		{"pyproject.toml", "TOML", "name = \"value\""},
		{"config.hcl", "HCL", "name = \"value\""},
		{"main.tf", "Terraform", "variable \"name\" {}"},
		{"prod.tfvars", "Terraform", "name = \"value\""},
		{"pipeline.yaml", "YAML", "name: value"},
		{"pipeline.yml", "YAML", "name: value"},
		{"config.ini", "INI", "name = value"},
		{"config.cfg", "INI", "name = value"},
		{"config.properties", "properties", "name = value"},
		{"run.sh", "Bash", "echo \"hello\""},
		{"job.scala", "Scala", "val x = 42"},
		{"job.java", "Java", "public class Job {}"},
		{"job.kt", "Kotlin", "val x = 42"},
		{"job.R", "R", "x <- 42"},
		{"job.jl", "Julia", "x = 42"},
		{"build.gradle", "Groovy", "def x = 42"},
		{"job.js", "JavaScript", "const x = 42;"},
		{"job.ts", "TypeScript", "const x = 42;"},
		{"config.xml", "XML", "<item>value</item>"},
		{"event.proto", "Protocol Buffer", "message Event {}"},
		{"run.ps1", "PowerShell", "$x = 42"},
		{"Dockerfile", "Docker", "FROM python:3"},
		{"docker/Dockerfile.worker", "Docker", "FROM python:3"},
		{"Makefile", "Makefile", "all: build"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			lexer := syntaxLexer(test.path)
			if lexer == nil || lexer.Config().Name != test.lexer {
				t.Fatalf("wrong lexer for %s: %v", test.path, lexer)
			}
			diff := "@@ -0,0 +1 @@\n+" + test.text + "\n"
			rows := buildRows([]change{{path: test.path, diff: diff, syntaxDiff: diff}}, 80, true)
			if len(rows) < 2 || len(rows[1].syntax) == 0 {
				t.Fatalf("missing highlighting: %#v", rows)
			}
		})
	}
}

func TestSQLMultilineSyntaxAcrossDiffViews(t *testing.T) {
	r := testRepo(t)
	prefix := "/*\n" + strings.Repeat("comment\n", 8)
	writeFile(t, r, "query.sql", prefix+"old text\n*/\nSELECT 1;\n")
	if err := r.stage("query.sql", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "query.sql", prefix+"staged text\n*/\nSELECT 1;\n")
	if err := r.stage("query.sql", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "query.sql", prefix+"working text\n*/\nSELECT 1;\n")
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

func TestPythonSyntaxTokensAndExtensions(t *testing.T) {
	diff := "@@ -0,0 +1,3 @@\n+def greet(name):\n+    return \"hello\"\n+    # comment\n"
	for _, path := range []string{"file.py", "file.pyi", "file.pyw"} {
		rows := buildRows([]change{{path: path, diff: diff, syntaxDiff: diff}}, 80, true)
		for _, test := range []struct {
			row   int
			text  string
			color string
		}{{1, "def", "\x1b[35m"}, {1, "greet", "\x1b[34m"}, {2, "hello", "\x1b[32m"}, {3, "# comment", "\x1b[90m"}} {
			found := false
			for _, span := range rows[test.row].syntax {
				if span.color == test.color && strings.Contains(rows[test.row].text[span.start:span.end], test.text) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: missing highlighted %q in %#v", path, test.text, rows[test.row].syntax)
			}
		}
	}
}

func TestPythonMultilineSyntaxAcrossDiffViews(t *testing.T) {
	r := testRepo(t)
	prefix := "message = \"\"\"\n" + strings.Repeat("text\n", 8)
	writeFile(t, r, "file.py", prefix+"old text\n\"\"\"\n")
	if err := r.stage("file.py", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.py", prefix+"staged text\n\"\"\"\n")
	if err := r.stage("file.py", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.py", prefix+"working text\n\"\"\"\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	assertStringRows(t, s.staged, "old text", "staged text")
	assertStringRows(t, s.unstaged, "staged text", "working text")
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
	assertStringRows(t, changes, "old text", "staged text")
}

func TestUntrackedPythonSyntax(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new.py", "def example():\n    return 42\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	rows := buildRows(s.unstaged, 80, true)
	if len(rows) < 2 || len(rows[1].syntax) == 0 || rows[1].syntax[0].color != "\x1b[35m" {
		t.Fatalf("untracked Python file missing keyword highlighting: %#v", rows)
	}
}

func assertStringRows(t *testing.T, changes []change, texts ...string) {
	t.Helper()
	rows := buildRows(changes, 80, true)
	for _, text := range texts {
		found := false
		for _, r := range rows {
			if r.text == text {
				found = true
				if len(r.syntax) == 0 || r.syntax[0].color != "\x1b[32m" {
					t.Errorf("%q not highlighted as string: %#v", text, r.syntax)
				}
			}
		}
		if !found {
			t.Errorf("missing row %q", text)
		}
	}
}

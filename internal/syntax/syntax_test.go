package syntax

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLexersForDataEngineeringFormats(t *testing.T) {
	tests := []struct {
		path, lexer string
	}{
		{"query.sql", "SQL"},
		{"main.rs", "Rust"},
		{"config.json", "JSON"},
		{"events.jsonl", "JSON"},
		{"events.ndjson", "JSON"},
		{"analysis.ipynb", "JSON"},
		{"schema.avsc", "JSON"},
		{"pyproject.toml", "TOML"},
		{"config.hcl", "HCL"},
		{"main.tf", "Terraform"},
		{"prod.tfvars", "Terraform"},
		{"pipeline.yaml", "YAML"},
		{"pipeline.yml", "YAML"},
		{"config.ini", "INI"},
		{"config.cfg", "INI"},
		{"config.properties", "properties"},
		{"run.sh", "Bash"},
		{"job.scala", "Scala"},
		{"job.java", "Java"},
		{"job.kt", "Kotlin"},
		{"job.R", "R"},
		{"job.jl", "Julia"},
		{"build.gradle", "Groovy"},
		{"job.js", "JavaScript"},
		{"job.ts", "TypeScript"},
		{"config.xml", "XML"},
		{"event.proto", "Protocol Buffer"},
		{"run.ps1", "PowerShell"},
		{"Dockerfile", "Docker"},
		{"docker/Dockerfile.worker", "Docker"},
		{"Makefile", "Makefile"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			lexer := Lexer(test.path)
			if lexer == nil || lexer.Config().Name != test.lexer {
				t.Fatalf("wrong lexer for %s: %v", test.path, lexer)
			}
		})
	}
}

func TestLinesHighlightTokenTypes(t *testing.T) {
	diff := "@@ -0,0 +1,3 @@\n+def greet(name):\n+    return \"hello\"\n+    # comment\n"
	_, lines := Lines("file.py", diff)
	for _, test := range []struct {
		line  int
		text  string
		color string
	}{
		{1, "def", "\x1b[35m"},
		{1, "greet", "\x1b[34m"},
		{2, "hello", "\x1b[32m"},
		{3, "# comment", "\x1b[90m"},
	} {
		found := false
		entry := lines[test.line]
		for _, span := range entry.Spans {
			if span.Color == test.color && strings.Contains(entry.Text[span.Start:span.End], test.text) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing highlighted %q on line %d: %#v", test.text, test.line, entry.Spans)
		}
	}
}

func TestLinesForWindowMatchesWholeFile(t *testing.T) {
	const total = 200
	source := make([]string, total)
	for i := range source {
		if i%7 == 0 {
			source[i] = fmt.Sprintf("var value%d = %d // comment", i, i)
		} else {
			source[i] = fmt.Sprintf("var value%d = %d", i, i)
		}
	}
	full := fmt.Sprintf("@@ -1,%d +1,%d @@\n", total, total)
	for i, line := range source {
		if i == 104 {
			full += "-" + line + " old\n+" + line + " new\n"
			continue
		}
		full += " " + line + "\n"
	}
	display := "@@ -100,12 +100,12 @@\n"
	for i := 99; i < 111; i++ {
		if i == 104 {
			display += "-" + source[i] + " old\n+" + source[i] + " new\n"
			continue
		}
		display += " " + source[i] + "\n"
	}
	path := "file.go"
	ResetCache()
	windowOld, windowNew := LinesFor(path, full, display)
	if len(windowOld) == 0 || len(windowNew) == 0 {
		t.Fatal("no displayed lines")
	}
	fullOld, fullNew := LinesFor(path, full, "")
	for _, side := range []struct {
		full, window map[int]Line
	}{{fullOld, windowOld}, {fullNew, windowNew}} {
		for n := range side.window {
			if !reflect.DeepEqual(side.full[n], side.window[n]) {
				t.Errorf("line %d: windowed %+v, whole file %+v", n, side.window[n], side.full[n])
			}
		}
	}
	if len(windowNew) >= len(fullNew) {
		t.Errorf("windowed tokenized %d lines, whole file %d", len(windowNew), len(fullNew))
	}
}

func TestLinesSkipUnlexablePaths(t *testing.T) {
	diff := "@@ -0,0 +1 @@\n+plain text\n"
	if old, new := Lines("file.unknown", diff); old != nil || new != nil {
		t.Fatalf("unexpected syntax for unknown lexer: %#v %#v", old, new)
	}
}

func TestCleanDropsVariationSelectors(t *testing.T) {
	if got := Clean("Darwin ❤️❤️"); got != "Darwin ❤❤" {
		t.Fatalf("Clean = %q", got)
	}
}

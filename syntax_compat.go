package main

import (
	"github.com/jeffbrennan/pane/internal/syntax"
)

type syntaxSpan = syntax.Span

func matchHunkHeader(line string) []string { return syntax.MatchHunkHeader(line) }

func syntaxLines(path, diff string) (map[int]syntax.Line, map[int]syntax.Line) {
	return syntax.Lines(path, diff)
}

func syntaxLinesFor(path, diff, display string) (map[int]syntax.Line, map[int]syntax.Line) {
	return syntax.LinesFor(path, diff, display)
}

func clipSyntax(spans []syntaxSpan, start, end int) []syntaxSpan {
	return syntax.Clip(spans, start, end)
}

func sliceStart(text string, start, width int) int { return syntax.SliceStart(text, start, width) }

func renderSyntax(text string, spans, inline []syntaxSpan, start int, query, background string, light bool) string {
	return syntax.Render(text, spans, inline, start, query, background, light)
}

func renderSyntaxWithHighlight(text string, spans, inline []syntaxSpan, start int, query, background string, light bool, highlight string) string {
	return syntax.RenderWithHighlight(text, spans, inline, start, query, background, light, highlight)
}

func inlineChanges(lines []string) map[int][]syntaxSpan { return syntax.InlineChanges(lines) }

func changedSpans(old, new string) ([]syntaxSpan, []syntaxSpan) {
	return syntax.ChangedSpans(old, new)
}

func renamePathSpans(old, new string, shortened bool) ([]syntaxSpan, []syntaxSpan) {
	return syntax.RenamePathSpans(old, new, shortened)
}

func searchFold(s, query string) string { return syntax.Fold(s, query) }

func clean(s string) string { return syntax.Clean(s) }

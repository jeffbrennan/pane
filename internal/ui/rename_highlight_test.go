package ui

import (
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
	"strings"
	"testing"
)

func highlightedText(text string, spans []syntax.Span) string {
	var result strings.Builder
	for _, span := range spans {
		if span.Start >= 0 && span.End <= len(text) && span.Start < span.End {
			result.WriteString(text[span.Start:span.End])
		}
	}
	return result.String()
}

func TestRenamePathHighlights(t *testing.T) {
	old, new := "src/old-name/file.go", "src/new-name/file.go"
	removed, added := syntax.RenamePathSpans(old, new, false)
	if got := highlightedText(old, removed); got != "old" {
		t.Fatalf("old highlighted %q", got)
	}
	if got := highlightedText(new, added); got != "new" {
		t.Fatalf("new highlighted %q", got)
	}
	removed, added = syntax.RenamePathSpans(old, new, true)
	if got := highlightedText(displayPath(old, true), removed); got != "old" {
		t.Fatalf("short old highlighted %q", got)
	}
	if got := highlightedText(displayPath(new, true), added); got != "new" {
		t.Fatalf("short new highlighted %q", got)
	}
}

func TestRenameHighlightMarksHiddenDifferences(t *testing.T) {
	old, new := "one/shared/file.go", "two/shared/file.go"
	removed, added := syntax.RenamePathSpans(old, new, true)
	if got := highlightedText(displayPath(old, true), removed); got != "…" {
		t.Fatalf("old hidden difference: %q", got)
	}
	if got := highlightedText(displayPath(new, true), added); got != "…" {
		t.Fatalf("new hidden difference: %q", got)
	}
}

func TestRenameHighlightWrapAndRendering(t *testing.T) {
	c := git.Change{OldPath: "prefix/old-name/файл.go", Path: "prefix/new-name/файл.go", Diff: "similarity index 100%\n"}
	m := model{width: 16, height: 20, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{c}}}
	m.rebuild()
	var removed, added strings.Builder
	newPath := false
	for _, r := range m.rows {
		if r.kind != 'f' && r.kind != 'F' {
			continue
		}
		if strings.Contains(r.text, "→") {
			newPath = true
		}
		if newPath {
			added.WriteString(highlightedText(r.text, r.inlineSpans()))
		} else {
			removed.WriteString(highlightedText(r.text, r.inlineSpans()))
		}
	}
	if removed.String() != "old" || added.String() != "new" {
		t.Fatalf("wrapped highlights: %q, %q", removed.String(), added.String())
	}
	for _, light := range []bool{false, true} {
		m.lightTheme = light
		removedColor, addedColor := "\x1b[48;2;100;38;43m", "\x1b[48;2;42;83;52m"
		if light {
			removedColor, addedColor = "\x1b[48;2;239;182;182m", "\x1b[48;2;175;220;181m"
		}
		var oldView, newView strings.Builder
		for _, r := range m.rows {
			if r.renameAdded {
				newView.WriteString(m.renderDiffRow(r, false))
			} else if r.kind == 'f' || r.kind == 'F' {
				oldView.WriteString(m.renderDiffRow(r, false))
			}
		}
		if !strings.Contains(oldView.String(), removedColor) || !strings.Contains(newView.String(), addedColor) {
			t.Fatalf("wrong highlights in light=%v: old=%q new=%q", light, oldView.String(), newView.String())
		}
	}
	if pinned := m.fileHeaderAt(len(m.rows) - 1); pinned == nil || !pinned.renameAdded || highlightedText(pinned.text, pinned.inlineSpans()) != "new" {
		t.Fatalf("pinned highlight: %#v", pinned)
	}
}

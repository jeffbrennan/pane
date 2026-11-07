package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// Every rendered row has to stay within the terminal. Headers, details and
// commit metadata carry a relative-number gutter that used to be forgotten
// when fitting their body, so they spilled four cells past the right edge.
func TestRenderedRowsFitTerminalWidth(t *testing.T) {
	newRows := func() []row {
		rows := []row{
			{kind: 'f', path: "some/really/long/file/name/that/is/wide.go", text: " some/really/long/file/name/that/is/wide.go"},
			{kind: 'm', text: "  Author Name <author@example.com> · 2h ago · a long metadata row"},
			{kind: 'M', text: "  a long commit message row that would previously spill past the edge"},
			{kind: 'n', text: "  a note row that is also longer than the terminal"},
			{kind: '@', text: "  @@ -100,3 +100,3 @@ a long hunk heading"},
			{kind: ' ', text: "a plain context line that is long enough to fill the width"},
			{kind: '+', text: "an added line that is long enough to fill the width"},
			{kind: '-', text: "a removed line that is long enough to fill the width"},
			{kind: 'd', text: "a deleted-file line that is long enough to fill the width"},
			{kind: 'F', path: "some/really/long/file/name/that/is/wide.go", text: " continued header row", offset: 1},
		}
		rename := row{kind: 'f', path: "new/path/file.go", text: " old/path/file.go"}
		rename.setOldPath("old/path/file.go")
		rename.setInlineSpans([]syntax.Span{{Start: 1, End: 4}})
		generated := row{kind: 'f', path: "generated/file.go", text: " generated/file.go", generated: true}
		generated.setGeneratedCounts(12000, 3)
		moved := row{kind: '+', text: "a moved line that is long enough to fill the width"}
		moved.setMoved()
		return append(rows, rename, generated, moved)
	}

	for _, width := range []int{20, 40, 60, 100} {
		for _, wrap := range []bool{true, false} {
			rows := newRows()
			m := model{width: width, height: 20, wrap: wrap, rows: rows, pos: 1}
			for i := range rows {
				rendered := m.renderNumberedDiffRow(rows[i], i)
				if got := ansi.StringWidth(rendered); got > width {
					t.Errorf("width %d wrap %v row %d (kind %c): %d cells: %q", width, wrap, i, rows[i].kind, got, rendered)
				}
				if rows[i].kind == 'f' || rows[i].kind == 'F' {
					pinned := m.renderNumberedDiffRow(rows[i], -1)
					if got := ansi.StringWidth(pinned); got > width {
						t.Errorf("width %d wrap %v pinned row %d (kind %c): %d cells: %q", width, wrap, i, rows[i].kind, got, pinned)
					}
				}
			}
		}
	}
}

// An active search must not strip the color from non-code rows, and moved
// lines must still reverse-video their matches.
func TestSearchPreservesRowColorsAndMovedHighlight(t *testing.T) {
	rows := []row{
		{kind: 'f', path: "some/file.go", text: " some/file.go"},
		{kind: '@', text: "  @@ -1,2 +1,2 @@ needle"},
		{kind: 'M', text: "  details needle"},
		{kind: 'n', text: "  note needle"},
		{kind: ' ', text: "context needle"},
	}
	moved := row{kind: '+', text: "moved needle line"}
	moved.setMoved()
	rows = append(rows, moved)

	m := model{width: 80, height: 20, wrap: true, rows: rows, pos: 1}
	m.search.query = "needle"

	header := m.renderNumberedDiffRow(rows[0], 0)
	if !strings.Contains(header, renameHeaderDark) {
		t.Errorf("search dropped the header color: %q", header)
	}
	hunk := m.renderNumberedDiffRow(rows[1], 1)
	if !strings.Contains(hunk, "\x1b[36m") || !strings.Contains(hunk, "\x1b[7m") {
		t.Errorf("search dropped hunk color or match highlight: %q", hunk)
	}
	details := m.renderNumberedDiffRow(rows[2], 2)
	if !strings.Contains(details, styleDim) {
		t.Errorf("search dropped the details color: %q", details)
	}
	context := m.renderNumberedDiffRow(rows[4], 4)
	if !strings.Contains(context, "\x1b[7m") {
		t.Errorf("search dropped the context match highlight: %q", context)
	}
	movedLine := m.renderNumberedDiffRow(rows[5], 5)
	if !strings.Contains(movedLine, styleDim) || !strings.Contains(movedLine, "\x1b[7m") {
		t.Errorf("moved row lost its dim color or match highlight: %q", movedLine)
	}
}

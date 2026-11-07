package ui

import (
	"github.com/jeffbrennan/pane/internal/syntax"
)

type row struct {
	path, text string
	spans      *rowSpans
	// Line numbers stay native ints so very large source lines are preserved.
	target, sourceLine int
	// Offsets and deleted-line counts are small in practice; keeping them
	// compact lets the four int32 fields pack without padding.
	offset, deletedLines   int32
	lineIndex, headerIndex int32
	commitIndex            uint16
	renameAdded, submodule bool
	generated, viewed      bool
	// A highlighted line can have no spans but still use syntax-aware rendering.
	syntaxPresent bool
	kind          byte
}

type rowSpans struct {
	syntax, inline []syntax.Span
	// Rare indexes that do not fit the compact fields share this optional storage.
	wide *wideRowIndexes
}

type wideRowIndexes struct {
	line, header     int
	generatedAdded   int
	generatedDeleted int
	oldPath          string
	moved            bool
}

// The sentinel preserves full native-int values, including MinInt32 itself.
const wideRowIndex int32 = -1 << 31

func newRowSpans(syntax, inline []syntax.Span, wide *wideRowIndexes) *rowSpans {
	if len(syntax) == 0 && len(inline) == 0 && wide == nil {
		return nil
	}
	return &rowSpans{syntax: syntax, inline: inline, wide: wide}
}

func (r row) lineNumber() int {
	if r.lineIndex == wideRowIndex {
		return r.spans.wide.line
	}
	return int(r.lineIndex)
}

func (r row) headerRow() int {
	if r.headerIndex == wideRowIndex {
		return r.spans.wide.header
	}
	return int(r.headerIndex)
}

func (r row) wideIndexes() *wideRowIndexes {
	if r.spans == nil {
		return nil
	}
	return r.spans.wide
}

// oldPathValue is only set on file headers for renames, so it lives in the
// optional wide storage rather than costing every content row a string header.
func (r row) oldPathValue() string {
	if wide := r.wideIndexes(); wide != nil {
		return wide.oldPath
	}
	return ""
}

func (r *row) setOldPath(value string) {
	if value == "" {
		return
	}
	wide := wideRowIndexes{}
	if old := r.wideIndexes(); old != nil {
		wide = *old
	}
	wide.oldPath = value
	r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), &wide)
}

func (r row) generatedCounts() (int, int) {
	if r.spans == nil || r.spans.wide == nil {
		return 0, 0
	}
	return r.spans.wide.generatedAdded, r.spans.wide.generatedDeleted
}

func (r *row) setGeneratedCounts(added, deleted int) {
	wide := wideRowIndexes{}
	if old := r.wideIndexes(); old != nil {
		wide = *old
	}
	wide.generatedAdded, wide.generatedDeleted = added, deleted
	r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), &wide)
}

func (r row) isMoved() bool {
	return r.spans != nil && r.spans.wide != nil && r.spans.wide.moved
}

func (r *row) setMoved() {
	wide := wideRowIndexes{}
	if old := r.wideIndexes(); old != nil {
		wide = *old
	}
	wide.moved = true
	r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), &wide)
}

func (r *row) setLineNumber(value int) {
	compact := int32(value)
	if int(compact) == value && compact != wideRowIndex {
		wasWide := r.lineIndex == wideRowIndex
		r.lineIndex = compact
		if wasWide && r.headerIndex != wideRowIndex {
			r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), nil)
		}
		return
	}
	wide := wideRowIndexes{}
	if old := r.wideIndexes(); old != nil {
		wide = *old
	}
	wide.line = value
	r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), &wide)
	r.lineIndex = wideRowIndex
}

func (r row) withLineNumber(value int) row {
	r.setLineNumber(value)
	return r
}

func (r *row) setHeaderRow(value int) {
	compact := int32(value)
	if int(compact) == value && compact != wideRowIndex {
		wasWide := r.headerIndex == wideRowIndex
		r.headerIndex = compact
		if wasWide && r.lineIndex != wideRowIndex {
			r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), nil)
		}
		return
	}
	wide := wideRowIndexes{}
	if old := r.wideIndexes(); old != nil {
		wide = *old
	}
	wide.header = value
	r.spans = newRowSpans(r.syntaxSpans(), r.inlineSpans(), &wide)
	r.headerIndex = wideRowIndex
}

func (r row) syntaxSpans() []syntax.Span {
	if !r.syntaxPresent {
		return nil
	}
	if r.spans == nil || r.spans.syntax == nil {
		return []syntax.Span{}
	}
	return r.spans.syntax
}

func (r row) inlineSpans() []syntax.Span {
	if r.spans == nil {
		return nil
	}
	return r.spans.inline
}

func (r *row) setSyntaxSpans(spans []syntax.Span) {
	r.syntaxPresent = spans != nil
	// Rows are copied into caches, so replacing the span object avoids mutating copies.
	r.spans = newRowSpans(spans, r.inlineSpans(), r.wideIndexes())
}

func (r *row) setInlineSpans(spans []syntax.Span) {
	r.spans = newRowSpans(r.syntaxSpans(), spans, r.wideIndexes())
}

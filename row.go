package main

type row struct {
	path, oldPath, text              string
	spans                            *rowSpans
	line, offset, target, sourceLine int
	deletedLines, headerIndex        int
	commitIndex                      uint16
	renameAdded, submodule           bool
	// A highlighted line can have no spans but still use syntax-aware rendering.
	syntaxPresent bool
	kind          byte
}

type rowSpans struct {
	syntax, inline []syntaxSpan
}

func newRowSpans(syntax, inline []syntaxSpan) *rowSpans {
	if len(syntax) == 0 && len(inline) == 0 {
		return nil
	}
	return &rowSpans{syntax: syntax, inline: inline}
}

func (r row) syntaxSpans() []syntaxSpan {
	if !r.syntaxPresent {
		return nil
	}
	if r.spans == nil || r.spans.syntax == nil {
		return []syntaxSpan{}
	}
	return r.spans.syntax
}

func (r row) inlineSpans() []syntaxSpan {
	if r.spans == nil {
		return nil
	}
	return r.spans.inline
}

func (r *row) setSyntaxSpans(spans []syntaxSpan) {
	r.syntaxPresent = spans != nil
	// Rows are copied into caches, so replacing the span object avoids mutating copies.
	r.spans = newRowSpans(spans, r.inlineSpans())
}

func (r *row) setInlineSpans(spans []syntaxSpan) {
	r.spans = newRowSpans(r.syntaxSpans(), spans)
}

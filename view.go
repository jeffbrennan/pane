package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func clean(s string) string {
	if printableASCII(s) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

func wrapLine(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	if width > 0 && printableASCII(s) {
		if len(s) <= width {
			return []string{s}
		}
		parts := make([]string, 0, (len(s)+width-1)/width)
		for len(s) > width {
			parts = append(parts, s[:width])
			s = s[width:]
		}
		return append(parts, s)
	}
	var parts []string
	start, w := 0, 0
	for index, r := range s {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[index:]); size == 1 {
				// Normalize malformed bytes before slicing so each renders as a replacement rune.
				return wrapLine(strings.Map(func(r rune) rune { return r }, s), width)
			}
		}
		rw := runewidth.RuneWidth(r)
		if w > 0 && w+rw > width {
			parts = append(parts, s[start:index])
			start = index
			w = 0
		}
		w += rw
	}
	return append(parts, s[start:])
}

func (m model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.modal == "error" || m.modal == "discard" || m.modal == "stash-confirm" || m.modal == "branch-delete" || m.modal == "worktree-delete" {
		return m.confirmationView()
	}
	if m.screen == "history" {
		return m.historyView()
	}
	if m.screen == "branches" {
		return m.branchView()
	}
	if m.screen == "worktrees" {
		return m.worktreeView()
	}
	if m.screen == "hotspots" {
		return m.hotView()
	}
	if m.screen == "stashes" {
		return m.stashView()
	}
	if m.screen == "commit" || m.screen == "base" {
		return m.commitView()
	}
	if m.screen == "sequence" {
		return m.sequenceView()
	}
	title := " " + m.viewTitle
	if m.viewTitle == "" {
		title = " " + m.stageTabs()
	}
	var lines []string
	lines = append(lines, m.titleBar(title, m.viewBadge))
	available := max(0, m.height-2)
	start, visible, pinned := m.diffViewport(available)
	if pinned != nil {
		// Overlay the top row so pinning and unpinning never shifts the rows below it.
		if m.pos == start {
			pinned = nil
			visible++
		} else {
			start++
		}
	}
	if pinned != nil {
		lines = append(lines, m.renderNumberedDiffRow(*pinned, -1))
	}
	extra := m.movedHintCount(start, start+visible)
	rowLimit := max(1, visible-extra)
	if m.pos >= start && m.pos >= start+rowLimit && len(m.rows) > 0 {
		// Keep the cursor on screen when moved hints consume viewport lines.
		start = max(0, min(m.pos-rowLimit+1, start))
		extra = m.movedHintCount(start, start+visible)
		rowLimit = max(1, visible-extra)
	}
	rendered, renderedRows := 0, 0
	for index := start; index < len(m.rows) && rendered < visible && renderedRows < rowLimit; index++ {
		r := m.rows[index]
		moved, hint, show := m.movedFor(r)
		if moved {
			r.setMoved()
		}
		renderedRows++
		if moved && show && rendered+1 < visible && !m.nextRowContinues(index) {
			lines = append(lines, m.renderNumberedDiffRow(r, index), m.movedHintRow(hint))
			rendered += 2
			continue
		}
		lines = append(lines, m.renderNumberedDiffRow(r, index))
		rendered++
	}
	for rendered < visible {
		index := start + renderedRows
		switch {
		case index == 0 && len(m.rows) == 0:
			lines = append(lines, fit(m.emptyText(), m.width))
		case index >= len(m.rows) && len(m.rows) > 0 && m.contextCommit != "" && m.history.streamActive:
			lines = append(lines, styleDim+"  Loading…"+styleReset)
		default:
			lines = append(lines, "")
		}
		rendered++
	}
	status := clean(strings.ReplaceAll(m.message, "\n", " | "))
	if m.flash != "" {
		status = clean(strings.ReplaceAll(m.flash, "\n", " | "))
	}
	if m.repo.ignoreSpace && m.modal == "" {
		badge := styleDim + "ignoring whitespace" + styleReset
		if status != "" {
			status += "  "
		}
		status += badge
	}
	hints := m.viewHints
	if hints == "" {
		hints = keyHints("?", "help")
	}
	switch m.modal {
	case "search":
		prefix := "/"
		if m.search.draftDirection < 0 {
			prefix = "?"
		}
		status, hints = prefix+renderInput(m.search.draft, m.search.cursor, max(1, m.width-1)), keyHints("↵", "search", "esc", "cancel")
	case "note":
		status, hints = "Note: "+renderInput(m.input, m.inputCursor, max(1, m.width-8)), keyHints("↵", "save", "esc", "cancel")
	case "branch-new":
		status, hints = "New branch from "+shortHash(m.branch.newFrom)+": "+renderInput(m.input, m.inputCursor, max(1, m.width-32)), keyHints("↵", "create", "esc", "cancel")
	case "worktree-new":
		status, hints = "New worktree (path  branch): "+renderInput(m.input, m.inputCursor, max(1, m.width-34)), keyHints("↵", "create", "esc", "cancel")
	case "commit":
		status, hints = "", ""
		if m.width < 24 || m.height < 9 {
			mode := "INSERT"
			if m.inputNormal {
				mode = "NORMAL"
			}
			status = prompt("Commit message ["+mode+"]: ", m.input)
		}
	case "help", "files":
		status, hints = "", ""
	}
	if m.searchProgress != "" && m.activeSearch() != "" && (m.modal == "" || m.modal == "search") {
		if m.modal == "" && m.flash == "" {
			status = "/" + clean(m.search.query)
		}
		status += styleDim + "  · " + m.searchProgress + styleReset
	}
	lines = append(lines, m.statusBar(status, hints))
	if m.modal == "commit" || m.modal == "help" || m.modal == "files" {
		clearBody(lines)
	}
	if m.modal == "commit" && m.width >= 24 && m.height >= 9 {
		m.showCommitDialog(lines)
	}
	if m.modal == "help" {
		m.showHelpDialog(lines)
	}
	if m.modal == "files" {
		m.showFilesDialog(lines)
	}
	return strings.Join(lines, "\n")
}

// confirmationView shows an error or discard confirmation over the current
// screen's title and status bars.
func (m model) confirmationView() string {
	under := m
	under.modal = ""
	lines := strings.Split(under.View(), "\n")
	small := m.width < 24 || m.height < 8
	if m.modal == "discard" && small {
		return m.discardView()
	}
	if m.modal == "stash-confirm" && small {
		return strings.Join([]string{fit(" "+strings.ToUpper(m.stash.operation)+" "+m.stash.target.ref+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "branch-delete" && small {
		return strings.Join([]string{fit(" Delete branch "+m.branch.confirm.name+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "worktree-delete" && small {
		return strings.Join([]string{fit(" Remove worktree "+m.worktrees.confirm.path+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "error" && small {
		content := m.errorLines(m.width)
		start := min(m.errorPos, max(0, len(content)-1))
		lines := []string{fit(" Git error", m.width)}
		for i := 0; i < max(0, m.height-2); i++ {
			if start+i < len(content) {
				lines = append(lines, content[start+i])
			} else {
				lines = append(lines, "")
			}
		}
		lines = append(lines, fit("j/k scroll  enter/esc dismiss", m.width))
		return strings.Join(lines, "\n")
	}
	clearBody(lines)
	lines[len(lines)-1] = ""
	if m.modal == "error" {
		m.showErrorDialog(lines)
	} else if m.modal == "stash-confirm" {
		m.showStashConfirm(lines)
	} else if m.modal == "branch-delete" {
		m.showBranchDelete(lines)
	} else if m.modal == "worktree-delete" {
		m.showWorktreeDelete(lines)
	} else {
		m.showDiscardDialog(lines)
	}
	return strings.Join(lines, "\n")
}

// stageTabs shows the unstaged and staged views with the active one
// emphasized, so tab's effect is visible before pressing it.
func (m model) stageTabs() string {
	tab := func(name string, count int, active bool) string {
		if active {
			return styleBold + name + " " + fmt.Sprint(count) + styleReset
		}
		return styleDim + name + " " + fmt.Sprint(count) + styleReset
	}
	return tab("unstaged", len(m.snapshot.unstaged), !m.staged) + styleDim + " · " + styleReset + tab("staged", len(m.snapshot.staged), m.staged)
}

func (m model) emptyText() string {
	if m.relativeLines && m.history.loading {
		if m.history.commitLoadingVisible {
			return " Loading commit…"
		}
		return ""
	}
	if m.viewTitle != "" {
		return " No changes"
	}
	if m.loading && m.startup {
		return ""
	}
	if m.staged {
		if n := len(m.snapshot.unstaged); n > 0 {
			return fmt.Sprintf(" No staged changes · tab shows %d unstaged", n)
		}
		return " No staged changes"
	}
	if n := len(m.snapshot.staged); n > 0 {
		return fmt.Sprintf(" No unstaged changes · tab shows %d staged", n)
	}
	return " No changes · H opens history"
}

func wrapFlash(wrap bool) string {
	if wrap {
		return "Line wrapping on"
	}
	return "Line wrapping off"
}

func (m model) relativeGutterWidth() int {
	if !m.relativeLines && m.screen != "sequence" {
		return 0
	}
	return max(3, len(fmt.Sprint(max(0, m.height-2))))
}

func (m model) diffContentWidth() int {
	extra := max(0, m.relativeGutterWidth()-3)
	return max(1, m.width-diffMargin(m.width)-extra)
}

// lineDistance counts whole lines between row index and the active row,
// matching how j and k move.
func (m model) lineDistance(index int) int {
	active := m.lineStart(m.pos)
	from, to := min(index, active), max(index, active)
	distance := 0
	for i := from + 1; i <= to; i++ {
		if m.rows[i].offset == 0 {
			distance++
		}
	}
	return distance
}

func (m model) renderNumberedDiffRow(r row, index int) string {
	if index >= 0 && len(r.inlineSpans()) == 0 {
		r.setInlineSpans(m.inlineSpansForRow(index))
	}
	gutterWidth := m.rowGutterWidth(r)
	gutter := rowGutter(r, gutterWidth)
	width := m.relativeGutterWidth()
	if width > 0 {
		if gutter != "" {
			width = max(width, len(gutter)-3)
		} else {
			// Line up headers and separators with the line numbers.
			width = max(width, gutterWidth)
		}
		number := strings.Repeat(" ", width)
		if index >= 0 && r.offset == 0 {
			value := m.lineDistance(index)
			if index == m.pos && r.sourceLine > 0 {
				value = r.sourceLine
			}
			number = fmt.Sprintf("%*d", width, value)
		}
		if gutter != "" {
			gutter = number + gutter[len(gutter)-3:]
		} else {
			gutter = number + " "
		}
	}
	return m.renderDiffRowWithGutter(r, index >= 0 && index == m.pos, gutter)
}

func (m model) renderDiffRow(r row, active bool) string {
	return m.renderDiffRowWithGutter(r, active, rowGutter(r, m.rowGutterWidth(r)))
}

func (m model) rowGutterWidth(r row) int {
	if m.screen == "sequence" || m.sequenceGutters {
		if int(r.commitIndex) < len(m.sequence.gutterWidths) {
			return m.sequence.gutterWidths[r.commitIndex]
		}
		return 0
	}
	return m.gutterWidth
}

func rowGutter(r row, width int) string {
	if width == 0 {
		return ""
	}
	kind := r.kind
	if kind == 'd' {
		kind = '-'
	}
	if kind != '+' && kind != '-' && kind != ' ' {
		return ""
	}
	if r.offset > 0 {
		return strings.Repeat(" ", width+3)
	}
	return fmt.Sprintf("%*d %c ", width, r.sourceLine, kind)
}

func (m model) renderDiffRowWithGutter(r row, active bool, gutter string) string {
	margin := diffMargin(m.width)
	text := r.text
	syntax, inline := r.syntaxSpans(), r.inlineSpans()
	contentWidth := max(0, m.width-margin)
	if r.kind == 'f' || r.kind == 'F' || r.kind == 'm' || r.kind == 'M' || r.kind == 'n' {
		text = fit(text, contentWidth)
	} else if !m.wrap {
		text = sliceWidth(text, m.horizontal, max(0, contentWidth-stringWidth(gutter)))
	} else {
		text = fit(text, max(0, contentWidth-stringWidth(gutter)))
	}
	text = fit(gutter+text, contentWidth)
	query := m.activeSearch()
	color := ""
	switch r.kind {
	case '+':
		color = "\x1b[32m"
	case '-':
		color = "\x1b[31m"
	case '@':
		color = "\x1b[36m"
		if r.submodule && (strings.HasPrefix(r.text, "  > ") || strings.HasPrefix(r.text, "  < ")) {
			color = styleDim
		}
	case 'f', 'F':
		if r.kind == 'f' && r.generated && (r.oldPath == "" || r.renameAdded) {
			suffix := generatedLabel(r)
			head := fit(text, max(0, contentWidth-stringWidth(suffix)))
			padding := strings.Repeat(" ", max(0, contentWidth-stringWidth(head)-stringWidth(suffix)))
			text = head + padding + styleDim + suffix + styleReset
		} else {
			text += strings.Repeat(" ", max(0, contentWidth-stringWidth(text)))
		}
		color = renameHeaderDark
		if m.lightTheme {
			color = renameHeaderLight
		}
	case 'M':
		color = styleDim
	case 'n':
		color = styleDim
	}
	if r.isMoved() {
		color = styleDim
	}
	if (r.kind == 'f' || r.kind == 'F') && len(inline) > 0 && strings.HasPrefix(text, gutter) {
		highlight := "\x1b[48;2;100;38;43m"
		if r.renameAdded {
			highlight = "\x1b[48;2;42;83;52m"
		}
		if m.lightTheme {
			highlight = "\x1b[48;2;239;182;182m"
			if r.renameAdded {
				highlight = "\x1b[48;2;175;220;181m"
			}
		}
		text = color + gutter + renderSyntaxWithHighlight(text[len(gutter):], nil, inline, 0, query, color, m.lightTheme, highlight) + "\x1b[0m"
		color = ""
	} else if (syntax != nil || r.kind == '+' || r.kind == '-' || r.kind == 'd') && !r.isMoved() && strings.HasPrefix(text, gutter) {
		visible := text[len(gutter):]
		start := 0
		if !m.wrap {
			start = sliceStart(r.text, m.horizontal, max(0, contentWidth-stringWidth(gutter)))
		}
		background := ""
		switch r.kind {
		case '+':
			background = darkAddedBackground
			if m.lightTheme {
				background = "\x1b[48;2;221;242;224m"
			}
		case '-', 'd':
			background = darkRemovedBackground
			if m.lightTheme {
				background = "\x1b[48;2;250;225;225m"
			}
		}
		padding := ""
		if background != "" {
			padding = strings.Repeat(" ", max(0, contentWidth-stringWidth(gutter+visible)))
		}
		text = background + gutter + renderSyntax(visible, syntax, inline, start, query, background, m.lightTheme) + padding + "\x1b[0m"
		color = ""
	} else if query != "" && strings.HasPrefix(text, gutter) {
		text = gutter + highlightMatches(text[len(gutter):], query)
	}
	if color != "" {
		text = color + text + "\x1b[0m"
	}
	prefix := strings.Repeat(" ", margin)
	if active {
		prefix = styleBold + m.accent() + "▸" + strings.Repeat(" ", margin-1) + styleReset
	}
	return prefix + text
}

func fit(s string, width int) string {
	if printableASCII(s) {
		return s[:max(0, min(len(s), width))]
	}
	return runewidth.Truncate(s, width, "")
}

// printableASCII reports whether every byte of s is a printable ASCII
// character, each of which is one cell wide. Most diff text is, and skipping
// runewidth's grapheme segmentation for it dominates rendering cost.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func stringWidth(s string) int {
	if printableASCII(s) {
		return len(s)
	}
	return runewidth.StringWidth(s)
}

func sliceWidth(s string, start, width int) string {
	var b strings.Builder
	pos := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if pos >= start && pos+w <= start+width {
			b.WriteRune(r)
		}
		pos += w
		if pos >= start+width {
			break
		}
	}
	return b.String()
}

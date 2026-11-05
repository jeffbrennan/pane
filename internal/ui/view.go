package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jeffbrennan/pane/internal/syntax"
	"github.com/mattn/go-runewidth"
)

const (
	renameHeaderDark  = "\x1b[1;37;48;5;236m"
	renameHeaderLight = "\x1b[1;38;2;45;45;45;48;2;222;222;222m"
)

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
	if m.modal == "error" || m.modal == "discard" || m.modal == "stash-confirm" || m.modal == "branch-delete" || m.modal == "worktree-delete" || m.modal == "push-upstream" || m.modal == "pull-issue" || m.modal == "push-rejected" {
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
	badge := m.viewBadge
	if m.viewTitle == "" {
		title = " " + m.stageTabs()
		badge = stateBadges(m.whitespaceBadge())
	}
	var lines []string
	lines = append(lines, m.titleBar(title, badge))
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
	filler := ""
	switch {
	case len(m.rows) == 0:
		filler = fit(m.emptyText(), m.width)
	case start+renderedRows >= len(m.rows) && m.contextCommit != "" && m.history.streamActive:
		filler = styleDim + "  Loading…" + styleReset
	}
	for rendered < visible {
		lines = append(lines, filler)
		filler = ""
		rendered++
	}
	status := syntax.Clean(strings.ReplaceAll(m.message, "\n", " | "))
	if m.flash != "" {
		status = syntax.Clean(strings.ReplaceAll(m.flash, "\n", " | "))
	}
	hints := m.viewHints
	if hints == "" {
		if m.viewTitle == "" {
			hints = footer(false, hint("stage", "changes.stage-file"), hint("hunk", "changes.stage-hunk"), hint("commit", "changes.commit"), hint("history", "changes.history"))
		} else {
			hints = footer(true)
		}
	}
	switch m.modal {
	case "search":
		prefix := "/"
		if m.search.draftDirection < 0 {
			prefix = "?"
		}
		status, hints = prefix+renderInput(m.search.draft, m.search.cursor, max(1, m.width-1)), keyHints("↵", "search", "esc", "cancel")
	case "branch-new":
		status, hints = m.newBranchStatus()
	case "worktree-new":
		status, hints = "New worktree (path  branch): "+renderInput(m.input, m.inputCursor, max(1, m.width-34)), keyHints("↵", "create", "esc", "cancel")
	case "commit":
		status, hints = "", ""
		if m.width < 24 || m.height < 9 {
			status = prompt("Commit message: ", m.input)
		}
	case "help", "files":
		status, hints = "", ""
	}
	if m.searchProgress != "" && m.activeSearch() != "" && (m.modal == "" || m.modal == "search") {
		if m.modal == "" && m.flash == "" {
			status = "/" + syntax.Clean(m.search.query)
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
		return strings.Join([]string{fit(" "+strings.ToUpper(m.stash.operation)+" "+m.stash.target.Ref+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "branch-delete" && small {
		return strings.Join([]string{fit(" Delete branch "+m.branch.confirm.Name+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "worktree-delete" && small {
		return strings.Join([]string{fit(" Remove worktree "+m.worktrees.confirm.Path+"?", m.width), fit(" y confirm  n/esc cancel", m.width)}, "\n")
	}
	if m.modal == "push-upstream" && small {
		title := " Push " + m.push.branch + " to " + m.push.remote + " and track it?"
		hint := " enter push  a always  esc cancel"
		if m.push.remote == "" {
			title = " Push " + m.push.branch + " to which remote?"
			hint = " 1/2 choose  esc cancel"
		}
		return strings.Join([]string{fit(title, m.width), fit(hint, m.width)}, "\n")
	}
	if (m.modal == "pull-issue" || m.modal == "push-rejected") && small {
		r := m.recovery()
		return strings.Join([]string{fit(" "+r.short, m.width), fit(" "+r.plainHints(), m.width)}, "\n")
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
		hint := "j/k scroll  enter/esc dismiss"
		if blockedByChanges(m.errorText) {
			hint = "j/k scroll  enter show changes  esc dismiss"
		}
		lines = append(lines, fit(hint, m.width))
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
	} else if m.modal == "push-upstream" {
		m.showPushUpstream(lines)
	} else if m.modal == "pull-issue" || m.modal == "push-rejected" {
		m.showRecovery(lines, m.recovery())
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
	return tab("unstaged", len(m.snapshot.Unstaged), !m.staged) + styleDim + " · " + styleReset + tab("staged", len(m.snapshot.Staged), m.staged)
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
		if n := len(m.snapshot.Unstaged); n > 0 {
			return fmt.Sprintf(" No staged changes · tab shows %d unstaged", n)
		}
		return " No staged changes"
	}
	if n := len(m.snapshot.Staged); n > 0 {
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

// relativeGutterWidth is the width of the relative line-number column, which
// every diff view shares so counts work the same way everywhere.
func (m model) relativeGutterWidth() int {
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
	inline := r.inlineSpans()
	if index >= 0 && len(inline) == 0 {
		inline = m.inlineSpansForRow(index)
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
			number = formatIntWidth(value, width)
		}
		if r.kind == 'm' || r.kind == 'M' {
			// Details rows are not code, so relative numbers mean nothing there.
			number = strings.Repeat(" ", width)
		}
		if gutter != "" {
			gutter = number + gutter[len(gutter)-3:]
		} else {
			gutter = number + " "
		}
	}
	return m.renderDiffRowWithGutter(r, inline, index >= 0 && index == m.pos, gutter)
}

func (m model) renderDiffRow(r row, active bool) string {
	return m.renderDiffRowWithGutter(r, r.inlineSpans(), active, rowGutter(r, m.rowGutterWidth(r)))
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
	return formatGutter(r.sourceLine, width, kind)
}

// formatIntWidth right-aligns value in width bytes. It avoids fmt on the
// per-row render path, where every visible line formats a relative number.
func formatIntWidth(value, width int) string {
	var buffer [24]byte
	digits := strconv.AppendInt(buffer[:0], int64(value), 10)
	if len(digits) >= width {
		return string(digits)
	}
	if width > 60 {
		return strings.Repeat(" ", width-len(digits)) + string(digits)
	}
	var out [64]byte
	pad := width - len(digits)
	for i := 0; i < pad; i++ {
		out[i] = ' '
	}
	copy(out[pad:], digits)
	return string(out[:width])
}

// formatGutter builds the padded number, space, kind, and trailing space in
// one allocation. Values wider than the column keep their full width, like
// the %*d form it replaces.
func formatGutter(value, width int, kind byte) string {
	var buffer [24]byte
	digits := strconv.AppendInt(buffer[:0], int64(value), 10)
	pad := max(0, width-len(digits))
	if pad+len(digits)+3 > 64 {
		return strings.Repeat(" ", pad) + string(digits) + " " + string(kind) + " "
	}
	var out [64]byte
	for i := 0; i < pad; i++ {
		out[i] = ' '
	}
	copy(out[pad:], digits)
	at := pad + len(digits)
	out[at], out[at+1], out[at+2] = ' ', kind, ' '
	return string(out[:at+3])
}

func (m model) renderDiffRowWithGutter(r row, inline []syntax.Span, active bool, gutter string) string {
	margin := diffMargin(m.width)
	prefix := strings.Repeat(" ", margin)
	if active {
		prefix = styleBold + m.accent() + "▸" + strings.Repeat(" ", margin-1) + styleReset
	}
	body := r.text
	spans := r.syntaxSpans()
	contentWidth := max(0, m.width-margin)
	bodyWidth := max(0, contentWidth-stringWidth(gutter))
	if r.kind == 'f' || r.kind == 'F' || r.kind == 'm' || r.kind == 'M' || r.kind == 'n' {
		body = fit(body, contentWidth)
	} else if !m.wrap {
		body = fit(sliceWidth(body, m.horizontal, bodyWidth), bodyWidth)
	} else {
		body = fit(body, bodyWidth)
	}
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
		if r.kind == 'f' && r.generated && (r.oldPathValue() == "" || r.renameAdded) {
			suffix := generatedLabel(r)
			head := fit(body, max(0, contentWidth-stringWidth(suffix)))
			padding := strings.Repeat(" ", max(0, contentWidth-stringWidth(head)-stringWidth(suffix)))
			body = head + padding + styleDim + suffix + styleReset
		} else {
			body += strings.Repeat(" ", max(0, contentWidth-stringWidth(body)))
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
	text := body
	prefixed := false
	if (r.kind == 'f' || r.kind == 'F') && len(inline) > 0 {
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
		text = prefix + color + gutter + syntax.RenderWithHighlight(body, nil, inline, 0, query, color, m.lightTheme, highlight) + "\x1b[0m"
		prefixed = true
		color = ""
	} else if spans != nil || r.kind == '+' || r.kind == '-' || r.kind == 'd' {
		if !r.isMoved() {
			start := 0
			if !m.wrap {
				start = syntax.SliceStart(r.text, m.horizontal, bodyWidth)
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
				padding = strings.Repeat(" ", max(0, contentWidth-stringWidth(gutter)-stringWidth(body)))
			}
			text = prefix + background + gutter + syntax.Render(body, spans, inline, start, query, background, m.lightTheme) + padding + "\x1b[0m"
			prefixed = true
			color = ""
		}
	} else if query != "" {
		text = prefix + gutter + highlightMatches(body, query)
		prefixed = true
	}
	if !prefixed {
		// Branches that skip syntax rendering still need the gutter on content
		// rows, such as moved lines and plain context.
		if gutter != "" {
			text = gutter + text
		}
		if color != "" {
			text = color + text + "\x1b[0m"
		}
		return prefix + text
	}
	return text
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

package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// Shared screen chrome: every view uses the same title bar, status bar, key
// hints and dialog boxes so moving between screens feels like one app.

const (
	styleReset = "\x1b[0m"
	styleBold  = "\x1b[1m"
	styleDim   = "\x1b[2m"
	cursor     = "\x1b[7m \x1b[0m"
)

func (m model) accent() string {
	if m.lightTheme {
		return "\x1b[38;2;0;105;110m"
	}
	return "\x1b[36m"
}

func (m model) warning() string {
	if m.lightTheme {
		return "\x1b[38;2;150;95;0m"
	}
	return "\x1b[33m"
}

func (m model) danger() string {
	if m.lightTheme {
		return "\x1b[38;2;170;40;40m"
	}
	return "\x1b[31m"
}

// bar lays out left- and right-aligned text across width, dropping the right
// side when both do not fit.
func bar(width int, left, right string) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(left) > width {
		left = ansi.Truncate(left, width, "") + styleReset
	}
	used := ansi.StringWidth(left)
	if right == "" || used+2+ansi.StringWidth(right) > width {
		return left
	}
	return left + strings.Repeat(" ", width-used-ansi.StringWidth(right)) + right
}

// titleBar renders the first row of every screen, with transient state such
// as a running push on the right.
func (m model) titleBar(left, right string) string {
	if action := m.actionStatus(); action != "" {
		right = m.warning() + action + styleReset
	} else if m.busy {
		right = m.warning() + "working…" + styleReset
	}
	return bar(m.width, left, right)
}

// stateBadges joins the non-empty right-hand badges of a title row in the
// shared order: position, viewed, ignoring whitespace, filter, selection.
func stateBadges(parts ...string) string {
	var out []string
	for _, part := range parts {
		if part != "" {
			out = append(out, styleDim+part+styleReset)
		}
	}
	return strings.Join(out, styleDim+" · "+styleReset)
}

func (m model) mergesBadge() string {
	if !m.repo.FirstParent {
		return "merges expanded"
	}
	return ""
}

func (m model) botsBadge() string {
	if !m.showBotCommits {
		return "bots hidden"
	}
	return ""
}

func (m model) whitespaceBadge() string {
	if m.repo.IgnoreSpace {
		return "ignoring whitespace"
	}
	return ""
}

func (m model) filterBadge(filter string) string {
	if filter == "" {
		return ""
	}
	return "filter: " + filter
}

func (m model) selectionBadge() string {
	if !m.history.visualActive {
		return ""
	}
	count := m.history.list.selected - m.history.visualAnchor + 1
	if count < 0 {
		count = -count
	}
	return fmt.Sprintf("selecting %d", count)
}

// pendingIndicator renders the showcmd-style state of a pending motion.
func (m model) pendingIndicator() string {
	if m.pending != "" {
		if m.count > 0 {
			return fmt.Sprintf("%d%s…", m.count, keyLabel(m.pending))
		}
		return keyLabel(m.pending) + "…"
	}
	if m.count > 0 {
		return fmt.Sprintf("%d…", m.count)
	}
	return ""
}

// statusBar renders the last row of every screen: a message or prompt on the
// left and dimmed key hints on the right, with any pending motion beside them.
func (m model) statusBar(status, hints string) string {
	if indicator := m.pendingIndicator(); indicator != "" {
		if hints != "" {
			hints += "  "
		}
		hints += styleDim + indicator + styleReset
	}
	return bar(m.width, status, hints)
}

// footer builds a screen's footer from its declared actions: up to four
// screen-specific keys, then esc back and ? help.
func footer(back bool, actions ...shortcut) string {
	pairs := make([]string, 0, 2*len(actions)+4)
	for i, action := range actions {
		if i == 4 {
			break
		}
		pairs = append(pairs, action.label(), action.action)
	}
	if back {
		pairs = append(pairs, activeKeys.label("back"), "back")
	}
	pairs = append(pairs, activeKeys.label("help"), "help")
	return keyHints(pairs...)
}

// keyHints formats alternating key and action pairs, such as
// keyHints("↵", "open", "esc", "close").
func keyHints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, pairs[i]+styleDim+" "+pairs[i+1]+styleReset)
	}
	return strings.Join(parts, "  ")
}

// prompt renders an editable line with a block cursor.
func prompt(prefix, text string) string { return prefix + syntax.Clean(text) + cursor }

type dialog struct {
	width      int
	title      string
	titleStyle string
	info       string
	body       []string
	hint       string
}

// render draws the dialog as a bordered box. Body lines may contain styles
// and are truncated to the inner width.
func (d dialog) render() []string {
	inner := max(1, d.width-4)
	title := d.titleStyle + styleBold + d.title + styleReset
	info := ""
	if d.info != "" {
		info = styleDim + d.info + styleReset
	}
	lines := []string{styleDim + "┌" + strings.Repeat("─", d.width-2) + "┐" + styleReset, dialogLine(bar(inner, title, info), inner)}
	for _, line := range d.body {
		lines = append(lines, dialogLine(line, inner))
	}
	if d.hint != "" {
		lines = append(lines, dialogLine(d.hint, inner))
	}
	return append(lines, styleDim+"└"+strings.Repeat("─", d.width-2)+"┘"+styleReset)
}

func dialogLine(text string, width int) string {
	if ansi.StringWidth(text) > width {
		text = ansi.Truncate(text, width, "") + styleReset
	}
	return styleDim + "│" + styleReset + " " + text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text))) + " " + styleDim + "│" + styleReset
}

func dialogLineWithCursor(text string, width int) string {
	return dialogLine(fit(text, max(0, width-1))+cursor, width)
}

// placeDialog centers box between the title and status bars of lines.
func placeDialog(lines []string, width int, box []string) {
	if len(box) == 0 {
		return
	}
	left := strings.Repeat(" ", max(0, (width-ansi.StringWidth(box[0]))/2))
	top := 1 + max(0, (len(lines)-2-len(box))/2)
	if top+len(box) > len(lines) {
		top = max(0, len(lines)-len(box))
	}
	for i, line := range box {
		if top+i < len(lines) {
			lines[top+i] = left + line
		}
	}
}

// clearBody blanks every row between the title and status bars.
func clearBody(lines []string) {
	for i := 1; i < len(lines)-1; i++ {
		lines[i] = ""
	}
}

package ui

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// authorGutterWidth is the column history reserves left of each commit for
// its author's initials: a space, two cells of initials and a space.
const authorGutterWidth = 4

// authorGutterMinWidth is the narrowest terminal that still shows the gutter;
// below it the subject needs every column.
const authorGutterMinWidth = 30

// authorPalette holds the colors authors are assigned from, as RGB. Red,
// green and cyan are left out because history uses them for line counts and
// hashes.
var authorPalette = struct{ dark, light [8][3]int }{
	dark: [8][3]int{
		{255, 138, 101}, {121, 192, 255}, {210, 153, 255}, {255, 203, 107},
		{125, 214, 160}, {255, 128, 191}, {160, 180, 255}, {205, 180, 140},
	},
	light: [8][3]int{
		{180, 70, 40}, {20, 95, 170}, {120, 60, 170}, {150, 100, 0},
		{20, 120, 70}, {175, 40, 110}, {70, 80, 170}, {120, 95, 50},
	},
}

// historyGutter is the width of the author column, or zero when it is hidden
// or the terminal is too narrow for it.
func (m model) historyGutter() int {
	if m.hideAuthorBadges || m.width < authorGutterMinWidth {
		return 0
	}
	return authorGutterWidth
}

// authorKey identifies an author by name rather than email: one person
// commits under several emails, such as GitHub's noreply address for merges
// made on the web, and the name is what the reader sees.
func authorKey(c git.CommitInfo) string {
	if name := strings.ToLower(strings.Join(strings.Fields(c.Author), " ")); name != "" {
		return name
	}
	return strings.ToLower(strings.TrimSpace(c.Email))
}

// authorColor is a stable color for an author, so the same person reads the
// same way across sessions and repositories. Bots are dimmed instead.
func (m model) authorColor(c git.CommitInfo) string {
	if isBotAuthor(c.Author) {
		return styleDim
	}
	h := fnv.New32a()
	h.Write([]byte(authorKey(c)))
	palette := authorPalette.dark
	if m.lightTheme {
		palette = authorPalette.light
	}
	rgb := palette[h.Sum32()%uint32(len(palette))]
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
}

// authorInitials abbreviates a name to two cells: the first letters of its
// first and last words, or the start of a single word.
func authorInitials(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(syntax.Clean(name)), "[bot]")
	words := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var initials string
	switch len(words) {
	case 0:
		return "? "
	case 1:
		runes := []rune(words[0])
		initials = string(runes[:min(2, len(runes))])
	default:
		initials = string([]rune(words[0])[0]) + string([]rune(words[len(words)-1])[0])
	}
	initials = fit(strings.ToUpper(initials), 2)
	return initials + strings.Repeat(" ", 2-stringWidth(initials))
}

// authorGutter renders the author column for one row of a commit. The
// subject row carries the initials; the rows below carry a bar, which joins
// with the next commit's when the same author wrote it, so runs of one
// author's work read as a single stripe.
func (m model) authorGutter(c git.CommitInfo, row int, next *git.CommitInfo, selected bool) string {
	color := m.authorColor(c)
	switch {
	case row == 0:
		style := color
		if selected {
			style = styleBold + color
		}
		return " " + style + authorInitials(c.Author) + styleReset + " "
	case row == 1 || next != nil && authorKey(*next) == authorKey(c):
		return " " + color + "│" + styleReset + "  "
	default:
		return strings.Repeat(" ", authorGutterWidth)
	}
}

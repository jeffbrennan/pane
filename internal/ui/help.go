package ui

import (
	"strings"
)

type shortcut struct{ keys, action string }

type shortcutSection struct {
	title string
	// screens limits the section to the screens it names; nil means every
	// screen. The help dialog shows the current screen's section first.
	screens []string
	keys    []shortcut
}

// shortcutSections is the single source for both the help dialog and the
// README key table. Sections follow the shared key grammar: screen actions,
// motion, finding, acting, toggles, general.
var shortcutSections = []shortcutSection{
	{"Changes", []string{""}, []shortcut{
		{"tab", "Switch unstaged / staged"},
		{"space", "Stage / unstage file"},
		{"s", "Stage / unstage hunk"},
		{"S", "Stash all changes, including untracked"},
		{"d", "Discard hunk / file"},
		{"c", "Commit changes"},
		{"p / P", "Push / pull"},
		{"F", "Fetch remote changes"},
		{"r", "Refresh"},
	}},
	{"History", []string{"history"}, []shortcut{
		{"H", "Open commit history"},
		{"↵ / space", "Open selected commit"},
		{"u", "Edit latest unpublished commit message"},
		{"S", "Open stashes"},
		{"b", "Open branches (tab switches to worktrees)"},
		{"X", "Open hotspots for the last year"},
		{"R", "Review from this commit to the working tree"},
		{"V", "Select a commit range to review"},
	}},
	{"Stashes", []string{"stashes"}, []shortcut{
		{"↵ / space", "View stash"},
		{"p / d", "Pop / drop selected stash"},
		{"/", "Filter stashes"},
	}},
	{"Branches", []string{"branches"}, []shortcut{
		{"↵", "Switch to the selected branch"},
		{"n / w", "New branch from the selection / new worktree for it"},
		{"d / D", "Delete branch / force delete"},
		{"r", "Include remote branches"},
		{"y", "Copy branch name"},
	}},
	{"Worktrees", []string{"worktrees"}, []shortcut{
		{"↵", "Switch this pane to the worktree"},
		{"n", "New worktree (path and branch)"},
		{"d / D", "Remove worktree / force remove"},
		{"p", "Prune stale worktrees"},
		{"y", "Copy path"},
	}},
	{"Hotspots", []string{"hotspots"}, []shortcut{
		{"tab", "Switch between the file tree and authors"},
		{"s", "Sort by score or path"},
		{"h / l", "Fold a directory"},
		{"↵", "Open file history or filter by author"},
		{"V", "Review the selection as a commit range"},
		{"/", "Filter hotspots"},
	}},
	{"Commit input", []string{"commit-input"}, []shortcut{
		{"← / →, ctrl-b / ctrl-f, alt-b / alt-f", "Move by character or word"},
		{"home / end, ctrl-a / ctrl-e", "Start / end of subject"},
		{"ctrl-w / alt-backspace, ctrl-u / ctrl-k", "Delete previous word, to start, or to end"},
		{"backspace / delete, ctrl-d", "Delete character"},
		{"ctrl-g", "Write message in external editor"},
		{"↵ / esc", "Commit / cancel"},
	}},
	{"Motion", nil, []shortcut{
		{"j / k", "Down / up"},
		{"gg / G", "First / last"},
		{"ctrl-d / ctrl-u", "Half page down / up"},
		{"ctrl-f / ctrl-b", "Page down / up"},
		{"ctrl-e / ctrl-y", "Scroll the diff viewport"},
		{"} / {", "Next / previous group"},
		{"J / K", "Next / previous file"},
		{") / (", "Next / previous commit in the list"},
		{"]c / [c", "Next / previous change"},
		{"gH / gM / gL", "Top / middle / bottom of screen"},
		{"zt / zz / zb", "Align the active line"},
		{"zh / zl", "Scroll horizontally"},
		{"ctrl-o / gi", "Jump back / forward"},
		{"count + motion", "Repeat a motion; with gg / G, jump to a source line"},
	}},
	{"Finding", nil, []shortcut{
		{"f", "File tree (h/l fold, v hides viewed files)"},
		{"/ / g?", "Search the diff forward / backward, or filter a list"},
		{"n / N", "Next / previous match"},
		{"L", "Open history for the current file"},
		{"gb", "Blame the current context or removed line"},
		{"← / →, home / end", "Move within a query"},
		{"alt-b / alt-f, ctrl-a / ctrl-e", "Move by word or to start / end"},
		{"ctrl-w / alt-backspace", "Delete the previous word"},
		{"backspace / delete", "Delete a character"},
		{"esc", "Clear the filter first, then go back"},
	}},
	{"Acting", nil, []shortcut{
		{"↵", "Open or activate the selection"},
		{"y / Y", "Copy the name or path / a location or link"},
		{"o", "Open the file in $EDITOR"},
		{"v", "Mark the file viewed / unviewed"},
		{"e", "Expand / collapse a deleted file"},
		{"h / l", "Fold / unfold the thing under the cursor"},
	}},
	{"Toggles", nil, []shortcut{
		{"w", "Toggle line wrapping"},
		{"W", "Toggle ignoring whitespace"},
		{"B", "Toggle short file paths"},
		{"T", "Toggle light / dark colors"},
	}},
	{"General", nil, []shortcut{
		{"?", "Show shortcuts"},
		{"esc / q", "Go back one screen; q quits on the first screen"},
	}},
}

const helpBoxWidth = 76

// helpVisibleRows is the number of shortcut rows shown in a help dialog on a
// screen of height rows: the title and status bars, the dialog border, title
// and hint take the rest.
func helpVisibleRows(height int) int { return max(1, height-6) }

// helpScreen names the help section for the current view. Commit and range
// views map to history because they are opened from it.
func (m model) helpScreen() string {
	if m.modal == "commit" {
		return "commit-input"
	}
	if m.helpContext != "" {
		return m.helpContext
	}
	switch m.screen {
	case "commit", "base", "sequence":
		return "history"
	}
	return m.screen
}

func (m model) sectionApplies(section shortcutSection) bool {
	current := m.helpScreen()
	for _, screen := range section.screens {
		if screen == current {
			return true
		}
	}
	return false
}

// helpLines renders the shortcut rows for the current screen and filter. The
// current screen's section comes first, then the shared grammar groups, then
// the other screens dimmed at the end.
func (m model) helpLines() []string {
	query := strings.ToLower(strings.TrimSpace(m.help.filter))
	keep := func(s shortcut) bool {
		return query == "" || strings.Contains(strings.ToLower(s.keys+" "+s.action), query)
	}
	innerWidth := max(1, min(helpBoxWidth, m.width-2)-4)
	var lines []string
	addSection := func(section shortcutSection, dim bool) {
		var keys []shortcut
		for _, s := range section.keys {
			if keep(s) {
				keys = append(keys, s)
			}
		}
		if len(keys) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		title := section.title
		if dim {
			title = styleDim + title + styleReset
		} else {
			title = styleBold + title + styleReset
		}
		lines = append(lines, title)
		keyWidth := 0
		for _, s := range keys {
			keyWidth = max(keyWidth, stringWidth(s.keys))
		}
		keyWidth += 2
		for _, s := range keys {
			lines = append(lines, shortcutRows(m.accent(), s, keyWidth, innerWidth, dim)...)
		}
	}
	for _, section := range shortcutSections {
		if len(section.screens) > 0 && m.sectionApplies(section) {
			addSection(section, false)
		}
	}
	for _, section := range shortcutSections {
		if len(section.screens) == 0 {
			addSection(section, false)
		}
	}
	var other []shortcutSection
	for _, section := range shortcutSections {
		if len(section.screens) > 0 && !m.sectionApplies(section) {
			other = append(other, section)
		}
	}
	first := true
	for _, section := range other {
		if first && hasShortcuts(section, keep) {
			lines = append(lines, "", styleDim+"Other screens"+styleReset)
			first = false
		}
		addSection(section, true)
	}
	return lines
}

func hasShortcuts(section shortcutSection, keep func(shortcut) bool) bool {
	for _, s := range section.keys {
		if keep(s) {
			return true
		}
	}
	return false
}

func shortcutRows(accent string, s shortcut, keyWidth, innerWidth int, dim bool) []string {
	if innerWidth < keyWidth+16 {
		var rows []string
		for j, part := range wrapLine(s.keys+"  "+s.action, innerWidth) {
			if j == 0 {
				part = accent + part + styleReset
			}
			rows = append(rows, part)
		}
		return rows
	}
	var rows []string
	for j, part := range wrapWords(s.action, innerWidth-keyWidth) {
		key := ""
		if j == 0 {
			key = s.keys
		}
		row := accent + key + styleReset + strings.Repeat(" ", keyWidth-stringWidth(key)) + part
		if dim {
			row = styleDim + row + styleReset
		}
		rows = append(rows, row)
	}
	return rows
}

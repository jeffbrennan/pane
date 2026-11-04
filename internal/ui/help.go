package ui

import (
	"strings"
)

// shortcut is one row of the help dialog or a footer. Rows for customizable
// keys name their bindings in ids and read their keys from activeKeys; the
// rest, such as text editing keys, spell them out in keys.
type shortcut struct {
	keys, action string
	ids          []string
}

// hint describes the bindings ids as action.
func hint(action string, ids ...string) shortcut { return shortcut{action: action, ids: ids} }

// label renders the shortcut's keys under the active bindings.
func (s shortcut) label() string {
	if len(s.ids) == 0 {
		return s.keys
	}
	return activeKeys.labels(s.ids...)
}

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
		hint("Switch unstaged / staged", "changes.switch-view"),
		hint("Stage / unstage file", "changes.stage-file"),
		hint("Stage / unstage hunk", "changes.stage-hunk"),
		hint("Stash all changes, including untracked", "changes.stash"),
		hint("Discard hunk / file", "changes.discard"),
		hint("Commit changes", "changes.commit"),
		hint("Push / pull", "changes.push", "changes.pull"),
		hint("Fetch remote changes", "fetch"),
		hint("Refresh", "changes.refresh"),
	}},
	{"History", []string{"history"}, []shortcut{
		hint("Open commit history", "changes.history"),
		hint("Open selected commit", "open", "history.open"),
		hint("Edit latest unpublished commit message", "history.edit-message"),
		hint("Open stashes", "history.stashes"),
		hint("Open branches (tab switches to worktrees)", "history.branches"),
		hint("Open hotspots for the last year", "history.hotspots"),
		hint("Review from this commit to the working tree", "history.review-to-worktree"),
		hint("Select a commit range to review", "history.select-range"),
		hint("Collapse / expand merged commits", "history.collapse-merges"),
	}},
	{"Stashes", []string{"stashes"}, []shortcut{
		hint("View stash", "open", "stashes.open"),
		hint("Pop / drop selected stash", "stashes.pop", "stashes.drop"),
		hint("Filter stashes", "search"),
	}},
	{"Branches", []string{"branches"}, []shortcut{
		hint("Switch to the selected branch", "open"),
		hint("New branch from the selection / new worktree for it", "branches.new-branch", "branches.new-worktree"),
		hint("Delete branch / force delete", "branches.delete", "branches.force-delete"),
		hint("Include remote branches", "branches.remotes"),
		hint("Copy branch name", "copy"),
	}},
	{"Worktrees", []string{"worktrees"}, []shortcut{
		hint("Switch this pane to the worktree", "open"),
		hint("New worktree (path and branch)", "worktrees.new-worktree"),
		hint("Remove worktree / force remove", "worktrees.delete", "worktrees.force-delete"),
		hint("Prune stale worktrees", "worktrees.prune"),
		hint("Copy path", "copy"),
	}},
	{"Hotspots", []string{"hotspots"}, []shortcut{
		hint("Cycle tree, files, authors", "hotspots.switch-view"),
		hint("Fold a directory / expand the rest", "fold", "unfold"),
		hint("Open file history, fold a directory, or filter by author", "open"),
		hint("Open history for the selected path", "file-history"),
		hint("Open the file in $EDITOR", "editor"),
		hint("Review the selection as a commit range", "hotspots.review-range"),
		hint("Cycle 3 months, 12 months, all time", "hotspots.window"),
		hint("Show or hide generated and vendored files", "hotspots.show-hidden"),
		hint("Filter hotspots", "search"),
	}},
	{"Commit input", []string{"commit-input"}, []shortcut{
		{keys: "← / →, ctrl-b / ctrl-f, alt-b / alt-f", action: "Move by character or word"},
		{keys: "home / end, ctrl-a / ctrl-e", action: "Start / end of subject"},
		{keys: "ctrl-w / alt-backspace, ctrl-u / ctrl-k", action: "Delete previous word, to start, or to end"},
		{keys: "backspace / delete, ctrl-d", action: "Delete character"},
		{keys: "ctrl-g", action: "Write message in external editor"},
		{keys: "↵ / esc", action: "Commit / cancel"},
	}},
	{"Motion", nil, []shortcut{
		hint("Down / up", "down", "up"),
		hint("First / last", "top", "bottom"),
		hint("Half page down / up", "half-page-down", "half-page-up"),
		hint("Page down / up", "page-down", "page-up"),
		hint("Scroll the diff viewport", "scroll-down", "scroll-up"),
		hint("Next / previous group", "next-group", "previous-group"),
		hint("Next / previous file", "next-file", "previous-file"),
		hint("Next / previous commit in history or a range", "next-commit", "previous-commit"),
		hint("Next / previous change", "next-change", "previous-change"),
		hint("Top / middle / bottom of screen", "screen-top", "screen-middle", "screen-bottom"),
		hint("Align the active line", "align-top", "align-center", "align-bottom"),
		hint("Scroll horizontally", "scroll-left", "scroll-right"),
		hint("Jump back / forward", "jump-back", "jump-forward"),
		{keys: "count + motion", action: "Repeat a motion; with gg / G, jump to a source line"},
	}},
	{"Finding", nil, []shortcut{
		hint("File tree (h/l fold, v hides viewed files)", "files"),
		hint("Search the diff forward / backward, or filter a list", "search", "search-backward"),
		hint("Next / previous match", "next-match", "previous-match"),
		hint("Open history for the current file", "file-history"),
		hint("Blame the current context or removed line", "blame"),
		{keys: "← / →, home / end", action: "Move within a query"},
		{keys: "alt-b / alt-f, ctrl-a / ctrl-e", action: "Move by word or to start / end"},
		{keys: "ctrl-w / alt-backspace", action: "Delete the previous word"},
		{keys: "backspace / delete", action: "Delete a character"},
		hint("Clear the filter first, then go back", "back"),
	}},
	{"Acting", nil, []shortcut{
		hint("Open or activate the selection", "open"),
		hint("Copy the name or path / a location or link", "copy", "copy-location"),
		hint("Open the file in $EDITOR", "editor"),
		hint("Mark the file viewed / unviewed", "viewed"),
		hint("Expand / collapse a deleted file", "expand-deleted"),
		hint("Fold / unfold the cursor: a file, commit body, or context", "fold", "unfold"),
	}},
	{"Toggles", nil, []shortcut{
		hint("Toggle line wrapping", "wrap"),
		hint("Toggle ignoring whitespace", "whitespace"),
		hint("Toggle short file paths", "short-paths"),
		hint("Toggle light / dark colors", "theme"),
	}},
	{"General", nil, []shortcut{
		hint("Show shortcuts", "help"),
		hint("Go back one screen; q quits on the first screen", "back", "quit"),
		hint("Customize keys (in this dialog)", "help.edit-keys"),
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
		return query == "" || strings.Contains(strings.ToLower(s.label()+" "+s.action), query)
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
			keyWidth = max(keyWidth, stringWidth(s.label()))
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
		for j, part := range wrapLine(s.label()+"  "+s.action, innerWidth) {
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
			key = s.label()
		}
		row := accent + key + styleReset + strings.Repeat(" ", keyWidth-stringWidth(key)) + part
		if dim {
			row = styleDim + row + styleReset
		}
		rows = append(rows, row)
	}
	return rows
}

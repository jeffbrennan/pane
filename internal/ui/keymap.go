package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// action names what a key does. Shared actions mean the same thing on every
// screen that supports them; screens interpret them in their own terms, so
// open switches a branch on the branches screen and opens a commit in
// history.
type action string

const (
	actNone      action = ""
	actInterrupt action = "interrupt"

	actDown          action = "down"
	actUp            action = "up"
	actTop           action = "top"
	actBottom        action = "bottom"
	actHalfDown      action = "half-page-down"
	actHalfUp        action = "half-page-up"
	actPageDown      action = "page-down"
	actPageUp        action = "page-up"
	actScrollDown    action = "scroll-down"
	actScrollUp      action = "scroll-up"
	actNextGroup     action = "next-group"
	actPrevGroup     action = "previous-group"
	actNextFile      action = "next-file"
	actPrevFile      action = "previous-file"
	actNextCommit    action = "next-commit"
	actPrevCommit    action = "previous-commit"
	actNextChange    action = "next-change"
	actPrevChange    action = "previous-change"
	actScreenTop     action = "screen-top"
	actScreenMiddle  action = "screen-middle"
	actScreenBottom  action = "screen-bottom"
	actAlignTop      action = "align-top"
	actAlignCenter   action = "align-center"
	actAlignBottom   action = "align-bottom"
	actScrollLeft    action = "scroll-left"
	actScrollRight   action = "scroll-right"
	actHalfLeft      action = "half-screen-left"
	actHalfRight     action = "half-screen-right"
	actJumpBack      action = "jump-back"
	actJumpForward   action = "jump-forward"
	actSearch        action = "search"
	actSearchBack    action = "search-backward"
	actNextMatch     action = "next-match"
	actPrevMatch     action = "previous-match"
	actFiles         action = "files"
	actFileHistory   action = "file-history"
	actBlame         action = "blame"
	actOpen          action = "open"
	actCopy          action = "copy"
	actCopyLocation  action = "copy-location"
	actEditor        action = "editor"
	actViewed        action = "viewed"
	actExpandDeleted action = "expand-deleted"
	actFold          action = "fold"
	actUnfold        action = "unfold"
	actWrap          action = "wrap"
	actWhitespace    action = "whitespace"
	actShortPaths    action = "short-paths"
	actTheme         action = "theme"
	actFetch         action = "fetch"
	actHelp          action = "help"
	actBack          action = "back"
	actQuit          action = "quit"

	actSwitchView     action = "switch-view"
	actStageFile      action = "stage-file"
	actStageHunk      action = "stage-hunk"
	actStash          action = "stash"
	actDiscard        action = "discard"
	actCommit         action = "commit"
	actPush           action = "push"
	actPull           action = "pull"
	actRefresh        action = "refresh"
	actHistory        action = "history"
	actEditMessage    action = "edit-message"
	actStashes        action = "stashes"
	actBranches       action = "branches"
	actHotspots       action = "hotspots"
	actReviewBase     action = "review-to-worktree"
	actSelectRange    action = "select-range"
	actCollapseMerges action = "collapse-merges"
	actAuthorBadges   action = "author-badges"
	actBotCommits     action = "bot-commits"
	actNewBranch      action = "new-branch"
	actNewWorktree    action = "new-worktree"
	actPop            action = "pop"
	actDrop           action = "drop"
	actRemotes        action = "remotes"
	actDelete         action = "delete"
	actForceDelete    action = "force-delete"
	actPrune          action = "prune"
	actWindow         action = "window"
	actShowHidden     action = "show-hidden"
	actReviewRange    action = "review-range"
	actEditKeys       action = "edit-keys"
	actAddKey         action = "add-key"
	actResetKey       action = "reset-key"
	actRemoveKey      action = "remove-key"
)

// Key scopes. Shared bindings live in the unnamed scope and apply on every
// screen; a screen's own bindings take precedence over them.
const (
	scopeShared    = ""
	scopeChanges   = "changes"
	scopeHistory   = "history"
	scopeReview    = "review"
	scopeStashes   = "stashes"
	scopeBranches  = "branches"
	scopeWorktrees = "worktrees"
	scopeHotspots  = "hotspots"
	scopeFiles     = "files"
	scopeHelp      = "help"
	scopeKeys      = "keys"
)

var scopeTitles = map[string]string{
	scopeShared:    "Everywhere",
	scopeChanges:   "Changes",
	scopeHistory:   "History",
	scopeReview:    "Commit review",
	scopeStashes:   "Stashes",
	scopeBranches:  "Branches",
	scopeWorktrees: "Worktrees",
	scopeHotspots:  "Hotspots",
	scopeFiles:     "File tree",
	scopeHelp:      "Shortcuts dialog",
	scopeKeys:      "Key editor",
}

// binding is one customizable action. A key that is a sequence, such as gg,
// separates its keys with spaces.
type binding struct {
	scope       string
	action      action
	keys        []string
	description string
}

func (b binding) id() string {
	if b.scope == scopeShared {
		return string(b.action)
	}
	return b.scope + "." + string(b.action)
}

// defaultBindings is the single table of customizable keys. The help dialog,
// footers and key editor all read keys from it.
var defaultBindings = []binding{
	{scopeShared, actDown, []string{"j", "down", "ctrl+n"}, "Down"},
	{scopeShared, actUp, []string{"k", "up", "ctrl+p"}, "Up"},
	{scopeShared, actTop, []string{"g g", "home"}, "First line or entry"},
	{scopeShared, actBottom, []string{"G", "end"}, "Last line or entry"},
	{scopeShared, actHalfDown, []string{"ctrl+d"}, "Half page down"},
	{scopeShared, actHalfUp, []string{"ctrl+u"}, "Half page up"},
	{scopeShared, actPageDown, []string{"ctrl+f", "pgdown"}, "Page down"},
	{scopeShared, actPageUp, []string{"ctrl+b", "pgup"}, "Page up"},
	{scopeShared, actScrollDown, []string{"ctrl+e"}, "Scroll the diff viewport down"},
	{scopeShared, actScrollUp, []string{"ctrl+y"}, "Scroll the diff viewport up"},
	{scopeShared, actNextGroup, []string{"}"}, "Next group"},
	{scopeShared, actPrevGroup, []string{"{"}, "Previous group"},
	{scopeShared, actNextFile, []string{"J"}, "Next file"},
	{scopeShared, actPrevFile, []string{"K"}, "Previous file"},
	{scopeShared, actNextCommit, []string{")"}, "Next commit in history or a range"},
	{scopeShared, actPrevCommit, []string{"("}, "Previous commit in history or a range"},
	{scopeShared, actNextChange, []string{"] c"}, "Next change"},
	{scopeShared, actPrevChange, []string{"[ c"}, "Previous change"},
	{scopeShared, actScreenTop, []string{"g H"}, "Top of screen"},
	{scopeShared, actScreenMiddle, []string{"g M"}, "Middle of screen"},
	{scopeShared, actScreenBottom, []string{"g L"}, "Bottom of screen"},
	{scopeShared, actAlignTop, []string{"z t"}, "Align the active line to the top"},
	{scopeShared, actAlignCenter, []string{"z z"}, "Align the active line to the center"},
	{scopeShared, actAlignBottom, []string{"z b"}, "Align the active line to the bottom"},
	{scopeShared, actScrollLeft, []string{"z h", "z ctrl+h", "z backspace", "ctrl+h", "backspace"}, "Scroll left"},
	{scopeShared, actScrollRight, []string{"z l"}, "Scroll right"},
	{scopeShared, actHalfLeft, []string{"z H"}, "Scroll left half a screen"},
	{scopeShared, actHalfRight, []string{"z L"}, "Scroll right half a screen"},
	{scopeShared, actJumpBack, []string{"ctrl+o"}, "Jump back"},
	{scopeShared, actJumpForward, []string{"g i"}, "Jump forward"},
	{scopeShared, actSearch, []string{"/"}, "Search the diff, or filter a list"},
	{scopeShared, actSearchBack, []string{"g ?"}, "Search the diff backward"},
	{scopeShared, actNextMatch, []string{"n"}, "Next match"},
	{scopeShared, actPrevMatch, []string{"N"}, "Previous match"},
	{scopeShared, actFiles, []string{"f"}, "File tree"},
	{scopeShared, actFileHistory, []string{"L"}, "Open history for the current file"},
	{scopeShared, actBlame, []string{"g b"}, "Blame the current context or removed line"},
	{scopeShared, actOpen, []string{"enter"}, "Open or activate the selection"},
	{scopeShared, actCopy, []string{"y"}, "Copy the name or path"},
	{scopeShared, actCopyLocation, []string{"Y"}, "Copy a location or link"},
	{scopeShared, actEditor, []string{"o"}, "Open the file in $EDITOR"},
	{scopeShared, actViewed, []string{"v"}, "Mark the file viewed / unviewed"},
	{scopeShared, actExpandDeleted, []string{"e"}, "Expand / collapse a deleted file"},
	{scopeShared, actFold, []string{"h", "left"}, "Fold the cursor: a file, commit body, or context"},
	{scopeShared, actUnfold, []string{"l", "right"}, "Unfold the cursor"},
	{scopeShared, actWrap, []string{"w"}, "Toggle line wrapping"},
	{scopeShared, actWhitespace, []string{"W"}, "Toggle ignoring whitespace"},
	{scopeShared, actShortPaths, []string{"~"}, "Toggle short file paths"},
	{scopeShared, actTheme, []string{"T"}, "Toggle light / dark colors"},
	{scopeShared, actFetch, []string{"F"}, "Fetch remote changes"},
	{scopeShared, actHelp, []string{"?"}, "Show shortcuts"},
	{scopeShared, actBack, []string{"esc"}, "Go back one screen"},
	{scopeShared, actQuit, []string{"q"}, "Go back; quit on the first screen"},

	{scopeChanges, actSwitchView, []string{"tab"}, "Switch unstaged / staged"},
	{scopeChanges, actStageFile, []string{"space"}, "Stage / unstage file"},
	{scopeChanges, actStageHunk, []string{"s"}, "Stage / unstage hunk"},
	{scopeChanges, actStash, []string{"S"}, "Stash all changes, including untracked"},
	{scopeChanges, actDiscard, []string{"d"}, "Discard hunk / file"},
	{scopeChanges, actCommit, []string{"c"}, "Commit changes"},
	{scopeChanges, actPush, []string{"p"}, "Push"},
	{scopeChanges, actPull, []string{"P"}, "Pull"},
	{scopeChanges, actRefresh, []string{"r"}, "Refresh"},
	{scopeChanges, actHistory, []string{"H"}, "Open commit history"},

	{scopeHistory, actOpen, []string{"space"}, "Open selected commit"},
	{scopeHistory, actEditMessage, []string{"A"}, "Edit latest unpublished commit message"},
	{scopeHistory, actStashes, []string{"S"}, "Open stashes"},
	{scopeHistory, actBranches, []string{"b"}, "Open branches"},
	{scopeHistory, actHotspots, []string{"%"}, "Open hotspots for the last year"},
	{scopeHistory, actReviewBase, []string{"R"}, "Review from this commit to the working tree"},
	{scopeHistory, actSelectRange, []string{"V"}, "Select a commit range to review"},
	{scopeHistory, actCollapseMerges, []string{"m"}, "Collapse / expand merged commits"},
	{scopeHistory, actAuthorBadges, []string{"a"}, "Show / hide author badges"},
	{scopeHistory, actBotCommits, []string{"B"}, "Show / hide bot commits"},
	{scopeHistory, actNewBranch, []string{"n"}, "New branch from the selected commit"},
	{scopeHistory, actPush, []string{"p"}, "Push"},
	{scopeHistory, actPull, []string{"P"}, "Pull"},
	{scopeHistory, actRefresh, []string{"r"}, "Refresh"},

	{scopeStashes, actOpen, []string{"space"}, "View stash"},
	{scopeStashes, actPop, []string{"p"}, "Pop selected stash"},
	{scopeStashes, actDrop, []string{"d"}, "Drop selected stash"},

	{scopeBranches, actSwitchView, []string{"tab"}, "Switch to worktrees"},
	{scopeBranches, actNewBranch, []string{"n"}, "New branch from the selection"},
	{scopeBranches, actNewWorktree, []string{"W"}, "New worktree for the selection"},
	{scopeBranches, actDelete, []string{"d"}, "Delete branch"},
	{scopeBranches, actForceDelete, []string{"D"}, "Force delete branch"},
	{scopeBranches, actRemotes, []string{"R"}, "Include remote branches"},
	{scopeBranches, actRefresh, []string{"r"}, "Refresh"},

	{scopeWorktrees, actSwitchView, []string{"tab"}, "Switch to branches"},
	{scopeWorktrees, actNewWorktree, []string{"n"}, "New worktree (path and branch)"},
	{scopeWorktrees, actDelete, []string{"d"}, "Remove worktree"},
	{scopeWorktrees, actForceDelete, []string{"D"}, "Force remove worktree"},
	{scopeWorktrees, actPrune, []string{"p"}, "Prune stale worktrees"},
	{scopeWorktrees, actRefresh, []string{"r"}, "Refresh"},

	{scopeHotspots, actSwitchView, []string{"tab"}, "Cycle tree, files, authors"},
	{scopeHotspots, actReviewRange, []string{"V"}, "Review the selection as a commit range"},
	{scopeHotspots, actWindow, []string{"w"}, "Cycle 3 months, 12 months, all time"},
	{scopeHotspots, actShowHidden, []string{"a"}, "Show or hide generated and vendored files"},

	{scopeFiles, actDown, []string{"ctrl+j", "tab"}, "Down"},
	{scopeFiles, actUp, []string{"ctrl+k", "shift+tab"}, "Up"},

	{scopeHelp, actEditKeys, []string{"e"}, "Customize keys"},

	{scopeKeys, actAddKey, []string{"a"}, "Add another key to the action"},
	{scopeKeys, actRemoveKey, []string{"d"}, "Remove a key from the action"},
	{scopeKeys, actResetKey, []string{"x"}, "Reset the action to its default keys"},
}

// keymap resolves keys to actions for a scope. Every binding's keys are
// stored, defaults included, so lookups never fall back to the table.
type keymap struct {
	keys     map[string][]string
	actions  map[string]map[string]action
	prefixes map[string]map[string]bool
}

// activeKeys is the keymap every screen resolves keys against. Run loads the
// user's overrides into it; tests see the defaults.
var activeKeys = newKeymap(nil)

func newKeymap(overrides map[string][]string) *keymap {
	k := &keymap{keys: make(map[string][]string)}
	for _, b := range defaultBindings {
		k.keys[b.id()] = b.keys
		if keys, ok := overrides[b.id()]; ok {
			k.keys[b.id()] = keys
		}
	}
	k.index()
	return k
}

func (k *keymap) index() {
	k.actions = make(map[string]map[string]action)
	k.prefixes = make(map[string]map[string]bool)
	for _, b := range defaultBindings {
		if k.actions[b.scope] == nil {
			k.actions[b.scope] = make(map[string]action)
			k.prefixes[b.scope] = make(map[string]bool)
		}
		for _, key := range k.keys[b.id()] {
			if _, taken := k.actions[b.scope][key]; !taken {
				k.actions[b.scope][key] = b.action
			}
			parts := strings.Split(key, " ")
			for i := 1; i < len(parts); i++ {
				k.prefixes[b.scope][strings.Join(parts[:i], " ")] = true
			}
		}
	}
}

// find returns the action for key on a screen's scope, falling back to the
// shared bindings.
func (k *keymap) find(key, scope string) (action, bool) {
	if act, ok := k.actions[scope][key]; ok {
		return act, true
	}
	act, ok := k.actions[scopeShared][key]
	return act, ok
}

func (k *keymap) isPrefix(key, scope string) bool {
	return k.prefixes[scope][key] || k.prefixes[scopeShared][key]
}

// label renders the first key bound to id, or nothing when it is unbound.
func (k *keymap) label(id string) string {
	if keys := k.keys[id]; len(keys) > 0 {
		return keyLabel(keys[0])
	}
	return ""
}

// labels joins the first key of each binding, as in "j / k".
func (k *keymap) labels(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if label := k.label(id); label != "" {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, " / ")
}

// pair renders two bindings compactly for a key hint, as in "j/k".
func (k *keymap) pair(a, b string) string { return k.label(a) + "/" + k.label(b) }

var keyGlyphs = map[string]string{
	"enter": "↵", "up": "↑", "down": "↓", "left": "←", "right": "→",
}

// keyLabel renders a key or sequence the way the help shows it: ctrl-d, ↵,
// and gg for a sequence of single characters.
func keyLabel(key string) string {
	parts := strings.Split(key, " ")
	short := true
	for i, part := range parts {
		if glyph, ok := keyGlyphs[part]; ok {
			part = glyph
		} else {
			part = strings.ReplaceAll(part, "+", "-")
		}
		parts[i] = part
		short = short && len([]rune(part)) == 1
	}
	if short {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, " ")
}

// keyPress is a key resolved against the current screen's bindings, with any
// count typed before it.
type keyPress struct {
	key      tea.KeyMsg
	action   action
	count    int
	hadCount bool
}

// rawPress passes a key through unresolved, for screens that are editing
// text. Enter still confirms, so it acts as open.
func rawPress(key tea.KeyMsg) keyPress {
	p := keyPress{key: key, count: 1}
	if key.String() == "enter" {
		p.action = actOpen
	}
	return p
}

// press resolves key on scope. Counts and the start of a sequence are kept
// in m.count and m.pending and resolve to no action. A key that does not
// continue the pending sequence starts over on its own.
func (m *model) press(key tea.KeyMsg, scope string) keyPress {
	name := keyName(key)
	if name == "ctrl+c" {
		m.pending, m.count = "", 0
		return keyPress{key: key, action: actInterrupt, count: 1}
	}
	p := keyPress{key: key, count: max(1, m.count), hadCount: m.count > 0}
	if m.pending != "" {
		sequence := m.pending + " " + name
		m.pending = ""
		if name == "esc" {
			m.count = 0
			return keyPress{key: key, count: 1}
		}
		if activeKeys.isPrefix(sequence, scope) {
			m.pending = sequence
			return keyPress{key: key, count: 1}
		}
		if act, ok := activeKeys.find(sequence, scope); ok {
			m.count = 0
			p.action = act
			return p
		}
	}
	if activeKeys.isPrefix(name, scope) {
		m.pending = name
		return keyPress{key: key, count: 1}
	}
	if act, ok := activeKeys.find(name, scope); ok {
		m.count = 0
		p.action = act
		return p
	}
	if len(name) == 1 && name[0] >= '0' && name[0] <= '9' {
		m.count = min(1000000, m.count*10+int(name[0]-'0'))
		return keyPress{key: key, count: 1}
	}
	m.count = 0
	return p
}

// keyScope names the bindings that apply to the current screen.
func (m model) keyScope() string {
	switch m.screen {
	case "":
		return scopeChanges
	case "commit", "base", "sequence":
		return scopeReview
	}
	return m.screen
}

// keysPath is where customized keys persist: $XDG_CONFIG_HOME/pane/keys,
// or ~/.config/pane/keys.
func keysPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pane", "keys")
}

// keyName is bubbletea's name for key, except that the space bar is space
// so that a space can separate the keys of a sequence.
func keyName(key tea.KeyMsg) string {
	if key.Type == tea.KeySpace {
		return "space"
	}
	return key.String()
}

// configKey and parseConfigKey spell keys in the config file, escaping the
// characters that separate keys and start comments.
func configKey(key string) string {
	parts := strings.Split(key, " ")
	for i, part := range parts {
		if part == "," || part == "#" {
			parts[i] = "\\" + part
		}
	}
	return strings.Join(parts, " ")
}

func parseConfigKey(text string) string {
	parts := strings.Fields(text)
	for i, part := range parts {
		switch part {
		case "\\,", "\\#":
			parts[i] = part[1:]
		}
	}
	return strings.Join(parts, " ")
}

// loadKeys reads overrides from path. Lines are `id = key, key`; a missing
// file means the defaults. Unknown actions are reported, not fatal.
func loadKeys(path string) (*keymap, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return newKeymap(nil), nil
	}
	if err != nil {
		return newKeymap(nil), err
	}
	defer file.Close()
	known := make(map[string]bool)
	for _, b := range defaultBindings {
		known[b.id()] = true
	}
	overrides := make(map[string][]string)
	var unknown []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, value, ok := strings.Cut(line, "=")
		id = strings.TrimSpace(id)
		if !ok || !known[id] {
			unknown = append(unknown, id)
			continue
		}
		var keys []string
		for _, field := range splitKeys(value) {
			if key := parseConfigKey(field); key != "" {
				keys = append(keys, key)
			}
		}
		overrides[id] = keys
	}
	k := newKeymap(overrides)
	if err := scanner.Err(); err != nil {
		return k, err
	}
	if len(unknown) > 0 {
		return k, fmt.Errorf("unknown key actions in %s: %s", path, strings.Join(unknown, ", "))
	}
	return k, nil
}

// splitKeys splits a config value at commas that are not escaped.
func splitKeys(value string) []string {
	var fields []string
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if value[i] == ',' {
			fields = append(fields, value[start:i])
			start = i + 1
		}
	}
	return append(fields, value[start:])
}

// save writes the bindings that differ from the defaults, so actions added
// in later versions keep their default keys.
func (k *keymap) save(path string) error {
	if path == "" {
		return fmt.Errorf("no config directory")
	}
	var lines []string
	for _, b := range defaultBindings {
		keys := k.keys[b.id()]
		if slices.Equal(keys, b.keys) {
			continue
		}
		spelled := make([]string, len(keys))
		for i, key := range keys {
			spelled[i] = configKey(key)
		}
		lines = append(lines, b.id()+" = "+strings.Join(spelled, ", "))
	}
	sort.Strings(lines)
	content := "# pane key bindings. Edit here or press e in the ? dialog.\n" +
		"# Each line is `action = key, key`; separate the keys of a sequence with spaces.\n" +
		strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

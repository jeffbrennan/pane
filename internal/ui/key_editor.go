package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// keyEditorState is the key editor inside the shortcuts dialog. While
// capturing, the next key pressed becomes the selected action's key.
type keyEditorState struct {
	open      bool
	list      listState
	capturing bool
	adding    bool
	// swap holds a captured key that is already bound in the same scope
	// until the person confirms taking it.
	swap    string
	message string
}

func (m *model) openKeyEditor() {
	m.keyEditor = keyEditorState{open: true}
}

// editorBindings returns the bindings matching the editor's filter.
func (m model) editorBindings() []binding {
	query := strings.ToLower(strings.TrimSpace(m.keyEditor.list.filter))
	var matches []binding
	for _, b := range defaultBindings {
		text := strings.ToLower(b.id() + " " + b.description + " " + scopeTitles[b.scope] + " " + bindingKeys(b.id()))
		if query == "" || strings.Contains(text, query) {
			matches = append(matches, b)
		}
	}
	return matches
}

// bindingKeys lists every key bound to id, as the editor shows them.
func bindingKeys(id string) string {
	keys := activeKeys.keys[id]
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = keyLabel(key)
	}
	return strings.Join(labels, ", ")
}

func (m model) handleKeyEditorKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	editor := &m.keyEditor
	matches := m.editorBindings()
	selected := min(editor.list.selected, max(0, len(matches)-1))
	if editor.capturing {
		return m.captureKey(key, matches, selected)
	}
	groups := func(index int) int {
		for index > 0 && matches[index-1].scope == matches[index].scope {
			index--
		}
		return index
	}
	p := m.listPress(&editor.list, key, scopeKeys)
	if m.handleListKey(&editor.list, p, len(matches), helpVisibleRows(m.height)-1, groups) {
		return m, nil
	}
	editor.message = ""
	switch p.action {
	case actOpen, actAddKey:
		if len(matches) > 0 {
			editor.capturing, editor.adding = true, p.action == actAddKey
		}
	case actResetKey:
		if len(matches) > 0 {
			b := matches[selected]
			activeKeys.keys[b.id()] = b.keys
			activeKeys.index()
			editor.message = m.saveKeys("Reset " + b.description)
		}
	case actBack, actQuit, actHelp, actInterrupt:
		m.keyEditor = keyEditorState{}
	}
	return m, nil
}

// captureKey binds the captured key, asking first when it would take the key
// from another action in the same scope.
func (m model) captureKey(key tea.KeyMsg, matches []binding, selected int) (tea.Model, tea.Cmd) {
	editor := &m.keyEditor
	name := keyName(key)
	if name == "esc" || name == "ctrl+c" || len(matches) == 0 || (key.Type == tea.KeyRunes && len(key.Runes) != 1) {
		editor.capturing, editor.swap, editor.message = false, "", ""
		return m, nil
	}
	b := matches[selected]
	if editor.swap != "" {
		if name == "enter" {
			name = editor.swap
		} else {
			editor.capturing, editor.swap, editor.message = false, "", ""
			return m, nil
		}
	}
	note := ""
	if editor.swap == "" {
		same, shadowed, prefixes := activeKeys.conflicts(b, name)
		if len(prefixes) > 0 {
			editor.capturing = false
			editor.message = fmt.Sprintf("%s starts %s; choose another key", keyLabel(name), describe(prefixes))
			return m, nil
		}
		if len(same) > 0 {
			editor.swap = name
			editor.message = fmt.Sprintf("%s is bound to %s: ↵ take it, esc cancel", keyLabel(name), describe(same))
			return m, nil
		}
		if len(shadowed) > 0 {
			note = " · also bound to " + describe(shadowed)
		}
	}
	editor.capturing, editor.swap = false, ""
	activeKeys.rebind(b, name, editor.adding)
	editor.message = m.saveKeys(fmt.Sprintf("%s: %s%s", b.description, bindingKeys(b.id()), note))
	return m, nil
}

// saveKeys persists the bindings and returns status for the editor.
func (m model) saveKeys(status string) string {
	if err := activeKeys.save(keysPath()); err != nil {
		return "Could not save keys: " + err.Error()
	}
	return status
}

// describe names bindings for a status line.
func describe(bindings []binding) string {
	names := make([]string, len(bindings))
	for i, b := range bindings {
		names[i] = b.description
		if b.scope != scopeShared {
			names[i] += " (" + scopeTitles[b.scope] + ")"
		}
	}
	return strings.Join(names, ", ")
}

// related reports whether two scopes are ever active together.
func related(a, b string) bool { return a == b || a == scopeShared || b == scopeShared }

// conflicts finds the bindings that key would collide with if bound to b:
// those in the same scope with the same key, those in an overlapping scope
// with the same key, which win or lose by scope, and sequences key would
// start, which would leave key waiting for more.
func (k *keymap) conflicts(b binding, key string) (same, shadowed, prefixes []binding) {
	for _, other := range defaultBindings {
		if other.id() == b.id() || !related(b.scope, other.scope) {
			continue
		}
		for _, existing := range k.keys[other.id()] {
			switch {
			case existing == key && other.scope == b.scope:
				same = append(same, other)
			case existing == key:
				shadowed = append(shadowed, other)
			case strings.HasPrefix(existing, key+" "):
				prefixes = append(prefixes, other)
			default:
				continue
			}
			break
		}
	}
	return same, shadowed, prefixes
}

// rebind binds key to b, replacing its first key or adding key after the
// first. Bindings in the same scope give key up; one left with no key takes
// the key b gave up, so the two swap.
func (k *keymap) rebind(b binding, key string, add bool) {
	keys := slices.Clone(k.keys[b.id()])
	keys = slices.DeleteFunc(keys, func(existing string) bool { return existing == key })
	replaced := ""
	switch {
	case add:
		keys = append(keys, key)
	case len(keys) > 0 && slices.Contains(k.keys[b.id()], key):
		keys = append([]string{key}, keys...)
	case len(keys) > 0:
		replaced = keys[0]
		keys[0] = key
	default:
		keys = []string{key}
	}
	k.keys[b.id()] = keys
	same, _, _ := k.conflicts(b, key)
	for _, other := range same {
		left := slices.DeleteFunc(slices.Clone(k.keys[other.id()]), func(existing string) bool { return existing == key })
		if len(left) == 0 && replaced != "" {
			left = []string{replaced}
		}
		k.keys[other.id()] = left
	}
	k.index()
}

// showKeyEditor draws the key editor in place of the shortcuts list.
func (m model) showKeyEditor(lines []string) {
	if m.width < 8 || len(lines) < 6 {
		return
	}
	boxWidth := min(helpBoxWidth, m.width-2)
	inner := max(1, boxWidth-4)
	editor := m.keyEditor
	matches := m.editorBindings()
	visible := max(1, helpVisibleRows(len(lines))-1)
	selected := min(editor.list.selected, max(0, len(matches)-1))
	offset, _ := listViewport(editor.list, len(matches), visible)
	scopeWidth := 0
	for _, title := range scopeTitles {
		scopeWidth = max(scopeWidth, stringWidth(title))
	}
	var body []string
	switch {
	case editor.list.filtering:
		body = append(body, "/"+renderInput(editor.list.draft, editor.list.draftCursor, max(1, inner-1)))
	case editor.capturing && editor.swap == "":
		body = append(body, m.accent()+"Press a key for "+matches[selected].description+styleReset+styleDim+"  esc cancel"+styleReset)
	case editor.message != "":
		body = append(body, syntax.Clean(editor.message))
	case editor.list.filter != "":
		body = append(body, styleDim+"/"+syntax.Clean(editor.list.filter)+styleReset)
	default:
		path := keysPath()
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(filepath.Separator)) {
			path = "~" + path[len(home):]
		}
		body = append(body, styleDim+"Saved to "+path+styleReset)
	}
	for i := offset; i < min(len(matches), offset+visible); i++ {
		b := matches[i]
		keys := bindingKeys(b.id())
		if keys == "" {
			keys = "unbound"
		}
		if !slices.Equal(activeKeys.keys[b.id()], b.keys) {
			keys += " *"
		}
		scope := scopeTitles[b.scope]
		marker := "  "
		if i == selected {
			marker = m.accent() + "› " + styleReset
		}
		descWidth := max(1, inner-2-scopeWidth-2-stringWidth(keys)-2)
		description := sliceWidth(b.description, 0, descWidth)
		row := marker + styleDim + scope + strings.Repeat(" ", scopeWidth-stringWidth(scope)) + styleReset + "  " +
			description + strings.Repeat(" ", max(0, descWidth-stringWidth(description))) + "  " + m.accent() + keys + styleReset
		body = append(body, row)
	}
	info := fmt.Sprintf("%d/%d", min(selected+1, len(matches)), len(matches))
	placeDialog(lines, m.width, dialog{
		width: boxWidth,
		title: "Customize keys",
		info:  info,
		body:  body,
		hint: keyHints(activeKeys.label("open"), "change", activeKeys.label("keys.add-key"), "add",
			activeKeys.label("keys.reset-key"), "reset", activeKeys.label("search"), "filter", activeKeys.label("back"), "back"),
	}.render())
}

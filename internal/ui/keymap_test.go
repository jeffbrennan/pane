package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds the key message a terminal sends for a bubbletea key name,
// such as "j", "enter" or "ctrl+d".
func keyMsg(name string) tea.KeyMsg {
	alt := false
	if rest, ok := cutAlt(name); ok {
		alt, name = true, rest
	}
	if name == " " || name == "space" {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}
	}
	if len([]rune(name)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name), Alt: alt}
	}
	for t := tea.KeyType(-200); t < 200; t++ {
		if t != tea.KeyRunes && (tea.Key{Type: t}).String() == name {
			return tea.KeyMsg{Type: t, Alt: alt}
		}
	}
	panic("unknown key " + name)
}

func cutAlt(name string) (string, bool) {
	if len(name) > 4 && name[:4] == "alt+" {
		return name[4:], true
	}
	return name, false
}

// navigate sends keys through the diff motions of the changes screen.
func (m *model) navigate(keys ...string) bool {
	handled := false
	for _, key := range keys {
		handled = m.handleDiffNavigation(m.press(keyMsg(key), scopeChanges)) || m.pending != "" || m.count > 0
	}
	return handled
}

// useKeys swaps in a keymap for one test and a config directory it may
// write to.
func useKeys(t *testing.T, overrides map[string][]string) string {
	t.Helper()
	previous := activeKeys
	t.Cleanup(func() { activeKeys = previous })
	activeKeys = newKeymap(overrides)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "pane", "keys")
}

func send(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, key := range keys {
		updated, _ := m.Update(keyMsg(key))
		m = updated.(model)
	}
	return m
}

func TestDefaultBindingsHaveUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range defaultBindings {
		if seen[b.id()] {
			t.Errorf("duplicate binding %s", b.id())
		}
		seen[b.id()] = true
		if _, ok := scopeTitles[b.scope]; !ok {
			t.Errorf("%s: untitled scope", b.id())
		}
	}
}

func TestCustomKeyReplacesDefault(t *testing.T) {
	useKeys(t, map[string][]string{"wrap": {"alt+w"}})
	m := model{width: 40, height: 10}
	if m = send(t, m, "w"); m.wrap {
		t.Fatal("the default key still wraps")
	}
	if m = send(t, m, "alt+w"); !m.wrap {
		t.Fatal("the custom key did not wrap")
	}
}

func TestCustomSequenceAndCount(t *testing.T) {
	useKeys(t, map[string][]string{"down": {"space j"}, "changes.stage-file": {"s s"}})
	m := model{width: 40, height: 12, rows: make([]row, 20)}
	if !m.navigate(" ") || m.pending != "space" {
		t.Fatalf("space did not start a sequence: %q", m.pending)
	}
	m.navigate("j")
	if m.pos != 1 {
		t.Fatalf("custom sequence moved to %d", m.pos)
	}
	m.navigate("3", " ", "j")
	if m.pos != 4 {
		t.Fatalf("count with a custom sequence moved to %d", m.pos)
	}
	if m.navigate("j") || m.pos != 4 {
		t.Fatal("the replaced default key still moves")
	}
}

func TestPendingSequenceFallsBackToItsLastKey(t *testing.T) {
	useKeys(t, nil)
	m := model{width: 40, height: 12, rows: make([]row, 20)}
	m.navigate("g", "j")
	if m.pos != 1 || m.pending != "" {
		t.Fatalf("g j: pos %d pending %q", m.pos, m.pending)
	}
}

func TestKeysConfigRoundTrip(t *testing.T) {
	path := useKeys(t, nil)
	activeKeys.keys["search"] = []string{"ctrl+f", ","}
	activeKeys.keys["changes.stage-file"] = []string{"space", "g ,"}
	activeKeys.keys["blame"] = nil
	if err := activeKeys.save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "\nwrap") || !strings.Contains(string(data), "changes.stage-file = space, g \\,") {
		t.Fatalf("unexpected config:\n%s", data)
	}
	loaded, err := loadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"search", "changes.stage-file", "blame", "wrap"} {
		if !slices.Equal(loaded.keys[id], activeKeys.keys[id]) {
			t.Errorf("%s: loaded %q, saved %q", id, loaded.keys[id], activeKeys.keys[id])
		}
	}
}

func TestKeysConfigReportsUnknownActions(t *testing.T) {
	path := useKeys(t, nil)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# comment\nnope = x\nwrap = alt+w\n"), 0o644)
	loaded, err := loadKeys(path)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown action not reported: %v", err)
	}
	if !slices.Equal(loaded.keys["wrap"], []string{"alt+w"}) {
		t.Fatal("valid lines were dropped")
	}
}

func TestHelpAndFootersShowCustomKeys(t *testing.T) {
	useKeys(t, map[string][]string{"changes.stage-hunk": {"alt+s"}})
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	if !strings.Contains(plain(strings.Join(m.helpLines(), "\n")), "alt-s") {
		t.Fatal("help does not show the custom key")
	}
}

func TestKeyEditorRebindsAndSaves(t *testing.T) {
	path := useKeys(t, nil)
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	m = send(t, m, "?", "e")
	if !m.keyEditor.open || !strings.Contains(m.View(), "Customize keys") {
		t.Fatal("key editor did not open")
	}
	m = send(t, m, "/", "w", "r", "a", "p", "enter", "ctrl+w")
	if got := activeKeys.keys["wrap"]; !slices.Equal(got, []string{"ctrl+w"}) {
		t.Fatalf("wrap bound to %q", got)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "wrap = ctrl+w") {
		t.Fatalf("not saved: %q %v", data, err)
	}
	m = send(t, m, "a", "alt+w")
	if got := activeKeys.keys["wrap"]; !slices.Equal(got, []string{"ctrl+w", "alt+w"}) {
		t.Fatalf("added key: %q", got)
	}
	m = send(t, m, "x")
	if got := activeKeys.keys["wrap"]; !slices.Equal(got, []string{"w"}) {
		t.Fatalf("reset: %q", got)
	}
	m = send(t, m, "esc", "esc", "esc")
	if m.modal != "" || m.keyEditor.open {
		t.Fatal("escape did not close the editor and dialog")
	}
}

func TestKeyEditorSwapsWithinAScope(t *testing.T) {
	useKeys(t, nil)
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	m = send(t, m, "?", "e", "/", "P", "u", "s", "h", "enter", "s")
	if view := plain(m.View()); !strings.Contains(view, "Bind s to Push (Changes)?") || !strings.Contains(view, "Stage / unstage hunk") {
		t.Fatalf("no swap prompt:\n%s", view)
	}
	m = send(t, m, "enter")
	if got := activeKeys.keys["changes.push"]; !slices.Equal(got, []string{"s"}) {
		t.Fatalf("push: %q", got)
	}
	if got := activeKeys.keys["changes.stage-hunk"]; !slices.Equal(got, []string{"p"}) {
		t.Fatalf("stage hunk did not take push's key: %q", got)
	}
	if got := activeKeys.keys["history.push"]; !slices.Equal(got, []string{"p"}) {
		t.Fatalf("another scope changed: %q", got)
	}
}

func TestKeyEditorConfirmsEndingSequences(t *testing.T) {
	useKeys(t, nil)
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	m = send(t, m, "?", "e", "/", "w", "r", "a", "p", "enter", "z")
	view := plain(m.View())
	if !strings.Contains(view, "Bind z to Toggle line wrapping?") || !strings.Contains(view, "Align the active line to the top · unbound") ||
		!strings.Contains(view, "Scroll left · keeps backspace") {
		t.Fatalf("no confirmation listing the sequences:\n%s", view)
	}
	m = send(t, m, "esc")
	if !slices.Equal(activeKeys.keys["wrap"], []string{"w"}) || !slices.Equal(activeKeys.keys["align-top"], []string{"z t"}) {
		t.Fatal("cancel changed keys")
	}
	m = send(t, m, "enter", "z", "enter")
	if !slices.Equal(activeKeys.keys["wrap"], []string{"z"}) || len(activeKeys.keys["align-top"]) != 0 ||
		!slices.Equal(activeKeys.keys["scroll-left"], []string{"ctrl+h", "backspace"}) {
		t.Fatalf("wrap %q, align-top %q, scroll-left %q", activeKeys.keys["wrap"], activeKeys.keys["align-top"], activeKeys.keys["scroll-left"])
	}
	if m.keyEditor.swap != "" || m.keyEditor.capturing {
		t.Fatal("confirmation stayed open")
	}
}

func TestKeyEditorGroupsByScopeAndKeepsItsViewport(t *testing.T) {
	useKeys(t, nil)
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	m = send(t, m, "?", "e")
	view := plain(m.View())
	if strings.Count(view, "Everywhere") != 1 {
		t.Fatalf("scope should be a single header:\n%s", view)
	}
	m = send(t, m, "G")
	if !strings.Contains(plain(m.View()), "Key editor") {
		t.Fatal("last scope's header is not shown")
	}
	top := m.keyEditor.top
	m = send(t, m, "k", "k", "k")
	if m.keyEditor.top != top {
		t.Fatalf("viewport moved from %d to %d while the cursor stayed in view", top, m.keyEditor.top)
	}
	m = send(t, m, "g", "g")
	if m.keyEditor.top != 0 {
		t.Fatalf("top did not show the first header: %d", m.keyEditor.top)
	}
}

func TestKeyEditorRemovesKeys(t *testing.T) {
	path := useKeys(t, nil)
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	m = send(t, m, "?", "e", "/", "w", "r", "a", "p", "enter", "esc", "d")
	if got := activeKeys.keys["wrap"]; len(got) != 0 {
		t.Fatalf("single key not removed: %q", got)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "wrap = ") {
		t.Fatalf("unbinding not saved: %q %v", data, err)
	}
	if loaded, _ := loadKeys(path); len(loaded.keys["wrap"]) != 0 {
		t.Fatalf("unbinding did not survive a reload: %q", loaded.keys["wrap"])
	}
	m = send(t, m, "esc", "/", "S", "c", "r", "o", "l", "l", " ", "l", "e", "f", "t", "enter", "esc")
	if got := bindingKeys("scroll-left"); got != "zh, z backspace, backspace" {
		t.Fatalf("twins not merged: %q", got)
	}
	m = send(t, m, "d")
	if !m.keyEditor.removing {
		t.Fatal("several keys should ask which to remove")
	}
	m = send(t, m, "3")
	if got := activeKeys.keys["scroll-left"]; !slices.Equal(got, []string{"z h", "z ctrl+h", "z backspace"}) {
		t.Fatalf("backspace and its ctrl+h twin not both removed: %q", got)
	}
}

func TestKeyEditorHeaderSticks(t *testing.T) {
	useKeys(t, nil)
	m := model{width: 100, height: 30, rows: []row{{text: "x"}}}
	m = send(t, m, "?", "e", "ctrl+d", "ctrl+d")
	view := plain(m.View())
	if !strings.Contains(view, "Everywhere ─") || strings.Contains(view, "› Down ") {
		t.Fatalf("header did not stick once scrolled:\n%s", view)
	}
}

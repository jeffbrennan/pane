package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestShortcutDialog(t *testing.T) {
	m := model{width: 60, height: 20, rows: []row{{text: "background"}}}
	if strings.Contains(m.View(), "j/k scroll") {
		t.Fatal("persistent shortcuts visible")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	got := updated.(model)
	view := got.View()
	if got.modal != "help" || !strings.Contains(view, "Keyboard shortcuts") || !strings.Contains(view, "Stage / unstage file") || strings.Contains(view, "background") {
		t.Fatalf("invalid help view: %q", view)
	}
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).modal != "" || !strings.Contains(updated.(model).View(), "background") {
		t.Fatal("help did not close")
	}
}

func TestShortcutDialogScrollsInNarrowPane(t *testing.T) {
	m := model{width: 20, height: 8, modal: "help"}
	for i := 0; i < len(shortcutLines(m.width, "")); i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = updated.(model)
	}
	if want := len(shortcutLines(m.width, "")) - helpVisibleRows(m.height); m.helpPos != want {
		t.Fatalf("help stopped at %d, want %d", m.helpPos, want)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if updated.(model).helpPos != m.helpPos-1 {
		t.Fatal("scroll up failed")
	}
}

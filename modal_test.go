package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommitDialogCenteredAndEditable(t *testing.T) {
	m := model{width: 60, height: 20, wrap: true, modal: "commit", input: "a commit"}
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 20 || !strings.Contains(lines[6], "┌") || !strings.Contains(view, "Commit changes") || !strings.Contains(view, "a commit") {
		t.Fatalf("dialog not centered: %q", view)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).modal != "" {
		t.Fatal("escape did not dismiss dialog")
	}
}

func TestCommitDialogLongInputAndSmallTerminal(t *testing.T) {
	m := model{width: 24, height: 9, modal: "commit", input: "a very long commit message 世界"}
	view := m.View()
	if !strings.Contains(view, "世界") || !strings.Contains(view, "\x1b[7m \x1b[0m") {
		t.Fatalf("input cursor not visible: %q", view)
	}
	m.width, m.height = 20, 6
	view = m.View()
	if !strings.Contains(view, "Commit message") {
		t.Fatalf("missing compact fallback: %q", view)
	}
}

func TestCommitDialogValidation(t *testing.T) {
	m := model{width: 60, height: 20, modal: "commit"}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if cmd != nil || got.modal != "commit" || !strings.Contains(got.View(), "Commit message cannot be empty") {
		t.Fatalf("expected validation in dialog: %#v", got)
	}
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if updated.(model).message != "" {
		t.Fatal("validation not cleared on edit")
	}
}

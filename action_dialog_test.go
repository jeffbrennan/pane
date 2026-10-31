package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPushConfirmationDialog(t *testing.T) {
	m := model{width: 70, height: 20, rows: []row{{text: "background diff"}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	view := m.View()
	if cmd != nil || m.modal != "push" || !strings.Contains(view, "Push changes?") || !strings.Contains(view, "configured upstream") || strings.Contains(view, "background diff") {
		t.Fatalf("unexpected push confirmation: %q", view)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || updated.(model).modal != "" {
		t.Fatal("cancel did not dismiss confirmation")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !updated.(model).busy || updated.(model).modal != "" {
		t.Fatal("confirmation did not start action")
	}
}

func TestActionResultDialog(t *testing.T) {
	m := model{width: 70, height: 20, busy: true}
	updated, _ := m.Update(completed{action: "push", message: "To example\n   abc..def  main -> main"})
	m = updated.(model)
	view := m.View()
	if m.modal != "result" || !strings.Contains(view, "Push complete") || !strings.Contains(view, "To example") || !strings.Contains(view, "abc..def") || strings.Contains(view, " | ") {
		t.Fatalf("unexpected result dialog: %q", view)
	}
	if strings.Contains(view, "j/k") {
		t.Fatal("scroll hint shown when output fits")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(model).modal != "" {
		t.Fatal("result did not close")
	}
}

func TestActionResultScrollAndError(t *testing.T) {
	m := model{width: 40, height: 10, modal: "result", resultAction: "pull", resultText: "one\ntwo\nthree\nfour\nfive\nsix"}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(model)
	if m.resultPos != 1 || !strings.Contains(m.View(), "five") || !strings.Contains(m.View(), "j/k") {
		t.Fatalf("result did not scroll: %q", m.View())
	}
	updated, _ = m.Update(completed{action: "pull", err: errors.New("network failed")})
	got := updated.(model)
	if got.modal != "error" || got.errorText != "network failed" {
		t.Fatal("errors should remain errors")
	}
}

func TestActionDialogOnSmallPane(t *testing.T) {
	m := model{width: 18, height: 5, modal: "push"}
	if !strings.Contains(m.View(), "Push changes?") {
		t.Fatal("missing small-pane fallback")
	}
}

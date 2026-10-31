package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPushPullStartImmediately(t *testing.T) {
	for _, screen := range []string{"", "history"} {
		for _, key := range []string{"p", "P"} {
			m := model{screen: screen}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			got := updated.(model)
			if cmd == nil || !got.busy || got.modal != "" {
				t.Errorf("screen %q key %q did not start action: %#v", screen, key, got)
			}
		}
	}
}

func TestActionResultDialog(t *testing.T) {
	m := model{width: 70, height: 20, busy: true}
	updated, _ := m.Update(completed{action: "pull", message: "From example\n   abc..def  main -> main"})
	m = updated.(model)
	view := m.View()
	if m.modal != "result" || !strings.Contains(view, "Pull complete") || !strings.Contains(view, "From example") || !strings.Contains(view, "abc..def") || strings.Contains(view, " | ") {
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
	m := model{width: 40, height: 10, modal: "result", resultText: "one\ntwo\nthree\nfour\nfive\nsix"}
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
	m := model{width: 18, height: 5, modal: "result"}
	if !strings.Contains(m.View(), "Pull complete") {
		t.Fatal("missing small-pane fallback")
	}
}

func TestSuccessfulPushReturnsToHistory(t *testing.T) {
	tests := []struct {
		name   string
		screen string
	}{
		{name: "diff"},
		{name: "history", screen: "history"},
		{name: "commit", screen: "commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{screen: tt.screen, busy: true, history: historyState{filter: "query", selected: 2}}
			updated, cmd := m.Update(completed{action: "push", message: "Pushed"})
			got := updated.(model)
			if got.screen != "history" || got.modal != "" || got.busy || !got.loading || cmd == nil {
				t.Fatalf("push did not return to refreshed history: %#v", got)
			}
			if tt.screen == "history" && (got.history.filter != "query" || got.history.selected != 2) {
				t.Fatal("push discarded the current history selection")
			}
		})
	}
}

func TestFailedPushStillShowsError(t *testing.T) {
	m := model{screen: "history", busy: true}
	updated, _ := m.Update(completed{action: "push", err: errors.New("push failed")})
	got := updated.(model)
	if got.modal != "error" || got.errorText != "push failed" {
		t.Fatal("push error was hidden")
	}
}

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

func TestPushPullProgressInSummary(t *testing.T) {
	for _, tt := range []struct{ key, status string }{{"p", "Pushing…"}, {"P", "Pulling…"}} {
		m := model{width: 60, height: 20, screen: "history", snapshot: snapshot{branch: branchStatus{available: true, upstream: "origin/main", countsKnown: true, ahead: 1}}}
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		m = updated.(model)
		if cmd == nil || !strings.Contains(m.View(), tt.status) {
			t.Errorf("%q did not show progress in summary", tt.key)
		}
		updated, _ = m.Update(completed{action: m.activeAction, err: errors.New("failed")})
		m = updated.(model)
		if m.activeAction != "" || m.busy || m.modal != "error" {
			t.Error("failed action did not clear progress and show an error")
		}
	}
}

func TestPushWithoutAheadCommitsShowsChecking(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", snapshot: snapshot{branch: branchStatus{available: true, upstream: "origin/main", countsKnown: true}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	got := updated.(model)
	if cmd == nil || !strings.Contains(got.View(), "Checking push…") || strings.Contains(got.View(), "Pushing…") {
		t.Fatal("push with no ahead commits should show that the remote is being checked")
	}
}

func TestFailedPullShowsError(t *testing.T) {
	m := model{width: 40, height: 10, busy: true}
	updated, _ := m.Update(completed{action: "pull", err: errors.New("network failed")})
	got := updated.(model)
	if got.modal != "error" || got.errorText != "network failed" {
		t.Fatal("errors should remain errors")
	}
}

func TestSuccessfulPullReturnsToHistory(t *testing.T) {
	for _, screen := range []string{"", "history", "commit"} {
		m := model{screen: screen, busy: true}
		updated, cmd := m.Update(completed{action: "pull", message: "Updated"})
		got := updated.(model)
		if cmd == nil || got.screen != "history" || got.modal != "" || got.busy || !got.loading || !got.history.loading {
			t.Errorf("pull from %q did not refresh the landing page: %#v", screen, got)
		}
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

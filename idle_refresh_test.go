package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIdleRefreshDependsOnScreen(t *testing.T) {
	for _, tc := range []struct {
		screen string
		path   string
		want   int
	}{
		{"", "", 60},
		{"base", "", 60},
		{"history", "", 20},
		{"history", "file", 0},
		{"commit", "", 0},
		{"sequence", "", 0},
		{"stashes", "", 0},
	} {
		t.Run(tc.screen+tc.path, func(t *testing.T) {
			m := model{screen: tc.screen, history: historyState{path: tc.path}}
			refreshes := 0
			for i := 0; i < 120; i++ {
				updated, _ := m.update(tick{})
				m = updated.(model)
				if m.loading {
					refreshes++
					m.loading = false
				}
			}
			if refreshes != tc.want {
				t.Fatalf("got %d refreshes, want %d", refreshes, tc.want)
			}
		})
	}
}

func TestLeavingHistoryRefreshesWorkingTree(t *testing.T) {
	m := model{screen: "history"}
	updated, cmd := m.handleHistoryKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)
	if got.screen != "" || !got.loading || cmd == nil {
		t.Fatal("leaving history should refresh the working tree")
	}
}

func TestReturningToHistoryRefreshesStatus(t *testing.T) {
	m := model{screen: "commit"}
	updated, cmd := m.returnToHistory()
	got := updated.(model)
	if got.screen != "history" || !got.loading || cmd == nil {
		t.Fatal("returning to history should refresh status")
	}
}

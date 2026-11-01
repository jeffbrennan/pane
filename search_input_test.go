package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/git"
)

func sendInput(t *testing.T, m model, keys ...tea.KeyMsg) model {
	t.Helper()
	for _, key := range keys {
		updated, _ := m.Update(key)
		m = updated.(model)
	}
	return m
}

func TestDiffSearchEditsAtCursor(t *testing.T) {
	m := keys(t, toolModel(), "/", "a", "c")
	m = sendInput(t, m, tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("界")})
	if m.search.draft != "a界c" || m.search.cursor != 2 || !strings.Contains(m.View(), "界\x1b[7mc\x1b[0m") {
		t.Fatalf("diff search: %#v, %q", m.search, m.View())
	}
	m = sendInput(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.search.draft != "c" || m.search.cursor != 0 {
		t.Fatalf("delete before cursor: %#v", m.search)
	}
}

func TestHistorySearchEditsWithoutLosingSelection(t *testing.T) {
	m := model{width: 50, height: 12, screen: "history", history: historyState{commits: []git.CommitInfo{{Subject: "abc"}, {Subject: "abc again"}}}}
	m = keys(t, m, "/", "a", "b", "c")
	m = sendInput(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyLeft})
	if m.history.selected != 1 || m.history.draftCursor != 2 {
		t.Fatalf("motion changed selection: %#v", m.history)
	}
	m = sendInput(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if m.history.draft != "abbc" || m.history.selected != 0 || !strings.Contains(m.View(), "abb\x1b[7mc\x1b[0m") {
		t.Fatalf("history search: %#v", m.history)
	}
}

func TestPickerFilterEditsAndKeepsNavigation(t *testing.T) {
	m := keys(t, toolModel(), "f", "/", "a", "c")
	m = sendInput(t, m, tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if m.picker.query != "abc" || m.picker.cursor != 2 || !strings.Contains(m.View(), "ab\x1b[7mc\x1b[0m") {
		t.Fatalf("picker input: %#v, %q", m.picker, m.View())
	}
	m = sendInput(t, m, tea.KeyMsg{Type: tea.KeyCtrlA}, tea.KeyMsg{Type: tea.KeyDelete})
	if m.picker.query != "bc" {
		t.Fatalf("picker deletion: %#v", m.picker)
	}
}

func TestInputRenderingKeepsCursorVisible(t *testing.T) {
	for _, width := range []int{1, 2, 4, 8} {
		for pos := 0; pos <= len([]rune("世界abcdef")); pos++ {
			got := renderInput("世界abcdef", pos, width)
			if ansi.StringWidth(got) > width || !strings.Contains(got, "\x1b[7m") {
				t.Fatalf("width %d pos %d: %q", width, pos, got)
			}
		}
	}
}

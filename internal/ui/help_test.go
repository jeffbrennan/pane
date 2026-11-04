package ui

import (
	"os"
	"path/filepath"
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
	for i := 0; i < len(m.helpLines()); i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = updated.(model)
	}
	if want := len(m.helpLines()) - helpVisibleRows(m.height); m.help.selected != want {
		t.Fatalf("help stopped at %d, want %d", m.help.selected, want)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if updated.(model).help.selected != m.help.selected-1 {
		t.Fatal("scroll up failed")
	}
}

func TestHelpShowsCurrentScreenFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    model
		want string
	}{
		{"changes", model{width: 80, height: 30}, "Changes"},
		{"history", model{width: 80, height: 30, screen: "history"}, "History"},
		{"branches", model{width: 80, height: 30, screen: "branches"}, "Branches"},
		{"commit input", model{width: 80, height: 30, modal: "commit"}, "Commit input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := tc.m.helpLines()
			if len(lines) == 0 || !strings.Contains(plain(lines[0]), tc.want) {
				t.Fatalf("first help line %q, want %q", plain(strings.Join(lines, "\n")), tc.want)
			}
		})
	}
}

func TestHelpFiltersRowsAndKeepsOtherScreensLast(t *testing.T) {
	m := model{width: 80, height: 30, screen: "history", help: listState{filter: "stash"}}
	lines := plain(strings.Join(m.helpLines(), "\n"))
	if !strings.Contains(lines, "Open stashes") || strings.Contains(lines, "Stage / unstage hunk") {
		t.Fatalf("filter did not narrow the rows: %q", lines)
	}
	if other := strings.Index(lines, "Other screens"); other < 0 || other < strings.Index(lines, "Open stashes") {
		t.Fatalf("other screens should come after the current one: %q", lines)
	}
}

// TestGuideContainsEveryShortcut keeps the guide's key tables from drifting
// away from the in-app help.
func TestGuideContainsEveryShortcut(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "guide.md")
	guide, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, section := range shortcutSections {
		for _, shortcut := range section.keys {
			if !strings.Contains(string(guide), shortcut.keys) {
				t.Errorf("%s: %q is missing from the guide", section.title, shortcut.keys)
			}
		}
	}
}

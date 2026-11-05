package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestDisplayPath(t *testing.T) {
	for _, tt := range []struct{ path, want string }{
		{"file.go", "file.go"},
		{"dir/file.go", "dir/file.go"},
		{"one/two/file.go", "…/two/file.go"},
		{"one/ü/file.go", "…/ü/file.go"},
	} {
		if got := displayPath(tt.path, true); got != tt.want {
			t.Errorf("displayPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
		if got := displayPath(tt.path, false); got != tt.path {
			t.Errorf("full path = %q, want %q", got, tt.path)
		}
	}
}

func TestShortPathsToggleInDiffViews(t *testing.T) {
	c := git.Change{OldPath: "old/root/file.go", Path: "new/parent/file.go", Diff: "similarity index 100%\n"}
	for _, screen := range []string{"", "commit", "sequence"} {
		t.Run(map[string]string{"": "working", "commit": "commit", "sequence": "sequence"}[screen], func(t *testing.T) {
			m := model{width: 80, height: 12, wrap: true, screen: screen}
			switch screen {
			case "":
				m.snapshot.Unstaged = []git.Change{c}
				m.rebuild()
			case "commit":
				m.history.changes = []git.Change{c}
				m.rebuildCommit()
			case "sequence":
				m.sequence = sequenceState{commits: []git.CommitInfo{{Hash: "abcd"}}, changes: [][]git.Change{{c}}, states: []sequenceLoadState{sequenceReady}}
				m.rebuildSequence()
			}
			key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("~")}
			updated, cmd := m.Update(key)
			m = updated.(model)
			if cmd != nil || !m.shortPaths {
				t.Fatalf("toggle: %#v", m)
			}
			view := m
			if screen == "commit" {
				view = m.commitModel()
			}
			if len(view.rows) < 2 || !strings.Contains(view.rows[0].text, "…/root/file.go") || !strings.Contains(view.rows[1].text, "…/parent/file.go") {
				t.Fatalf("short rows: %#v", view.rows)
			}
			if view.rows[0].path != c.Path || view.rows[0].oldPathValue() != c.OldPath {
				t.Fatalf("paths changed: %#v", view.rows[0])
			}
			if len(filterFiles(view.fileEntries(), "old/root")) != 1 {
				t.Fatal("picker cannot search old full path")
			}
			if index, _ := view.findMatch("new/parent", -1, 1); index != 0 {
				t.Fatalf("full path search matched row %d", index)
			}
			updated, _ = m.Update(key)
			m = updated.(model)
			if screen == "commit" {
				view = m.commitModel()
			} else {
				view = m
			}
			if m.shortPaths || !strings.Contains(view.rows[0].text, c.OldPath) {
				t.Fatalf("full paths not restored: %#v", view.rows)
			}
		})
	}
}

func TestShortPinnedRenameUsesDestination(t *testing.T) {
	c := git.Change{OldPath: "old/root/file.go", Path: "new/parent/file.go", Diff: "@@ -1 +1 @@\n-old\n+new\n"}
	m := model{width: 40, height: 8, wrap: true, shortPaths: true, snapshot: git.Snapshot{Unstaged: []git.Change{c}}}
	m.rebuild()
	pinned := m.fileHeaderAt(len(m.rows) - 1)
	if pinned == nil || !strings.Contains(pinned.text, "…/parent/file.go") {
		t.Fatalf("pinned header: %#v", pinned)
	}
}

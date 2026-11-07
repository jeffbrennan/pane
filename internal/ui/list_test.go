package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

// TestListScreensShareMotionsAndFilters sends one key sequence to every list
// screen. It is the guard that keeps the shared list grammar from drifting.
func TestListScreensShareMotionsAndFilters(t *testing.T) {
	type screen struct {
		name  string
		start func() model
		send  func(model, tea.KeyMsg) model
		list  func(model) listState
	}
	screens := []screen{
		{
			name: "history",
			start: func() model {
				commits := make([]git.CommitInfo, 10)
				for i := range commits {
					commits[i] = git.CommitInfo{Hash: fmt.Sprintf("hash%d", i), Subject: fmt.Sprintf("commit %d", i)}
				}
				return model{width: 80, height: 20, screen: "history", history: historyState{commits: commits}}
			},
			send: func(m model, key tea.KeyMsg) model {
				updated, _ := m.handleHistoryKey(key)
				return updated.(model)
			},
			list: func(m model) listState { return m.history.list },
		},
		{
			name: "picker",
			start: func() model {
				changes := make([]git.Change, 10)
				for i := range changes {
					changes[i] = git.Change{Path: fmt.Sprintf("file%02d.go", i), Diff: "@@ -0,0 +1 @@\n+line\n"}
				}
				m := model{width: 80, height: 20, wrap: true, snapshot: git.Snapshot{Unstaged: changes}}
				m.rebuild()
				m.openPicker()
				return m
			},
			send: func(m model, key tea.KeyMsg) model {
				m.handlePickerKey(key)
				return m
			},
			list: func(m model) listState { return m.picker.list },
		},
		{
			name: "branches",
			start: func() model {
				entries := make([]git.BranchEntry, 10)
				for i := range entries {
					entries[i] = git.BranchEntry{Name: fmt.Sprintf("branch%02d", i), Ref: fmt.Sprintf("refs/heads/branch%02d", i)}
				}
				return model{width: 80, height: 20, screen: "branches", branch: branchState{entries: entries}}
			},
			send: func(m model, key tea.KeyMsg) model {
				updated, _ := m.handleBranchKey(key)
				return updated.(model)
			},
			list: func(m model) listState { return m.branch.list },
		},
		{
			name: "worktrees",
			start: func() model {
				entries := make([]git.WorktreeEntry, 10)
				for i := range entries {
					entries[i] = git.WorktreeEntry{Path: fmt.Sprintf("/tmp/tree%02d", i), Branch: fmt.Sprintf("branch%02d", i)}
				}
				return model{width: 80, height: 20, screen: "worktrees", worktrees: worktreeState{entries: entries, status: map[string]worktreeStatus{}}}
			},
			send: func(m model, key tea.KeyMsg) model {
				updated, _ := m.handleWorktreeKey(key)
				return updated.(model)
			},
			list: func(m model) listState { return m.worktrees.list },
		},
		{
			name: "stashes",
			start: func() model {
				entries := make([]git.StashEntry, 10)
				for i := range entries {
					entries[i] = git.StashEntry{Ref: fmt.Sprintf("stash@{%d}", i), Message: fmt.Sprintf("stash %d", i), Hash: fmt.Sprintf("hash%d", i)}
				}
				return model{width: 80, height: 20, screen: "stashes", stash: stashState{entries: entries}}
			},
			send: func(m model, key tea.KeyMsg) model {
				updated, _ := m.handleStashKey(key)
				return updated.(model)
			},
			list: func(m model) listState { return m.stash.list },
		},
		{
			name: "hotspots",
			start: func() model {
				authors := make(map[string]*hotAuthor, 10)
				for i := 0; i < 10; i++ {
					name := fmt.Sprintf("author%02d", i)
					authors[name] = &hotAuthor{name: name, commits: 10 - i, churn: 100 - i, dirs: map[string]int{"src": 1}}
				}
				return model{width: 80, height: 20, screen: "hotspots", hotspots: hotspotState{mode: "authors", authors: authors}}
			},
			send: func(m model, key tea.KeyMsg) model {
				updated, _ := m.handleHotspotKey(key)
				return updated.(model)
			},
			list: func(m model) listState { return m.hotspots.list },
		},
	}
	for _, tc := range screens {
		t.Run(tc.name, func(t *testing.T) {
			send := func(m model, keys ...string) model {
				for _, name := range keys {
					var key tea.KeyMsg
					switch name {
					case "enter":
						key = tea.KeyMsg{Type: tea.KeyEnter}
					case "esc":
						key = tea.KeyMsg{Type: tea.KeyEsc}
					case "ctrl+d":
						key = tea.KeyMsg{Type: tea.KeyCtrlD}
					default:
						key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
					}
					m = tc.send(m, key)
				}
				return m
			}
			m := tc.start()
			m = send(m, "5", "j")
			if got := tc.list(m).selected; got != 5 {
				t.Fatalf("count motion selected %d, want 5", got)
			}
			m = send(m, "g", "g")
			if got := tc.list(m).selected; got != 0 {
				t.Fatalf("gg selected %d, want 0", got)
			}
			m = send(m, "G")
			if got := tc.list(m).selected; got != 9 {
				t.Fatalf("G selected %d, want 9", got)
			}
			m = send(m, "ctrl+d")
			if got := tc.list(m).selected; got != 9 {
				t.Fatalf("half page past the end selected %d, want 9", got)
			}
			m = send(m, "g", "g", "/", "0")
			if l := tc.list(m); !l.filtering || l.filter != "0" {
				t.Fatalf("filter input: filtering=%t filter=%q", l.filtering, l.filter)
			}
			m = send(m, "esc")
			if l := tc.list(m); l.filter != "" || l.filtering {
				t.Fatalf("escape did not clear the filter: %#v", l)
			}
			m = send(m, "/", "0", "enter")
			if tc.list(m).filtering {
				t.Fatal("enter did not end filtering")
			}
		})
	}
}

func TestStyledRowRestoresRowStyleAfterColors(t *testing.T) {
	m := model{width: 40}
	row := m.styledRow(1, 0, 3, "a\x1b[31mred"+styleReset+"after", "")
	if !strings.Contains(row, styleReset+styleDim+"after") {
		t.Fatalf("text after a color should stay dim: %q", row)
	}
	selected := m.styledRow(0, 0, 3, "a\x1b[31mred"+styleReset+"after", "")
	if !strings.Contains(selected, styleReset+"\x1b[7mafter") {
		t.Fatalf("text after a color should stay selected: %q", selected)
	}
}

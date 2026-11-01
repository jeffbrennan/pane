package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestCommitDialogCenteredAndEditable(t *testing.T) {
	m := model{width: 60, height: 20, wrap: true, modal: "commit", input: "a commit", inputCursor: 8}
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 20 || !strings.Contains(view, "┌") || !strings.Contains(view, "Commit changes") || !strings.Contains(view, "a commit") {
		t.Fatalf("dialog not centered: %q", view)
	}
	if strings.Contains(view, "Nothing staged?") {
		t.Fatalf("unwanted stage-all help: %q", view)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !updated.(model).inputNormal {
		t.Fatal("escape did not enter normal mode")
	}
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).modal != "" {
		t.Fatal("escape in normal mode did not dismiss dialog")
	}
}

func TestCommitDialogLongInputAndSmallTerminal(t *testing.T) {
	m := model{width: 24, height: 9, modal: "commit", input: "a very long commit message 世界", inputCursor: len([]rune("a very long commit message 世界"))}
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

func TestCommitDialogHidesBackground(t *testing.T) {
	m := model{width: 60, height: 20, modal: "commit", wrap: true, rows: []row{{text: "first", kind: '+'}, {text: "second", kind: '-'}}}
	lines := strings.Split(m.View(), "\n")
	if lines[1] != "" || lines[2] != "" {
		t.Fatalf("diff is not hidden: %q %q", lines[1], lines[2])
	}
	if !strings.Contains(m.View(), "┌") {
		t.Fatalf("dialog is missing: %q", lines[6])
	}
	m.modal = ""
	if !strings.Contains(m.View(), "first") || !strings.Contains(m.View(), "second") {
		t.Fatal("diff remains hidden after closing dialog")
	}
}

func TestCommitDialogRequiresChanges(t *testing.T) {
	for _, tt := range []struct {
		name     string
		snapshot git.Snapshot
		wantOpen bool
	}{
		{name: "clean"},
		{name: "unstaged", snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "file"}}}, wantOpen: true},
		{name: "staged", snapshot: git.Snapshot{Staged: []git.Change{{Path: "file"}}}, wantOpen: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := model{width: 60, height: 20, snapshot: tt.snapshot}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
			got := updated.(model)
			if tt.wantOpen {
				if got.modal != "commit" || cmd == nil {
					t.Fatal("commit dialog did not open for changes")
				}
			} else if got.modal != "" || cmd != nil || !strings.Contains(got.View(), "Nothing to commit") {
				t.Fatalf("clean repository should show a status message: %#v", got)
			}
		})
	}
}

func TestCommitDialogLoadsRecentCommits(t *testing.T) {
	r := testRepo(t)
	for _, subject := range []string{"first", "second", "third", "fourth", "fifth", "sixth"} {
		if _, err := r.Action("commit", "--allow-empty", "-m", subject); err != nil {
			t.Fatal(err)
		}
	}
	m := model{repo: r, width: 60, height: 20, snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "file"}}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(model)
	if cmd == nil || !strings.Contains(m.View(), "Loading…") {
		t.Fatal("recent history was not loaded asynchronously")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	view := m.View()
	if strings.Contains(view, "Recent commits") || !strings.Contains(view, "sixth") || !strings.Contains(view, "second") || strings.Contains(view, "first") || strings.Index(view, "sixth") > strings.Index(view, "second") {
		t.Fatalf("unexpected recent commits: %q", view)
	}
	if strings.Index(view, "└") > strings.Index(view, "sixth") || !strings.Contains(view, "ago · Test") {
		t.Fatalf("recent history should be below the editor with age and author: %q", view)
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "sixth") {
			if !strings.HasPrefix(line, "   sixth") || i+2 >= len(lines) || strings.TrimSpace(lines[i+1]) != "" || !strings.Contains(lines[i+2], "fifth") {
				t.Fatalf("recent commits should be aligned and spaced: %q", view)
			}
			break
		}
	}
	m = keys(t, m, "h", "i")
	if m.input != "hi" {
		t.Fatal("recent commits interfered with message entry")
	}
}

func TestRelativeCommitTime(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		age  time.Duration
		want string
	}{
		{5 * time.Second, "5s ago"}, {2 * time.Minute, "2m ago"},
		{3 * time.Hour, "3h ago"}, {4 * 24 * time.Hour, "4d ago"},
		{2 * 7 * 24 * time.Hour, "2w ago"}, {2 * 30 * 24 * time.Hour, "2mo ago"},
		{2 * 365 * 24 * time.Hour, "2y ago"}, {-time.Hour, "0s ago"},
	} {
		date := now.Add(-tt.age).Format(time.RFC3339)
		if got := git.RelativeCommitTime(date, now); got != tt.want {
			t.Errorf("age %v: got %q, want %q", tt.age, got, tt.want)
		}
	}
}

func TestCommitDialogRecentHistoryEmptyAndStale(t *testing.T) {
	r := testRepo(t)
	m := model{repo: r, width: 60, height: 15, snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "file"}}}}
	updated, oldCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = keys(t, updated.(model), "esc", "esc")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = updated.(model)
	updated, _ = m.Update(oldCmd())
	m = updated.(model)
	if !m.recentCommitLoading {
		t.Fatal("stale history response changed the new dialog")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if !strings.Contains(m.View(), "No previous commits") {
		t.Fatalf("empty repository: %q", m.View())
	}
}

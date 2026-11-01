package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

func TestNavigationAndRefreshAnchor(t *testing.T) {
	m := model{width: 20, height: 10, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "one", Diff: "@@ -0,0 +1,2 @@\n+first\n+second\n"}, {Path: "two", Diff: "@@ -0,0 +1 @@\n+other\n"}}}}
	m.rebuild()
	m.jump(1)
	if m.rows[m.pos].path != "two" {
		t.Fatal("next file")
	}
	m.jump(-1)
	if m.rows[m.pos].path != "one" || m.pos != 0 {
		t.Fatal("previous file")
	}
	m.move(2)
	m.rebuild()
	if m.rows[m.pos].path != "one" || m.rows[m.pos].lineNumber() != 2 {
		t.Fatal("refresh anchor")
	}
}

func TestFileHeadersSeparateChangesAndFileJumps(t *testing.T) {
	m := model{width: 24, height: 12, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{
		{Path: "first.go", Diff: "@@ -0,0 +1 @@\n+first\n"},
		{Path: "second.go", Diff: "@@ -0,0 +1 @@\n+second\n"},
	}}}
	m.rebuild()
	m.jump(1)
	if m.rows[m.pos].kind != 'f' || m.rows[m.pos].path != "second.go" || m.rows[m.pos-1].kind != 's' {
		t.Fatalf("file jump or spacer: %#v", m.rows)
	}
	view := m.View()
	if !strings.Contains(view, "\x1b[1;37;48;5;236m first.go") || !strings.Contains(view, "\x1b[1;37;48;5;236m second.go") {
		t.Fatalf("missing file header bars: %q", view)
	}
	m.jump(-1)
	if m.pos != 0 || m.rows[m.pos].path != "first.go" {
		t.Fatal("previous file jump missed header")
	}
}

func TestActiveLineShowsContext(t *testing.T) {
	rows := make([]row, 20)
	for i := range rows {
		rows[i].text = fmt.Sprintf("line %02d", i)
	}
	m := model{width: 30, height: 10, wrap: true, rows: rows, pos: 7}
	m.reveal()
	view := m.View()
	if !strings.Contains(view, "line 03") || !strings.Contains(view, "▸ \x1b[0mline 07") || !strings.Contains(view, "line 10") {
		t.Fatalf("active line or context missing: %q", view)
	}
	if strings.Contains(view, "line 02") {
		t.Fatalf("viewport starts too early: %q", view)
	}
}

func TestWrappedActiveLineSurvivesRefreshAndResize(t *testing.T) {
	s := git.Snapshot{Unstaged: []git.Change{{Path: "long", Diff: "@@ -0,0 +1 @@\n+abcdefghijklmnopqrstuvwxyz0123456789\n"}}}
	m := model{width: 16, height: 10, wrap: true, snapshot: s}
	m.rebuild()
	if len(m.rows) < 4 {
		t.Fatalf("expected wrapped rows: %#v", m.rows)
	}
	m.pos = 3
	original := m.rows[m.pos]
	updated, _ := m.Update(loaded{snapshot: s, generation: m.generation})
	m = updated.(model)
	if got := m.rows[m.pos]; got.offset != original.offset || got.text != original.text {
		t.Fatalf("refresh changed wrapped segment: got %#v, want %#v", got, original)
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	m = updated.(model)
	got := m.rows[m.pos]
	if got.lineNumber() != original.lineNumber() || got.offset > original.offset || (m.pos+1 < len(m.rows) && m.rows[m.pos+1].lineNumber() == got.lineNumber() && m.rows[m.pos+1].offset <= original.offset) {
		t.Fatalf("resize lost position: got %#v, original %#v", got, original)
	}
}

func TestCommitWrappedLineSurvivesResize(t *testing.T) {
	m := model{width: 16, height: 10, wrap: true, screen: "commit", history: historyState{changes: []git.Change{{Path: "long", Diff: "@@ -0,0 +1 @@\n+abcdefghijklmnopqrstuvwxyz0123456789\n"}}}}
	m.rebuildCommit()
	m.history.pos = 3
	original := m.history.rows[m.history.pos]
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	m = updated.(model)
	got := m.history.rows[m.history.pos]
	if got.lineNumber() != original.lineNumber() || got.offset == 0 || got.offset > original.offset {
		t.Fatalf("commit resize lost position: %#v", got)
	}
}

func TestHeightOnlyResizeKeepsRows(t *testing.T) {
	for _, screen := range []string{"", "commit", "sequence"} {
		t.Run(screen, func(t *testing.T) {
			rows := []row{{path: "one", text: "first"}, {path: "one", text: "second"}}
			m := model{width: 80, height: 20, screen: screen, rows: rows}
			if screen == "commit" {
				m.history.rows = rows
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
			got := updated.(model)
			if screen == "commit" {
				got.rows = got.history.rows
			}
			if len(got.rows) != len(rows) || &got.rows[0] != &rows[0] {
				t.Fatal("height-only resize rebuilt rows")
			}
		})
	}
}

func TestLargeWidthResizeCoalesces(t *testing.T) {
	rows := make([]row, 50000)
	m := model{width: 80, height: 20, rows: rows, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "file", Diff: "@@ -0,0 +1 @@\n+abcdef\n"}}}}
	for _, width := range []int{70, 60, 50} {
		updated, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		m = updated.(model)
		if cmd == nil || &m.rows[0] != &rows[0] {
			t.Fatal("large width resize should defer rebuilding")
		}
	}
	updated, _ := m.Update(resizeRebuild{request: m.resizeRequest - 1})
	m = updated.(model)
	if &m.rows[0] != &rows[0] {
		t.Fatal("stale resize timer rebuilt rows")
	}
	updated, _ = m.Update(resizeRebuild{request: m.resizeRequest})
	m = updated.(model)
	if m.width != 50 || len(m.rows) == len(rows) || m.rows[0].path != "file" {
		t.Fatal("latest resize did not rebuild at the latest width")
	}
}

func TestStartupViewSelection(t *testing.T) {
	tests := []struct {
		name     string
		snapshot git.Snapshot
		want     string
	}{
		{name: "clean", want: "history"},
		{name: "unstaged", snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "file", Diff: "changed"}}}},
		{name: "staged", snapshot: git.Snapshot{Staged: []git.Change{{Path: "file", Diff: "changed"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{startup: true, loading: true, width: 60, height: 10}
			updated, cmd := m.Update(loaded{snapshot: tt.snapshot})
			got := updated.(model)
			if got.screen != tt.want || got.startup || (tt.want == "history" && cmd == nil) {
				t.Fatalf("unexpected startup state: %#v", got)
			}
		})
	}
}

func TestCleanRefreshDoesNotChangeViewAfterStartup(t *testing.T) {
	m := model{startup: false, width: 60, height: 10}
	updated, cmd := m.Update(loaded{})
	if updated.(model).screen != "" || cmd != nil {
		t.Fatal("refresh unexpectedly opened history")
	}
}

func TestStartupDoesNotInterruptModal(t *testing.T) {
	m := model{startup: true, modal: "help", width: 60, height: 10}
	updated, cmd := m.Update(loaded{})
	if updated.(model).screen != "" || cmd != nil {
		t.Fatal("startup interrupted modal")
	}
}

func TestStagingUsesActiveLine(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "first", "first\n")
	writeFile(t, r, "second", "second\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, width: 60, height: 20, wrap: true, snapshot: s}
	m.rebuild()
	m.jump(1)
	if m.rows[m.pos].path != "second" || m.viewportStart(18) != 0 {
		t.Fatal("test setup: wrong active file")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("missing stage command")
	}
	result, ok := cmd().(completed)
	if !ok || result.err != nil {
		t.Fatalf("stage failed: %#v", result)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 1 || s.Staged[0].Path != "second" || len(s.Unstaged) != 1 || s.Unstaged[0].Path != "first" {
		t.Fatalf("staged wrong file: %#v", s)
	}
}

func TestWrapAndSanitize(t *testing.T) {
	parts := wrapLine("ab界cd", 4)
	if len(parts) != 2 || parts[0] != "ab界" || parts[1] != "cd" {
		t.Fatalf("%q", parts)
	}
	if got := syntax.Clean("a\x1b[31m"); got != "a�[31m" {
		t.Fatalf("unsafe text: %q", got)
	}
}

func TestParagraphDiffJumps(t *testing.T) {
	m := model{pos: 1, rows: []row{
		{kind: 'f', text: "file one"}, {text: "first"}, {text: "second"}, {text: ""}, {text: "  "},
		{text: "third"}, {kind: '@', text: "  ···"}, {text: "fourth"}, {kind: 'f', text: "file two"}, {text: "last"},
	}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("}")})
	m = updated.(model)
	if m.pos != 3 {
		t.Fatalf("jump down: %d", m.pos)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("}")})
	m = updated.(model)
	if m.pos != 6 {
		t.Fatalf("jump past blank lines: %d", m.pos)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{")})
	m = updated.(model)
	if m.pos != 4 {
		t.Fatalf("jump up: %d", m.pos)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{")})
	if updated.(model).pos != 0 {
		t.Fatal("jump to file boundary failed")
	}
}

func TestFileJumpPlacesHeaderAtTop(t *testing.T) {
	long := "@@ -0,0 +1,30 @@\n" + strings.Repeat("+line\n", 30)
	m := model{width: 40, height: 12, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{
		{Path: "one", Diff: long}, {Path: "two", Diff: long}, {Path: "three", Diff: long},
	}}}
	m.rebuild()
	m.jump(1)
	if m.rows[m.pos].path != "two" || m.viewTop != m.pos {
		t.Fatalf("next file header not at top: pos %d top %d", m.pos, m.viewTop)
	}
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[1], "▸") || !strings.Contains(lines[1], "two") {
		t.Fatalf("first diff line should be the active header: %q", lines[1])
	}
	m.jump(1)
	m.jump(-1)
	if m.rows[m.pos].path != "two" || m.viewTop != m.pos {
		t.Fatalf("previous file header not at top: pos %d top %d", m.pos, m.viewTop)
	}
}

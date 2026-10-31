package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNavigationAndRefreshAnchor(t *testing.T) {
	m := model{width: 20, height: 10, wrap: true, snapshot: snapshot{unstaged: []change{{path: "one", diff: "@@ -0,0 +1,2 @@\n+first\n+second\n"}, {path: "two", diff: "@@ -0,0 +1 @@\n+other\n"}}}}
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
	if m.rows[m.pos].path != "one" || m.rows[m.pos].line != 2 {
		t.Fatal("refresh anchor")
	}
}

func TestActiveLineShowsContext(t *testing.T) {
	rows := make([]row, 20)
	for i := range rows {
		rows[i].text = fmt.Sprintf("line %02d", i)
	}
	m := model{width: 30, height: 10, wrap: true, rows: rows, pos: 7}
	view := m.View()
	if !strings.Contains(view, "line 04") || !strings.Contains(view, "▸ \x1b[0mline 07") || !strings.Contains(view, "line 11") {
		t.Fatalf("active line or context missing: %q", view)
	}
	if strings.Contains(view, "line 03") {
		t.Fatalf("viewport starts too early: %q", view)
	}
}

func TestWrappedActiveLineSurvivesRefreshAndResize(t *testing.T) {
	s := snapshot{unstaged: []change{{path: "long", diff: "@@ -0,0 +1 @@\n+abcdefghijklmnopqrstuvwxyz0123456789\n"}}}
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
	if got.line != original.line || got.offset > original.offset || (m.pos+1 < len(m.rows) && m.rows[m.pos+1].line == got.line && m.rows[m.pos+1].offset <= original.offset) {
		t.Fatalf("resize lost position: got %#v, original %#v", got, original)
	}
}

func TestCommitWrappedLineSurvivesResize(t *testing.T) {
	m := model{width: 16, height: 10, wrap: true, screen: "commit", history: historyState{changes: []change{{path: "long", diff: "@@ -0,0 +1 @@\n+abcdefghijklmnopqrstuvwxyz0123456789\n"}}}}
	m.rebuildCommit()
	m.history.pos = 3
	original := m.history.rows[m.history.pos]
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	m = updated.(model)
	got := m.history.rows[m.history.pos]
	if got.line != original.line || got.offset == 0 || got.offset > original.offset {
		t.Fatalf("commit resize lost position: %#v", got)
	}
}

func TestStartupViewSelection(t *testing.T) {
	tests := []struct {
		name     string
		snapshot snapshot
		want     string
	}{
		{name: "clean", want: "history"},
		{name: "unstaged", snapshot: snapshot{unstaged: []change{{path: "file", diff: "changed"}}}},
		{name: "staged", snapshot: snapshot{staged: []change{{path: "file", diff: "changed"}}}},
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
	s, err := r.load()
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
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 1 || s.staged[0].path != "second" || len(s.unstaged) != 1 || s.unstaged[0].path != "first" {
		t.Fatalf("staged wrong file: %#v", s)
	}
}

func TestWrapAndSanitize(t *testing.T) {
	parts := wrapLine("ab界cd", 4)
	if len(parts) != 2 || parts[0] != "ab界" || parts[1] != "cd" {
		t.Fatalf("%q", parts)
	}
	if got := clean("a\x1b[31m"); got != "a�[31m" {
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

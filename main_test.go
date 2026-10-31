package main

import (
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

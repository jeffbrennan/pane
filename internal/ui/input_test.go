package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLineEditing(t *testing.T) {
	text, pos := "hello, 世界!", len([]rune("hello, 世界!"))
	text, pos, _ = editLine(text, pos, "ctrl+w", nil)
	if text != "hello, " || pos != 7 {
		t.Fatalf("word deletion: %q at %d", text, pos)
	}
	text, pos, _ = editLine(text, pos, "home", nil)
	text, pos, _ = editLine(text, pos, "right", nil)
	text, pos, _ = editLine(text, pos, "x", []rune("界"))
	if text != "h界ello, " || pos != 2 {
		t.Fatalf("insertion: %q at %d", text, pos)
	}
	text, pos, _ = editLine(text, pos, "delete", nil)
	text, pos, _ = editLine(text, pos, "ctrl+k", nil)
	if text != "h界" || pos != 2 {
		t.Fatalf("deletion: %q at %d", text, pos)
	}
}

func TestCommitPasteAndCursor(t *testing.T) {
	m := model{width: 60, height: 20, modal: "commit"}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("subject\n\nbody"), Paste: true})
	m = updated.(model)
	if m.input != "subject" || m.inputBody != "\n\nbody" {
		t.Fatalf("paste: %q + %q", m.input, m.inputBody)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("界")})
	m = updated.(model)
	if m.input != "界subject" || !strings.Contains(m.View(), "界\x1b[7ms\x1b[0m") {
		t.Fatalf("cursor after wide rune: %q", m.View())
	}
}

func TestPastedCommitMessage(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "content\n")
	m := model{repo: r, modal: "commit"}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("subject\n\nbody line"), Paste: true})
	updated, cmd := updated.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("commit did not start")
	}
	result := cmd().(completed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	out, err := r.Command("log", "-1", "--format=%B")
	if err != nil || strings.TrimSpace(string(out)) != "subject\n\nbody line" {
		t.Fatalf("commit message %q: %v", out, err)
	}
}

func TestCommitVimModes(t *testing.T) {
	m := model{width: 70, height: 20, modal: "commit"}
	m = keys(t, m, "o", "n", "e", " ", "t", "w", "o")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if !m.inputNormal || m.inputCursor != 6 || !strings.Contains(m.View(), "NORMAL") {
		t.Fatalf("normal mode: %#v", m)
	}
	m = keys(t, m, "b", "d", "w")
	if m.input != "one " || m.inputCursor != 3 {
		t.Fatalf("delete word: %q at %d", m.input, m.inputCursor)
	}
	m = keys(t, m, "0", "a", "X")
	if m.inputNormal || m.input != "oXne " || m.inputCursor != 2 || !strings.Contains(m.View(), "INSERT") {
		t.Fatalf("insert after: %q at %d", m.input, m.inputCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = keys(t, updated.(model), "d", "d")
	if m.input != "" || m.inputCursor != 0 || !m.inputNormal {
		t.Fatalf("delete line: %#v", m)
	}
	m = keys(t, m, "A", "z")
	if m.input != "z" || m.inputNormal {
		t.Fatalf("append to empty subject: %#v", m)
	}
}

package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func toolModel() model {
	m := model{width: 60, height: 12, wrap: true, snapshot: snapshot{unstaged: []change{
		{path: "cmd/server/main.go", diff: "@@ -1,3 +1,3 @@\n context\n-old Value\n+new value\n"},
		{path: "pkg/api/types.go", diff: "@@ -10,2 +10,3 @@\n keep\n+added value\n+more\n"},
		{path: "README.md", diff: "@@ -0,0 +1,20 @@\n" + strings.Repeat("+docs\n", 20)},
	}}}
	m.rebuild()
	return m
}

func keys(t *testing.T, m model, input ...string) model {
	t.Helper()
	for _, k := range input {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	return m
}

func TestFilePickerFiltersAndJumps(t *testing.T) {
	m := toolModel()
	m = keys(t, m, "f")
	if m.modal != "files" || !strings.Contains(m.View(), "Files  3/3") || !strings.Contains(m.View(), "+2 -0") {
		t.Fatalf("picker: %q", m.View())
	}
	m = keys(t, m, "/", "t", "y", "p")
	if got := filterFiles(m.fileEntries(), "typ"); len(got) != 1 || got[0].path != "pkg/api/types.go" {
		t.Fatalf("filter: %#v", got)
	}
	m = keys(t, m, "enter")
	if m.modal != "" || m.rows[m.pos].kind != 'f' || m.rows[m.pos].path != "pkg/api/types.go" || m.viewTop != m.pos {
		t.Fatalf("jump: pos %d top %d %#v", m.pos, m.viewTop, m.rows[m.pos])
	}
	if got := filterFiles(m.fileEntries(), "csm"); len(got) != 1 || got[0].path != "cmd/server/main.go" {
		t.Fatalf("subsequence: %#v", got)
	}
	if got := filterFiles(m.fileEntries(), "md"); len(got) != 2 || got[0].path != "README.md" {
		t.Fatalf("basename matches should rank first: %#v", got)
	}
	m = keys(t, m, "f", "/", "z", "z", "z", "enter")
	if m.modal != "" || m.rows[m.pos].path != "pkg/api/types.go" {
		t.Fatal("no-match enter should close without moving")
	}
}

func TestFilePickerNavigationAndSearchModes(t *testing.T) {
	m := keys(t, toolModel(), "f")
	if !strings.Contains(m.View(), "j/k select") || !strings.Contains(m.View(), "/ search") {
		t.Fatalf("missing navigation hints: %q", m.View())
	}
	m = keys(t, m, "j", "j", "k")
	if m.picker.selected != 1 || m.picker.query != "" {
		t.Fatalf("j/k did not navigate: %#v", m.picker)
	}
	m = keys(t, m, "/", "k", "j", "/")
	if !m.picker.searching || m.picker.query != "kj/" {
		t.Fatalf("search mode did not accept literal keys: %#v", m.picker)
	}
	m = keys(t, m, "esc")
	if m.modal != "files" || m.picker.searching || m.picker.query != "kj/" {
		t.Fatal("escape should return to navigation while keeping the filter")
	}
	m = keys(t, m, "/", "backspace", "backspace", "backspace", "t", "y", "p", "esc", "enter")
	if m.modal != "" || m.rows[m.pos].path != "pkg/api/types.go" {
		t.Fatal("enter should jump to the selected filtered file")
	}
	m = keys(t, m, "f", "esc")
	if m.modal != "" {
		t.Fatal("escape in navigation mode should close the picker")
	}
}

func TestDiffSearch(t *testing.T) {
	m := toolModel()
	m = keys(t, m, "/", "v", "a", "l")
	if m.modal != "search" || !strings.Contains(m.rows[m.pos].text, "Value") || !strings.Contains(m.View(), "/val_") {
		t.Fatalf("incremental search: %#v", m.rows[m.pos])
	}
	if !strings.Contains(m.View(), "\x1b[7mVal\x1b[27m") {
		t.Fatalf("missing highlight: %q", m.View())
	}
	m = keys(t, m, "enter")
	first := m.pos
	if m.search.query != "val" || !strings.Contains(m.flash, "[1/3]") {
		t.Fatalf("enter: %q %q", m.search.query, m.flash)
	}
	m = keys(t, m, "n")
	if m.pos <= first || !strings.Contains(m.flash, "[2/3]") {
		t.Fatalf("next: %d %q", m.pos, m.flash)
	}
	m = keys(t, m, "n", "n")
	if m.pos != first || !strings.Contains(m.flash, "wrapped") {
		t.Fatalf("wrap: %d %q", m.pos, m.flash)
	}
	m = keys(t, m, "N")
	if !strings.Contains(m.flash, "[3/3]") {
		t.Fatalf("previous: %q", m.flash)
	}
	m = keys(t, m, "j")
	if m.flash != "" {
		t.Fatal("flash should clear on the next key")
	}
	origin := m.pos
	m = keys(t, m, "/", "V", "a", "l", "esc")
	if m.pos != origin || m.modal != "" || m.search.query != "val" {
		t.Fatal("esc should restore position and keep the previous search")
	}
	m = keys(t, m, "/", "V", "a", "l", "u", "e", "enter")
	if !strings.Contains(m.flash, "[1/1]") {
		t.Fatalf("smartcase: %q", m.flash)
	}
	m = keys(t, m, "/", "z", "z", "z", "enter")
	if !strings.HasPrefix(m.flash, "Pattern not found") {
		t.Fatalf("not found: %q", m.flash)
	}
}

func TestCommitViewSharesDiffTools(t *testing.T) {
	m := toolModel()
	m.screen = "commit"
	m.history.changes = m.snapshot.unstaged
	m.history.current = commitInfo{hash: "abcdef1234567890", subject: "subject"}
	m.rebuildCommit()
	m = keys(t, m, "f", "/", "R", "E", "A", "enter")
	if m.screen != "commit" || m.history.rows[m.history.pos].path != "README.md" {
		t.Fatalf("commit picker: %#v", m.history.rows[m.history.pos])
	}
	m = keys(t, m, "/", "a", "d", "d", "e", "d", "enter")
	if !strings.Contains(m.history.rows[m.history.pos].text, "added") || !strings.Contains(m.View(), "[1/1]") {
		t.Fatalf("commit search: %q", m.View())
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("copy hash unavailable in commit view")
	}
}

func TestRowTargetsAndEditorArgs(t *testing.T) {
	rows := buildRows([]change{{path: "f", diff: "@@ -5,3 +5,3 @@\n same\n-gone\n+here\n tail\n"}}, 80, true)
	want := map[string]int{"same": 5, "gone": 6, "here": 6, "tail": 7}
	for _, r := range rows {
		if line, ok := want[r.text]; ok && r.target != line {
			t.Errorf("%q target %d, want %d", r.text, r.target, line)
		}
	}
	cases := map[string][]string{
		"":                  {"vi", "+6", "/r/f"},
		"nvim":              {"nvim", "+6", "/r/f"},
		"code --wait":       {"code", "--wait", "-g", "/r/f:6"},
		"/usr/local/bin/hx": {"/usr/local/bin/hx", "/r/f:6"},
	}
	for editor, expected := range cases {
		if got := editorArgs(editor, "/r/f", 6); strings.Join(got, " ") != strings.Join(expected, " ") {
			t.Errorf("%q: %v", editor, got)
		}
	}
	if got := editorArgs("vim", "/r/f", 0); strings.Join(got, " ") != "vim /r/f" {
		t.Errorf("no line: %v", got)
	}
}

func TestOpenEditorRequiresWorkingTreeFile(t *testing.T) {
	m := toolModel()
	m.repo = repository{root: t.TempDir()}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd != nil || !strings.Contains(updated.(model).flash, "does not exist") {
		t.Fatalf("missing file: %q", updated.(model).flash)
	}
	r := testRepo(t)
	writeFile(t, r, "present", "line\n")
	m.repo = r
	m.rows = []row{{path: "present", kind: '+', target: 1}}
	m.pos = 0
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")}); cmd == nil {
		t.Fatal("editor not launched")
	}
}

func TestHistoryCopyAndFlash(t *testing.T) {
	m := model{width: 60, height: 12, screen: "history", history: historyState{commits: []commitInfo{{hash: "abcdef1234567890", subject: "one"}}}}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("copy hash unavailable in history")
	}
	updated, _ := m.Update(flashed{"Copied abcdef12"})
	if !strings.Contains(updated.(model).View(), "Copied abcdef12") {
		t.Fatal("flash not shown in history")
	}
}

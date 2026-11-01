package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func toolModel() model {
	m := model{width: 60, height: 12, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{
		{Path: "cmd/server/main.go", Diff: "@@ -1,3 +1,3 @@\n context\n-old Value\n+new value\n"},
		{Path: "pkg/api/types.go", Diff: "@@ -10,2 +10,3 @@\n keep\n+added value\n+more\n"},
		{Path: "README.md", Diff: "@@ -0,0 +1,20 @@\n" + strings.Repeat("+docs\n", 20)},
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
	if m.modal != "files" || !strings.Contains(plain(m.View()), "3/3 │") || !strings.Contains(plain(m.View()), "+ 1 -1") {
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

func TestFilePickerRefreshesAfterRebuild(t *testing.T) {
	m := keys(t, toolModel(), "f")
	if len(m.fileEntries()) != 3 {
		t.Fatalf("entries: %#v", m.fileEntries())
	}
	m.snapshot.Unstaged = m.snapshot.Unstaged[:1]
	m.rebuild()
	if entries := m.fileEntries(); len(entries) != 1 || !strings.Contains(plain(m.View()), "1/1 │") {
		t.Fatalf("stale picker after rebuild: %#v", entries)
	}
	m = keys(t, m, "esc")
	if m.picker.cache != nil {
		t.Fatal("closed picker kept its rows")
	}
}

func TestWorkingDiffPickerBackNavigation(t *testing.T) {
	for _, staged := range []bool{false, true} {
		for _, back := range []string{"esc", "q"} {
			for _, close := range []string{"esc", "q"} {
				name := fmt.Sprintf("staged=%t/%s/%s", staged, back, close)
				t.Run(name, func(t *testing.T) {
					m := toolModel()
					if staged {
						m.snapshot.Staged = m.snapshot.Unstaged
						m.staged = true
						m.rebuild()
					}
					m = keys(t, m, "f", "/", "t", "y", "p", "enter", "j", back)
					if m.screen != "" || m.modal != "files" || m.picker.query != "typ" || m.picker.searching {
						t.Fatalf("picker not restored: screen=%q modal=%q picker=%#v", m.screen, m.modal, m.picker)
					}
					items := m.pickerItems()
					if len(items) == 0 || items[m.picker.selected].path != "pkg/api/types.go" {
						t.Fatalf("picker selection not restored: %#v", items)
					}
					m = keys(t, m, close)
					if m.screen != "history" || m.modal != "" {
						t.Fatalf("did not return to history: screen=%q modal=%q", m.screen, m.modal)
					}
				})
			}
		}
	}
}

func TestWorkingDiffNormalQuitAndTabReset(t *testing.T) {
	m := toolModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("normal q did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("normal q did not produce quit message")
	}
	m = keys(t, m, "f", "/", "t", "y", "p", "enter")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.pickerReturn {
		t.Fatal("switching diff views kept stale picker navigation")
	}
}

func TestFilePickerNavigationAndSearchModes(t *testing.T) {
	m := keys(t, toolModel(), "f")
	if !strings.Contains(plain(m.View()), "j/k move") || !strings.Contains(plain(m.View()), "/ filter") {
		t.Fatalf("missing navigation hints: %q", m.View())
	}
	initial := m.picker.selected
	m = keys(t, m, "j", "j", "k")
	if m.picker.selected != initial+1 || m.picker.query != "" {
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

func TestFilePickerVimMotions(t *testing.T) {
	m := keys(t, toolModel(), "f")
	for _, tc := range []struct {
		input []string
		want  int
	}{
		{[]string{"G"}, 6},
		{[]string{"g", "g"}, 0},
		{[]string{"3", "j"}, 3},
		{[]string{"2", "k"}, 1},
		{[]string{"4", "G"}, 3},
		{[]string{"2", "g", "g"}, 1},
		{[]string{"}"}, 3},
		{[]string{"{"}, 1},
		{[]string{"2", "}"}, 4},
		{[]string{"2", "{"}, 1},
	} {
		m = keys(t, m, tc.input...)
		if m.picker.selected != tc.want {
			t.Fatalf("%v selected %d, want %d", tc.input, m.picker.selected, tc.want)
		}
	}
	for _, tc := range []struct {
		key  string
		want int
	}{{"ctrl+d", 3}, {"ctrl+f", 6}, {"ctrl+u", 4}, {"ctrl+b", 0}, {"end", 6}, {"home", 0}} {
		m.handlePickerKey(tc.key, nil)
		if m.picker.selected != tc.want {
			t.Fatalf("%s selected %d, want %d", tc.key, m.picker.selected, tc.want)
		}
	}
	m = keys(t, m, "g", "esc")
	if m.modal != "files" || m.picker.pending != "" {
		t.Fatal("escape should cancel a pending motion")
	}
	m = keys(t, m, "/", "g", "G", "3")
	if m.picker.query != "gG3" {
		t.Fatalf("motion keys were not entered as search text: %q", m.picker.query)
	}
	m.handlePickerKey("ctrl+d", nil)
	if m.picker.selected != 0 {
		t.Fatal("page motion should be safe with no matches")
	}
	m = keys(t, toolModel(), "f", "g", "g", "h", "}")
	if item := m.pickerItems()[m.picker.selected]; item.path != "pkg" {
		t.Fatalf("directory jump should skip folded descendants: %#v", item)
	}
}

func TestFilePickerDirectoryTree(t *testing.T) {
	m := keys(t, toolModel(), "f")
	items := m.pickerItems()
	if len(items) != 7 || items[0].path != "cmd" || !items[0].directory || items[1].path != "cmd/server" || items[2].path != "cmd/server/main.go" {
		t.Fatalf("unexpected tree: %#v", items)
	}
	m.picker.selected = 0
	m = keys(t, m, "h")
	items = m.pickerItems()
	if len(items) != 5 || !items[0].collapsed || items[1].path != "pkg" {
		t.Fatalf("directory not collapsed: %#v", items)
	}
	m = keys(t, m, "enter")
	if len(m.pickerItems()) != 7 || m.modal != "files" {
		t.Fatal("enter did not expand the directory")
	}
	m.picker.selected = 2
	m = keys(t, m, "h")
	if item := m.pickerItems()[m.picker.selected]; item.path != "cmd/server" || !item.collapsed {
		t.Fatal("h on a file did not collapse its parent")
	}
	m.picker.selected = 0
	m = keys(t, m, "h", "/", "m", "a", "i", "n")
	if !strings.Contains(m.View(), "main.go") || m.pickerItems()[m.picker.selected].path != "cmd/server/main.go" {
		t.Fatal("search did not reveal a file in a collapsed directory")
	}
	m = keys(t, m, "enter")
	if m.modal != "" || m.rows[m.pos].path != "cmd/server/main.go" {
		t.Fatal("search result did not jump to the file")
	}
}

func TestFilePickerReverseScrollKeepsViewportStill(t *testing.T) {
	m := keys(t, toolModel(), "f", "j", "j", "j", "j")
	offset := m.picker.offset
	selected := m.picker.selected
	if offset == 0 {
		t.Fatal("picker did not scroll down")
	}
	m = keys(t, m, "k")
	if m.picker.offset != offset || m.picker.selected != selected-1 {
		t.Fatal("picker scrolled instead of moving the selection")
	}
	for m.picker.selected > offset {
		m = keys(t, m, "k")
	}
	if m.picker.offset != offset {
		t.Fatal("picker scrolled before the selection reached the top")
	}
	m = keys(t, m, "k")
	if m.picker.offset != offset-1 {
		t.Fatal("picker did not scroll after the selection reached the top")
	}
}

func TestFilePickerUsesWiderDialog(t *testing.T) {
	m := toolModel()
	m.width, m.height = 130, 20
	m = keys(t, m, "f")
	if !strings.Contains(m.View(), "┌"+strings.Repeat("─", 118)+"┐") {
		t.Fatal("picker did not use its wider available space")
	}
}

func TestFilePickerHighlightsEntireSelectedRow(t *testing.T) {
	m := toolModel()
	m.height = 20
	m = keys(t, m, "f")
	for _, selected := range []int{1, 2} {
		m.picker.selected = selected
		item := m.pickerItems()[selected]
		view := m.View()
		if strings.Count(view, "\x1b[7m") != 1 {
			t.Fatalf("expected one highlighted row: %q", view)
		}
		for _, line := range strings.Split(view, "\n") {
			if !strings.Contains(line, "\x1b[7m") {
				continue
			}
			start := strings.Index(line, "\x1b[7m")
			end := start + strings.Index(line[start:], "\x1b[0m")
			if end < start || !strings.Contains(line[start:end], item.name) || !strings.HasSuffix(plain(line), "│") || strings.TrimRight(plain(line[end:]), " │") != "" {
				t.Fatalf("selection does not span row for %q: %q", item.path, line)
			}
		}
	}
}

func TestFilePickerRelativeNumbersAndChangeColumn(t *testing.T) {
	m := toolModel()
	m.height = 20
	m = keys(t, m, "f")
	check := func() {
		t.Helper()
		view := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(m.View(), "")
		lines := strings.Split(view, "\n")
		items := m.pickerItems()
		start, visible := m.pickerViewport(len(items))
		for i := start; i < min(len(items), start+visible); i++ {
			item := items[i]
			found := false
			for _, line := range lines {
				if !strings.Contains(line, item.name) {
					continue
				}
				found = true
				fields := strings.Fields(line)
				want := fmt.Sprint(max(i-m.picker.selected, m.picker.selected-i))
				if len(fields) < 2 || fields[1] != want {
					t.Fatalf("wrong distance for %s: %q, want %s", item.path, line, want)
				}
				if !item.directory && strings.Index(line, item.name) > strings.LastIndex(line, " │ +") {
					t.Fatalf("change counts should follow file name: %q", line)
				}
				break
			}
			if !found {
				t.Fatalf("missing row for %s in %q", item.path, view)
			}
		}
	}
	check()
	m = keys(t, m, "G")
	m.height = 11
	m.revealPickerSelection()
	check()
	m = keys(t, m, "g", "g", "h")
	check()
	m = keys(t, m, "/", "t", "y", "p")
	check()
}

func TestFilePickerAlignsChangeCounts(t *testing.T) {
	m := model{width: 80, height: 15, snapshot: git.Snapshot{Unstaged: []git.Change{
		{Path: "small.go", Diff: "@@ -1,0 +1,2 @@\n+one\n+two\n"},
		{Path: "large.go", Diff: "@@ -1,0 +1,1001 @@\n" + strings.Repeat("+line\n", 1001)},
		{Path: "deleted.go", Diff: "@@ -1,1 +1,0 @@\n-line\n"},
	}}}
	m.rebuild()
	m = keys(t, m, "f")
	view := plain(m.View())
	for _, counts := range []string{"+   2 -0", "+1001 -0", "+   0 -1"} {
		if !strings.Contains(view, counts) {
			t.Errorf("missing aligned counts %q in %q", counts, view)
		}
	}
}

func TestFilePickerTreeBranchesAndStableExpansion(t *testing.T) {
	m := toolModel()
	m.height = 20
	m = keys(t, m, "f")
	m.picker.selected = 0
	m = keys(t, m, "h")
	before := strings.Split(m.View(), "\n")
	selectedLine := -1
	for i, line := range before {
		if strings.Contains(line, "cmd/") {
			selectedLine = i
		}
	}
	m = keys(t, m, "l")
	after := strings.Split(m.View(), "\n")
	if selectedLine < 0 || !strings.Contains(after[selectedLine], "cmd/") {
		t.Fatal("expanding a directory moved it on screen")
	}
	view := strings.Join(after, "\n")
	for _, branch := range []string{"Files", "│ Changes", "├─ ▾ cmd/", "│  └─ ▾ server/", "│     └─   main.go", "└─   README.md"} {
		if !strings.Contains(view, branch) {
			t.Errorf("missing tree branch %q in %q", branch, view)
		}
	}
}

func TestCollapsedDirectoryShowsNestedChangeTotals(t *testing.T) {
	entries := []fileEntry{
		{path: "src/a.go", added: 12, deleted: 2},
		{path: "src/nested/b.go", added: 7, deleted: 4},
		{path: "other/c.go", added: 100, deleted: 10},
	}
	items := pickerTree(entries, map[string]bool{"src": true})
	if len(items) != 3 || items[0].path != "src" || items[0].added != 19 || items[0].deleted != 6 {
		t.Fatalf("incorrect collapsed totals: %#v", items)
	}
	filtered := pickerTree(filterFiles(entries, "b.go"), map[string]bool{"src": true})
	if len(filtered) != 1 || filtered[0].added != 7 || filtered[0].deleted != 4 {
		t.Fatalf("totals should reflect matching files: %#v", filtered)
	}

	m := toolModel()
	m.height = 20
	m = keys(t, m, "f")
	m.picker.selected = 0
	m = keys(t, m, "h")
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "cmd/") {
			if !strings.Contains(line, "+ 1 -1") {
				t.Fatalf("collapsed directory missing totals: %q", line)
			}
			return
		}
	}
	t.Fatal("collapsed directory missing from picker")
}

func TestDiffSearch(t *testing.T) {
	m := toolModel()
	m = keys(t, m, "/", "v", "a", "l")
	if m.modal != "search" || !strings.Contains(m.rows[m.pos].text, "Value") || !strings.Contains(m.View(), "/val"+cursor) {
		t.Fatalf("incremental search: %#v", m.rows[m.pos])
	}
	view := regexp.MustCompile("\\x1b\\[(39|48;2;[0-9;]+)m").ReplaceAllString(m.View(), "")
	if !strings.Contains(view, "\x1b[7mVal\x1b[27m") {
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
	m.history.changes = m.snapshot.Unstaged
	m.history.current = git.CommitInfo{Hash: "abcdef1234567890", Subject: "subject"}
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
	rows := buildRows([]git.Change{{Path: "f", Diff: "@@ -5,3 +5,3 @@\n same\n-gone\n+here\n tail\n"}}, 80, true)
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
	m.repo = git.Repo{Root: t.TempDir()}
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
	m := model{width: 60, height: 12, screen: "history", history: historyState{commits: []git.CommitInfo{{Hash: "abcdef1234567890", Subject: "one"}}}}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("copy hash unavailable in history")
	}
	updated, _ := m.Update(flashed{"Copied abcdef12"})
	if !strings.Contains(updated.(model).View(), "Copied abcdef12") {
		t.Fatal("flash not shown in history")
	}
}

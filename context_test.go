package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExpandFileContext(t *testing.T) {
	r := testRepo(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	writeFile(t, r, "file", strings.Join(lines, "\n")+"\n")
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	lines[14] = "changed"
	writeFile(t, r, "file", strings.Join(lines, "\n")+"\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, width: 60, height: 12, wrap: true, snapshot: s}
	m.rebuild()
	if strings.Contains(m.View(), "line 01") {
		t.Fatal("unexpected full context before expansion")
	}
	for i, r := range m.rows {
		if r.kind == '+' {
			m.pos = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if cmd == nil {
		t.Fatal("expansion did not load context")
	}
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	if !hasContextText(m.rows, "line 05") || hasContextText(m.rows, "line 01") || m.rows[m.pos].kind != '+' {
		t.Fatalf("local context or selection incorrect: %#v", m.rows)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(model)
	if !hasContextText(m.rows, "line 01") {
		t.Fatal("repeating l did not reveal more context around the changed line")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	if hasContextText(m.rows, "line 01") || !hasContextText(m.rows, "line 05") {
		t.Fatal("h did not undo the most recent context expansion")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	if hasContextText(m.rows, "line 05") || m.rows[m.pos].kind != '+' {
		t.Fatalf("context did not collapse: %q", m.View())
	}
}

func TestContextResponseAfterCollapseIsIgnored(t *testing.T) {
	m := model{width: 40, height: 10, snapshot: Snapshot{Unstaged: []Change{longFile("file")}}}
	m.rebuild()
	m.pos = 1
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("missing context request")
	}
	file := m.contextFiles[contextKey{path: "file"}]
	m.pos = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	m = m.applyContext(contextLoaded{key: contextKey{path: "file"}, base: file.base, diff: "expanded", request: file.request})
	if len(m.contextFiles) != 0 {
		t.Fatal("late response restored collapsed context")
	}
}

func TestContextKeysOnChangedAndHunkSeparatorRows(t *testing.T) {
	base := "@@ -2 +2 @@\n-old\n+new\n@@ -12 +12 @@\n-before\n+after\n"
	for _, kind := range []byte{'+', '@'} {
		t.Run(string(kind), func(t *testing.T) {
			m := model{width: 40, height: 10, wrap: true, snapshot: Snapshot{Unstaged: []Change{{Path: "first", Diff: base}, longFile("second")}}}
			m.rebuild()
			for i, r := range m.rows {
				if r.kind == kind {
					m.pos = i
					break
				}
			}
			if m.rows[m.pos].kind != kind {
				t.Fatalf("missing row kind %q", kind)
			}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
			m = updated.(model)
			if cmd == nil {
				t.Fatal("expansion did not start from diff row")
			}
			file := m.contextFiles[contextKey{path: "first"}]
			full := "@@ -1,13 +1,13 @@\n context\n-old\n+new\n" + strings.Repeat(" middle\n", 9) + "-before\n+after\n"
			m = m.applyContext(contextLoaded{key: contextKey{path: "first"}, base: base, diff: full, request: file.request})
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
			m = updated.(model)
			if len(m.contextFiles[contextKey{path: "first"}].ranges) != 0 {
				t.Fatal("local context did not collapse")
			}
			m.pos = 0
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
			m = updated.(model)
			if m.rows[m.pos].kind != 'f' || m.rows[m.pos].path != "first" {
				t.Fatal("collapse did not select the file header")
			}
			for _, r := range m.rows {
				if r.path == "first" && r.kind != 'f' {
					t.Fatalf("collapsed file still has content: %#v", r)
				}
			}
			if m.rows[len(m.rows)-1].path != "second" {
				t.Fatal("collapsing hid the next file")
			}
			updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
			m = updated.(model)
			if cmd != nil || len(m.rows) < 3 || m.rows[1].path != "first" || m.rows[1].kind == 's' {
				t.Fatal("expanding a collapsed file did not restore its diff immediately")
			}
			if strings.Contains(m.View(), "context") {
				t.Fatal("reopening a collapsed file should show the standard diff")
			}
		})
	}
}

func TestExpandDeletedFileWithContextKey(t *testing.T) {
	diff := "diff --git a/file b/file\ndeleted file mode 100644\n@@ -1,2 +0,0 @@\n-first\n-second\n"
	m := model{width: 40, height: 10, snapshot: Snapshot{Unstaged: []Change{{Path: "file", Diff: diff}}}}
	m.rebuild()
	m.toggleDeletedFile()
	if !strings.Contains(m.View(), "first") {
		t.Fatal("deleted file did not expand")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	if len(m.rows) != 1 || m.rows[0].kind != 'f' {
		t.Fatal("deleted file did not collapse to its header")
	}
}

func TestContextLoadKeepsSelectionAndRejectsStaleDiff(t *testing.T) {
	base := "@@ -10,2 +10,2 @@\n same\n-old\n+new\n"
	full := "@@ -1,11 +1,11 @@\n" + strings.Repeat(" earlier\n", 9) + " same\n-old\n+new\n"
	m := model{width: 40, height: 10, wrap: true, snapshot: Snapshot{Unstaged: []Change{{Path: "file", Diff: base}}}}
	m.rebuild()
	m.pos = 1
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(model)
	file := m.contextFiles[contextKey{path: "file"}]
	m.pos = len(m.rows) - 1
	anchor := m.rows[m.pos]
	m = m.applyContext(contextLoaded{key: contextKey{path: "file"}, base: base, diff: full, request: file.request})
	if got := m.rows[m.pos]; got.text != anchor.text || got.target != anchor.target || got.kind != anchor.kind {
		t.Fatalf("selection changed after expansion: %#v", got)
	}
	m.snapshot.Unstaged[0].Diff = "@@ -1 +1 @@\n-other\n+updated\n"
	m.rebuild()
	if strings.Contains(m.View(), "earlier") {
		t.Fatal("expanded context was reused for a changed diff")
	}
}

func TestExpandContextInCommitView(t *testing.T) {
	base := "@@ -10 +10 @@\n-old\n+new\n"
	m := model{width: 40, height: 10, wrap: true, screen: "commit", history: historyState{current: CommitInfo{Hash: "abc"}, changes: []Change{{Path: "file", Diff: base}}}}
	m.rebuildCommit()
	m.history.pos = 1
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("commit context did not start loading")
	}
	key := contextKey{path: "file", commit: "abc"}
	file := m.contextFiles[key]
	full := "@@ -1,10 +1,10 @@\n" + strings.Repeat(" earlier\n", 9) + "-old\n+new\n"
	m = m.applyContext(contextLoaded{key: key, base: base, diff: full, request: file.request})
	if !strings.Contains(m.View(), "earlier") {
		t.Fatal("commit context was not rendered")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if strings.Contains(updated.(model).View(), "earlier") {
		t.Fatal("commit context did not collapse")
	}
}

func hasContextText(rows []row, text string) bool {
	for _, r := range rows {
		if r.text == text {
			return true
		}
	}
	return false
}

func TestContextUsesCorrectGitVersion(t *testing.T) {
	r := testRepo(t)
	base := strings.Repeat("before\n", 15) + "old\n" + strings.Repeat("after\n", 15)
	writeFile(t, r, "file", base)
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", strings.Replace(base, "old\n", "staged\n", 1))
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "second"); err != nil {
		t.Fatal(err)
	}
	hashBytes, err := r.Command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(hashBytes))
	writeFile(t, r, "file", strings.Replace(base, "old\n", "index\n", 1))
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", strings.Replace(base, "old\n", "working\n", 1))
	for _, tt := range []struct {
		key  contextKey
		want string
	}{{contextKey{path: "file"}, "+working"}, {contextKey{path: "file", staged: true}, "+index"}, {contextKey{path: "file", commit: hash}, "+staged"}} {
		diff, err := r.FullContext(tt.key.gitContext(), "", "")
		if err != nil || !strings.Contains(diff, tt.want) || !strings.Contains(diff, " before\n") {
			t.Fatalf("context for %#v: %v %q", tt.key, err, diff)
		}
	}
}

func TestIncrementalContextBetweenHunks(t *testing.T) {
	r := testRepo(t)
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %03d", i))
	}
	writeFile(t, r, "file", strings.Join(lines, "\n")+"\n")
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	lines[19], lines[79] = "first change", "second change"
	writeFile(t, r, "file", strings.Join(lines, "\n")+"\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, width: 60, height: 15, wrap: true, snapshot: s}
	m.rebuild()
	for i, r := range m.rows {
		if r.kind == '@' && strings.TrimSpace(r.text) == "···" {
			m.pos = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if cmd == nil {
		t.Fatal("context load did not start")
	}
	updated, _ = updated.(model).Update(cmd())
	m = updated.(model)
	if !hasContextText(m.rows, "line 024") || !hasContextText(m.rows, "line 033") || hasContextText(m.rows, "line 034") {
		t.Fatal("first expansion did not reveal exactly the next ten hidden lines")
	}
	if m.rows[m.pos].kind != '@' || m.rows[m.pos].target != 34 {
		t.Fatalf("selection did not stay on the remaining gap: %#v", m.rows[m.pos])
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(model)
	if !hasContextText(m.rows, "line 043") || hasContextText(m.rows, "line 044") {
		t.Fatal("second expansion did not reveal the next ten lines")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	if hasContextText(m.rows, "line 043") || !hasContextText(m.rows, "line 033") {
		t.Fatal("h did not undo the latest local expansion")
	}
}

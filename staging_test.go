package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func stagingModel(t *testing.T, r repository, staged bool) model {
	t.Helper()
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: s, staged: staged, width: 80, height: 20, wrap: true}
	m.rebuild()
	return m
}

func stageAt(t *testing.T, m model, text string) model {
	t.Helper()
	found := false
	for i, row := range m.rows {
		if row.text == text {
			m.pos, found = i, true
			break
		}
	}
	if !found {
		t.Fatalf("row %q not found: %#v", text, m.rows)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = updated.(model)
	if cmd == nil || !m.busy {
		t.Fatalf("staging did not start: %#v", m)
	}
	result := cmd().(completed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	updated, cmd = m.Update(result)
	if cmd == nil {
		t.Fatal("missing refresh")
	}
	updated, _ = updated.Update(cmd())
	return updated.(model)
}

func TestStageAndUnstageMiddleHunk(t *testing.T) {
	r := testRepo(t)
	base := "first\n" + strings.Repeat("context\n", 12) + "middle\n" + strings.Repeat("context\n", 12) + "last\n"
	writeFile(t, r, "file.txt", base)
	if err := r.stage("file.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	changed := strings.NewReplacer("first", "changed first", "middle", "changed middle", "last", "changed last").Replace(base)
	writeFile(t, r, "file.txt", changed)
	m := stageAt(t, stagingModel(t, r, false), "changed middle")
	staged, err := r.diff("file.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(staged, "+changed middle") || strings.Contains(staged, "+changed first") || strings.Contains(staged, "+changed last") {
		t.Fatalf("wrong staged hunks: %s", staged)
	}
	if m.rows[m.pos].path != "file.txt" || m.rows[m.pos].text != "last" {
		t.Fatalf("cursor not on next hunk: %#v", m.rows[m.pos])
	}
	if err := r.stage("file.txt", false); err != nil {
		t.Fatal(err)
	}
	m = stageAt(t, stagingModel(t, r, true), "changed middle")
	staged, err = r.diff("file.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(staged, "+changed middle") || !strings.Contains(staged, "+changed first") || !strings.Contains(staged, "+changed last") {
		t.Fatalf("wrong unstaged hunk: %s", staged)
	}
	if m.rows[m.pos].path != "file.txt" || m.rows[m.pos].text != "last" {
		t.Fatalf("cursor not on next staged hunk: %#v", m.rows[m.pos])
	}
}

func TestStageHeaderAndUntracked(t *testing.T) {
	for _, onHeader := range []bool{true, false} {
		t.Run(map[bool]string{true: "header", false: "line"}[onHeader], func(t *testing.T) {
			r := testRepo(t)
			writeFile(t, r, "new.txt", "new content\n")
			text := "new content"
			if onHeader {
				text = " new.txt"
			}
			stageAt(t, stagingModel(t, r, false), text)
			out, err := r.command("show", ":new.txt")
			if err != nil || string(out) != "new content\n" {
				t.Fatalf("index: %q, %v", out, err)
			}
		})
	}
}

func TestStageHunkWithoutFinalNewline(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "old")
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "new")
	stageAt(t, stagingModel(t, r, false), "new")
	out, err := r.command("show", ":file")
	if err != nil || string(out) != "new" {
		t.Fatalf("index: %q, %v", out, err)
	}
	stageAt(t, stagingModel(t, r, true), "new")
	out, err = r.command("show", ":file")
	if err != nil || string(out) != "old" {
		t.Fatalf("index: %q, %v", out, err)
	}
}

func TestStageRejectsStaleAndOutsideHunk(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "old\n")
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "new\n")
	m := stagingModel(t, r, false)
	c := m.snapshot.unstaged[0]
	patch := selectedHunk(c.diff, row{kind: '+', sourceLine: 1})
	writeFile(t, r, "file", "newer\n")
	if err := r.stageHunk(c, patch, false); err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Fatalf("stale error: %v", err)
	}
	m.rows = []row{{path: "file", kind: ' ', sourceLine: 100, target: 100}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if cmd != nil || updated.(model).flash != "No change hunk at this line" {
		t.Fatalf("outside hunk: %#v", updated)
	}
}

func TestStageDeletedFileFromLine(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "old\n")
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, "file")); err != nil {
		t.Fatal(err)
	}
	m := stagingModel(t, r, false)
	m.expandedDeleted = map[string]bool{"file": true}
	m.rebuild()
	stageAt(t, m, "old")
	out, err := r.command("diff", "--cached", "--name-status")
	if err != nil || !strings.HasPrefix(string(out), "D\tfile") {
		t.Fatalf("deletion: %q, %v", out, err)
	}
}

func TestStageContentHunkLeavesModeUnstaged(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "old\n")
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "new\n")
	if err := os.Chmod(filepath.Join(r.root, "file"), 0700); err != nil {
		t.Fatal(err)
	}
	stageAt(t, stagingModel(t, r, false), "new")
	staged, err := r.diff("file", true)
	if err != nil || !strings.Contains(staged, "+new") || strings.Contains(staged, "new mode") {
		t.Fatalf("staged diff: %q, %v", staged, err)
	}
	unstaged, err := r.diff("file", false)
	if err != nil || !strings.Contains(unstaged, "new mode") {
		t.Fatalf("unstaged mode: %q, %v", unstaged, err)
	}
}

func TestStageConflictAndBinaryGuards(t *testing.T) {
	conflict := model{snapshot: snapshot{unstaged: []change{{path: "file", conflict: true}}}, rows: []row{{path: "file", kind: 'f'}}}
	updated, cmd := conflict.stageSelection(true)
	if cmd != nil || updated.(model).flash != "Resolve conflicts outside pane" {
		t.Fatalf("conflict: %#v", updated)
	}
	binary := model{snapshot: snapshot{unstaged: []change{{path: "file", diff: "Binary files differ"}}}, rows: []row{{path: "file", kind: '@'}}}
	updated, cmd = binary.stageSelection(true)
	if cmd != nil || updated.(model).flash == "" {
		t.Fatalf("binary: %#v", updated)
	}
}

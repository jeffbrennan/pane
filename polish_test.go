package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRemotePermalink(t *testing.T) {
	for _, tt := range []struct{ remote, want string }{
		{"git@github.com:owner/repo.git", "https://github.com/owner/repo/blob/abc/dir/a%20%23b.go#L42"},
		{"https://gitlab.com/group/sub/repo.git", "https://gitlab.com/group/sub/repo/blob/abc/dir/a%20%23b.go#L42"},
		{"https://example.com/owner/repo.git", ""},
		{"https://github.com/owner", ""},
		{"https://user:password@github.com/owner/repo", ""},
		{"file:///repo", ""},
	} {
		if got := remotePermalink(tt.remote, "abc", "dir/a #b.go", 42); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.remote, got, tt.want)
		}
	}
}

func TestCopyLocationKey(t *testing.T) {
	r := testRepo(t)
	if out, err := r.command("remote", "add", "origin", "git@github.com:owner/repo.git"); err != nil {
		t.Fatalf("remote: %s: %v", out, err)
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "clipboard")
	if err := os.WriteFile(filepath.Join(dir, "pbcopy"), []byte("#!/bin/sh\ncat > \"$PANE_TEST_CLIPBOARD\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PANE_TEST_CLIPBOARD", output)
	for _, tt := range []struct {
		name, screen, want string
		history            historyState
		rows               []row
	}{
		{name: "working deletion", rows: []row{{path: "file", kind: '-', sourceLine: 10, target: 7}}, want: "file:7"},
		{name: "deletion at end", rows: []row{{path: "file", kind: '-', sourceLine: 10, target: 8}, {path: "file", kind: ' ', sourceLine: 7, target: 7}}, want: "file:7"},
		{name: "commit deletion", screen: "commit", history: historyState{current: commitInfo{hash: "abc"}, rows: []row{{path: "a b", kind: '-', sourceLine: 10, target: 7}}}, want: "https://github.com/owner/repo/blob/abc/a%20b#L7"},
		{name: "file history", screen: "history", history: historyState{commits: []commitInfo{{hash: "abc", filePath: "old name", statsLoaded: true}}}, want: "https://github.com/owner/repo/blob/abc/old%20name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := model{repo: r, screen: tt.screen, rows: tt.rows, history: tt.history}
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
			if cmd == nil {
				t.Fatal("missing copy command")
			}
			if result := cmd().(flashed); !strings.HasPrefix(result.text, "Copied ") {
				t.Fatal(result.text)
			}
			got, err := os.ReadFile(output)
			if err != nil || string(got) != tt.want {
				t.Fatalf("clipboard %q, err %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestSequenceSearchProgressAndNewMatches(t *testing.T) {
	m := model{screen: "sequence", width: 100, height: 12, search: searchState{query: "needle", direction: 1}, sequence: sequenceState{
		commits: []commitInfo{{hash: "one"}, {hash: "two"}},
		changes: [][]change{{{path: "first", diff: "@@ -1 +1 @@\n-old\n+other\n"}}, nil},
		states:  []sequenceLoadState{sequenceReady, sequenceLoading}, errors: make([]string, 2), loading: true, request: 1,
	}}
	m.rebuildSequence()
	if view := plain(m.View()); !strings.Contains(view, "searching 1/2 loaded") {
		t.Fatalf("missing search progress: %q", view)
	}
	updated, _ := m.Update(sequenceLoaded{index: 1, request: 1, commit: commitInfo{hash: "two"}, changes: []change{{path: "second", diff: "@@ -1 +1 @@\n-old\n+needle\n"}}})
	m = updated.(model)
	if !rowMatches(m.rows[m.pos], "needle") || strings.Contains(plain(m.View()), "searching 2/2 loaded") {
		t.Fatalf("new match not selected or loading did not end: %#v", m.rows[m.pos])
	}
}

func TestSequenceDraftFindsMatchesAsTheyLoad(t *testing.T) {
	m := model{screen: "sequence", width: 100, height: 12, modal: "search", search: searchState{draft: "needle", draftDirection: 1}, sequence: sequenceState{
		commits: []commitInfo{{hash: "one"}, {hash: "two"}},
		changes: make([][]change, 2), states: []sequenceLoadState{sequenceLoading, sequenceLoading}, errors: make([]string, 2), loading: true, request: 1,
	}}
	m.rebuildSequence()
	updated, _ := m.Update(sequenceLoaded{index: 0, request: 1, commit: commitInfo{hash: "one"}, changes: []change{{path: "first", diff: "@@ -1 +1 @@\n-old\n+needle\n"}}})
	m = updated.(model)
	if !rowMatches(m.rows[m.pos], "needle") || !strings.Contains(plain(m.View()), "searching 1/2 loaded") {
		t.Fatalf("draft search not updated: row=%#v view=%q", m.rows[m.pos], plain(m.View()))
	}
}

func TestSplitPatchSubmoduleSections(t *testing.T) {
	patch := "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-a\n+b\n" +
		"Submodule space name contains modified content\nSubmodule space name abc1234..def5678:\n  > subject\n" +
		"Submodule new module 0000000...abcdef0 (new submodule)\n"
	sections := splitPatch(patch)
	if len(sections) != 3 || !strings.Contains(sections["space name"], "subject") || !strings.Contains(sections["new module"], "new submodule") || !strings.Contains(sections["file"], "+b") {
		t.Fatalf("sections: %#v", sections)
	}
	rows := buildRows([]change{{path: "new module", diff: sections["new module"]}}, 80, false)
	if len(rows) != 2 || !rows[0].submodule || !strings.Contains(rows[1].text, "0000000 → abcdef0 (new submodule)") {
		t.Fatalf("new submodule: %#v", rows)
	}
}

func TestSubmoduleDiffsAndPicker(t *testing.T) {
	sub := testRepo(t)
	writeFile(t, sub, "file", "one\n")
	if _, err := sub.commit("first"); err != nil {
		t.Fatal(err)
	}
	r := testRepo(t)
	if out, err := r.command("-c", "protocol.file.allow=always", "submodule", "add", sub.root, "module name"); err != nil {
		t.Fatalf("submodule add: %s: %v", out, err)
	}
	if _, err := r.action("commit", "-m", "add submodule"); err != nil {
		t.Fatal(err)
	}
	nested := repository{root: filepath.Join(r.root, "module name")}
	for _, args := range [][]string{{"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := nested.command(args...); err != nil {
			t.Fatalf("config: %s: %v", out, err)
		}
	}
	writeFile(t, nested, "file", "two\n")
	if _, err := nested.commit("second subject"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, nested, "file", "dirty\n")
	snapshot, err := r.load()
	if err != nil || len(snapshot.unstaged) != 1 {
		t.Fatalf("status: %#v, %v", snapshot.unstaged, err)
	}
	c := snapshot.unstaged[0]
	if !strings.Contains(c.diff, "second subject") || !strings.Contains(c.diff, "contains modified content") {
		t.Fatalf("submodule diff: %q", c.diff)
	}
	rows := buildRows([]change{c}, 80, false)
	if len(rows) < 4 || !rows[0].submodule || !strings.Contains(rows[2].text, " → ") || !strings.Contains(rows[3].text, "second subject") {
		t.Fatalf("submodule rows: %#v", rows)
	}
	m := model{rows: rows}
	if entries := m.fileEntries(); len(entries) != 1 || !entries[0].submodule {
		t.Fatalf("picker: %#v", entries)
	}
	if err := r.stage("module name", false); err != nil {
		t.Fatal(err)
	}
	staged, err := r.load()
	if err != nil || len(staged.staged) != 1 || !strings.Contains(staged.staged[0].diff, "second subject") {
		t.Fatalf("staged submodule: %#v, %v", staged.staged, err)
	}
	if _, err := r.action("commit", "-m", "update submodule"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChangesWithoutSyntax("HEAD")
	if err != nil || len(changes) != 1 || !strings.Contains(changes[0].diff, "second subject") {
		t.Fatalf("commit submodule: %#v, %v", changes, err)
	}
}

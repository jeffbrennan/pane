package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
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
	if out, err := r.Command("remote", "add", "origin", "git@github.com:owner/repo.git"); err != nil {
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
		{name: "commit deletion", screen: "commit", history: historyState{current: git.CommitInfo{Hash: "abc"}, rows: []row{{path: "a b", kind: '-', sourceLine: 10, target: 7}}}, want: "https://github.com/owner/repo/blob/abc/a%20b#L7"},
		{name: "file history", screen: "history", history: historyState{commits: []git.CommitInfo{{Hash: "abc", FilePath: "old name", StatsLoaded: true}}}, want: "https://github.com/owner/repo/blob/abc/old%20name"},
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

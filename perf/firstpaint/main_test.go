package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTerminalOutputHandlesSplitSequences(t *testing.T) {
	output := terminalOutput{content: "needle", after: "ready"}
	var replies bytes.Buffer
	data := []byte("\x1b[6n\x1b]11;?\x1b\\read-only read-only needle needle ready ready")
	paints, contents, ready := 0, 0, 0
	for _, b := range data {
		painted, found, isReady := output.feed([]byte{b}, &replies)
		if painted {
			paints++
		}
		if found {
			contents++
		}
		if isReady {
			ready++
		}
	}
	if paints != 1 || contents != 1 || ready != 1 || replies.String() != "\x1b[1;1R\x1b]11;rgb:0000/0000/0000\x1b\\" {
		t.Fatalf("paints=%d contents=%d ready=%d replies=%q", paints, contents, ready, replies.String())
	}
}

func TestResolveWorktrees(t *testing.T) {
	if got := resolveWorktrees("/repo", "", 1); len(got) != 1 || got[0] != "/repo" {
		t.Fatalf("single: %v", got)
	}
	if got := resolveWorktrees("/repo", "/a, /b", 2); len(got) != 2 || got[1] != "/b" {
		t.Fatalf("list: %v", got)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "one"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "two"), 0755); err != nil {
		t.Fatal(err)
	}
	got := resolveWorktrees("/repo", dir, 2)
	if len(got) != 2 {
		t.Fatalf("directory: %v", got)
	}
}

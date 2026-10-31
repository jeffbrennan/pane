package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRepo(t *testing.T) repository {
	t.Helper()
	r := repository{root: t.TempDir()}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := r.command(args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	return r
}

func writeFile(t *testing.T, r repository, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStagingAndUnbornUnstaging(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new file.txt", "hello\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.unstaged) != 1 || !strings.Contains(s.unstaged[0].diff, "+hello") {
		t.Fatalf("untracked: %#v", s)
	}
	if err := r.stage("new file.txt", false); err != nil {
		t.Fatal(err)
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 1 || len(s.unstaged) != 0 {
		t.Fatalf("staged: %#v", s)
	}
	if err := r.stage("new file.txt", true); err != nil {
		t.Fatal(err)
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 0 || len(s.unstaged) != 1 {
		t.Fatalf("unstaged: %#v", s)
	}
}

func TestPartiallyStagedFile(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "initial\n")
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "staged\n")
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "final\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 1 || len(s.unstaged) != 1 {
		t.Fatalf("partial: %#v", s)
	}
	if err := r.stage("file", false); err != nil {
		t.Fatal(err)
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.unstaged) != 0 || !strings.Contains(s.staged[0].diff, "+final") {
		t.Fatalf("whole file: %#v", s)
	}
	if err := r.stage("file", true); err != nil {
		t.Fatal(err)
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 0 || len(s.unstaged) != 1 {
		t.Fatalf("unstaged: %#v", s)
	}
}

func TestBinaryAndDeletedFile(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "binary", "\x00\x01\x02")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.unstaged) != 1 || !strings.Contains(s.unstaged[0].diff, "Binary files") {
		t.Fatalf("binary: %#v", s)
	}
	if err := r.stage("binary", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "binary"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, "binary")); err != nil {
		t.Fatal(err)
	}
	if err := r.stage("binary", false); err != nil {
		t.Fatal(err)
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 1 || len(s.unstaged) != 0 {
		t.Fatalf("deletion: %#v", s)
	}
}

func TestUntrackedDiffPresentation(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new.txt", "first\nsecond\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	rows := buildRows(s.unstaged, 80, true)
	if len(rows) != 3 || rows[1].gutter != "  1 + " || rows[1].text != "first" || rows[2].text != "second" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

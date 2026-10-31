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
	if len(rows) != 3 || rowGutter(rows[1], 3) != "  1 + " || rows[1].text != "first" || rows[2].text != "second" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestCommitStagesAllWhenIndexEmpty(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "tracked", "old\n")
	if err := r.stage("tracked", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "tracked", "updated\n")
	writeFile(t, r, "untracked", "new\n")
	writeFile(t, r, ".gitignore", "ignored\n")
	writeFile(t, r, "ignored", "secret\n")
	if _, err := r.commit("all changes"); err != nil {
		t.Fatal(err)
	}
	out, err := r.command("show", "--format=", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"tracked", "untracked", ".gitignore"} {
		if !strings.Contains(string(out), path+"\n") {
			t.Errorf("missing %q: %s", path, out)
		}
	}
	if strings.Contains(string(out), "\nignored\n") {
		t.Fatalf("ignored file committed: %s", out)
	}
}

func TestCommitPreservesUnstagedChanges(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "staged", "staged content\n")
	writeFile(t, r, "unstaged", "unstaged content\n")
	if err := r.stage("staged", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.commit("staged only"); err != nil {
		t.Fatal(err)
	}
	out, err := r.command("show", "--format=", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "staged\n" {
		t.Fatalf("committed files: %q", out)
	}
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.unstaged) != 1 || s.unstaged[0].path != "unstaged" {
		t.Fatalf("remaining changes: %#v", s)
	}
}

func TestCommitStagesInitialUntrackedFiles(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "first", "initial\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 0 || len(s.unstaged) != 0 {
		t.Fatalf("remaining changes: %#v", s)
	}
}

func TestLoadBatchesDiffsAndFallsBackForQuotedPaths(t *testing.T) {
	r := testRepo(t)
	for _, path := range []string{"plain", "a b", `q"t`} {
		writeFile(t, r, path, "old\n")
	}
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"plain", "a b", `q"t`} {
		writeFile(t, r, path, "staged "+path+"\n")
		if err := r.stage(path, false); err != nil {
			t.Fatal(err)
		}
		writeFile(t, r, path, "unstaged "+path+"\n")
	}
	writeFile(t, r, "untracked", "new\n")
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.staged) != 3 || len(s.unstaged) != 4 || s.unstaged[3].path != "untracked" {
		t.Fatalf("snapshot: %#v", s)
	}
	for _, c := range s.staged {
		if strings.Count(c.diff, "diff --git") != 1 || !strings.Contains(c.diff, "+staged "+c.path) {
			t.Errorf("staged %q: %q", c.path, c.diff)
		}
	}
	for _, c := range s.unstaged[:3] {
		if strings.Count(c.diff, "diff --git") != 1 || !strings.Contains(c.diff, "+unstaged "+c.path) {
			t.Errorf("unstaged %q: %q", c.path, c.diff)
		}
	}
	again, err := r.load()
	if err != nil || !s.equal(again) {
		t.Fatalf("unchanged snapshots differ: %v", err)
	}
}

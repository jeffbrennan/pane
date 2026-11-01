package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRepo(t *testing.T) Repo {
	t.Helper()
	r := Repo{Root: t.TempDir()}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := r.Command(args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	return r
}

func writeFile(t *testing.T, r Repo, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStagingAndUnbornUnstaging(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new file.txt", "hello\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unstaged) != 1 || !strings.Contains(s.Unstaged[0].Diff, "+hello") {
		t.Fatalf("untracked: %#v", s)
	}
	if err := r.Stage("new file.txt", false); err != nil {
		t.Fatal(err)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 1 || len(s.Unstaged) != 0 {
		t.Fatalf("staged: %#v", s)
	}
	if err := r.Stage("new file.txt", true); err != nil {
		t.Fatal(err)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 0 || len(s.Unstaged) != 1 {
		t.Fatalf("unstaged: %#v", s)
	}
}

func TestPartiallyStagedFile(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "initial\n")
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "staged\n")
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "final\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 1 || len(s.Unstaged) != 1 {
		t.Fatalf("partial: %#v", s)
	}
	if err := r.Stage("file", false); err != nil {
		t.Fatal(err)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unstaged) != 0 || !strings.Contains(s.Staged[0].Diff, "+final") {
		t.Fatalf("whole file: %#v", s)
	}
	if err := r.Stage("file", true); err != nil {
		t.Fatal(err)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 0 || len(s.Unstaged) != 1 {
		t.Fatalf("unstaged: %#v", s)
	}
}

func TestBinaryAndDeletedFile(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "binary", "\x00\x01\x02")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unstaged) != 1 || !strings.Contains(s.Unstaged[0].Diff, "Binary files") {
		t.Fatalf("binary: %#v", s)
	}
	if err := r.Stage("binary", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "binary"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.Root, "binary")); err != nil {
		t.Fatal(err)
	}
	if err := r.Stage("binary", false); err != nil {
		t.Fatal(err)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 1 || len(s.Unstaged) != 0 {
		t.Fatalf("deletion: %#v", s)
	}
}

func TestUntrackedDiffPresentation(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new.txt", "first\nsecond\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	rows := buildRows(s.Unstaged, 80, true)
	if len(rows) != 3 || rowGutter(rows[1], 3) != "  1 + " || rows[1].text != "first" || rows[2].text != "second" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestCommitStagesAllWhenIndexEmpty(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "tracked", "old\n")
	if err := r.Stage("tracked", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "tracked", "updated\n")
	writeFile(t, r, "untracked", "new\n")
	writeFile(t, r, ".gitignore", "ignored\n")
	writeFile(t, r, "ignored", "secret\n")
	if _, err := r.Commit("all changes"); err != nil {
		t.Fatal(err)
	}
	out, err := r.Command("show", "--format=", "--name-only", "HEAD")
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
	if err := r.Stage("staged", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("staged only"); err != nil {
		t.Fatal(err)
	}
	out, err := r.Command("show", "--format=", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "staged\n" {
		t.Fatalf("committed files: %q", out)
	}
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unstaged) != 1 || s.Unstaged[0].Path != "unstaged" {
		t.Fatalf("remaining changes: %#v", s)
	}
}

func TestCommitStagesInitialUntrackedFiles(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "first", "initial\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 0 || len(s.Unstaged) != 0 {
		t.Fatalf("remaining changes: %#v", s)
	}
}

func TestLoadBatchesDiffsAndFallsBackForQuotedPaths(t *testing.T) {
	r := testRepo(t)
	for _, path := range []string{"plain", "a b", `q"t`} {
		writeFile(t, r, path, "old\n")
	}
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"plain", "a b", `q"t`} {
		writeFile(t, r, path, "staged "+path+"\n")
		if err := r.Stage(path, false); err != nil {
			t.Fatal(err)
		}
		writeFile(t, r, path, "unstaged "+path+"\n")
	}
	writeFile(t, r, "untracked", "new\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Staged) != 3 || len(s.Unstaged) != 4 || s.Unstaged[3].Path != "untracked" {
		t.Fatalf("snapshot: %#v", s)
	}
	for _, c := range s.Staged {
		if strings.Count(c.Diff, "diff --git") != 1 || !strings.Contains(c.Diff, "+staged "+c.Path) {
			t.Errorf("staged %q: %q", c.Path, c.Diff)
		}
	}
	for _, c := range s.Unstaged[:3] {
		if strings.Count(c.Diff, "diff --git") != 1 || !strings.Contains(c.Diff, "+unstaged "+c.Path) {
			t.Errorf("unstaged %q: %q", c.Path, c.Diff)
		}
	}
	again, err := r.Load()
	if err != nil || !s.Equal(again) {
		t.Fatalf("unchanged snapshots differ: %v", err)
	}
}

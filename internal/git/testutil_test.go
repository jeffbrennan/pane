package git

import (
	"os"
	"path/filepath"
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

func commitFile(t *testing.T, r Repo, path, content, message string) {
	t.Helper()
	writeFile(t, r, path, content)
	if _, err := r.Commit(message); err != nil {
		t.Fatal(err)
	}
}

func fileHistoryFixture(t *testing.T) Repo {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "old name", "first line\nsecond line\nthird line\n")
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "old name", "first line\nsecond line\nthird line\nfourth line\n")
	if _, err := r.Commit("second"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("mv", "old name", "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("rename"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "renamed", "first line\nsecond line\nthird line\nfourth line\nfifth line\n")
	writeFile(t, r, "another", "another file\n")
	if _, err := r.Commit("latest"); err != nil {
		t.Fatal(err)
	}
	return r
}

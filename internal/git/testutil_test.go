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

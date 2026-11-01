package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func changeFor(t *testing.T, r git.Repo, hash, path string) git.Change {
	t.Helper()
	changes, err := r.CommitChanges(hash)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("no change for %s", path)
	return git.Change{}
}

func TestGeneratedByPath(t *testing.T) {
	for _, path := range []string{"go.sum", "bun.lock", "package-lock.json", "Cargo.lock", "sub/dir/yarn.lock", "app.min.js", "test.snap", "styles.min.css"} {
		if !git.GeneratedByPath(path) {
			t.Errorf("%s should be generated", path)
		}
	}
	for _, path := range []string{"main.go", "README.md", "go.mod", "lockfile.go", "snapshot_test.go"} {
		if git.GeneratedByPath(path) {
			t.Errorf("%s should not be generated", path)
		}
	}
}

func TestBuiltinGeneratedFileStartsCollapsed(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "go.sum", strings.Repeat("example.com/mod v1.0.0 h1:abc\n", 5))
	writeFile(t, r, "main.go", "package main\n\nfunc main() {}\n")
	if _, err := r.Commit("add files"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges(commit.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if generated := changeFor(t, r, commit.Hash, "go.sum").Generated; !generated {
		t.Fatal("go.sum was not marked generated")
	}
	m := model{width: 100, height: 40, wrap: true, screen: "commit", history: historyState{current: commit, meta: commit, changes: changes, request: 1}}
	m.history.loading = false
	m.rebuildCommit()
	view := m.commitModel()
	var sumRows, codeRows int
	for _, r := range view.rows {
		if r.path == "go.sum" {
			sumRows++
		}
		if r.path == "main.go" && r.kind == '+' {
			codeRows++
		}
	}
	if sumRows != 1 || codeRows == 0 {
		t.Fatalf("go.sum rows=%d main.go added rows=%d", sumRows, codeRows)
	}
	rendered := view.View()
	if !strings.Contains(rendered, "generated · +5 −0") {
		t.Fatalf("rendered header lacks counts: %q", rendered)
	}

	// l on the header opens the file for this session.
	pos := -1
	for i, r := range view.rows {
		if r.path == "go.sum" && r.kind == 'f' {
			pos = i
		}
	}
	m.history.pos = pos
	updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	opened := updated.(model)
	count := 0
	for _, r := range opened.history.rows {
		if r.path == "go.sum" {
			count++
		}
	}
	if count <= 1 {
		t.Fatalf("l did not reopen the generated file: %d rows", count)
	}
}

func TestGitattributesGeneratedAndDiffMarkers(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, ".gitattributes", "vendor.txt linguist-generated\nsecret.txt -diff\n")
	writeFile(t, r, "vendor.txt", "generated one\ngenerated two\n")
	writeFile(t, r, "secret.txt", "secret one\n")
	writeFile(t, r, "normal.txt", "normal one\n")
	if _, err := r.Commit("attributes"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitChanges(commit.Hash); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"vendor.txt", "secret.txt"} {
		if !changeFor(t, r, commit.Hash, path).Generated {
			t.Fatalf("%s was not marked generated", path)
		}
	}
	if changeFor(t, r, commit.Hash, "normal.txt").Generated {
		t.Fatal("normal.txt was marked generated")
	}
}

func TestWorkingTreeGeneratedFileStartsCollapsed(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "main.go", "package main\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "go.sum", "example.com/mod v1.0.0 h1:abc\n")
	writeFile(t, r, "main.go", "package main\n\n// changed\n")
	snapshot, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 40, wrap: true, snapshot: snapshot}
	m.rebuild()
	sumRows := 0
	for _, row := range m.rows {
		if row.path == "go.sum" && row.kind != 's' {
			sumRows++
		}
	}
	if sumRows != 1 {
		t.Fatalf("go.sum rows=%d", sumRows)
	}
}

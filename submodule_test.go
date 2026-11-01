package main

import (
	"path/filepath"
	"strings"
	"testing"
)

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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func renameFixture(t *testing.T, oldPath, newPath string, edit bool) (repository, string) {
	t.Helper()
	r := testRepo(t)
	var content strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&content, "original line %d\n", i)
	}
	writeFile(t, r, oldPath, content.String())
	if err := r.stage(oldPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("mv", "--", oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if edit {
		writeFile(t, r, newPath, strings.Replace(content.String(), "original line 20", "changed line 20", 1))
		if err := r.stage(newPath, false); err != nil {
			t.Fatal(err)
		}
	}
	return r, content.String()
}

func TestStagedRenameAndUnstage(t *testing.T) {
	r, original := renameFixture(t, "old.go", "new.go", true)
	cache, _, err := r.loadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.snapshot.staged) != 1 {
		t.Fatalf("staged: %#v", cache.snapshot.staged)
	}
	c := cache.snapshot.staged[0]
	if c.oldPath != "old.go" || c.path != "new.go" || !strings.Contains(c.diff, "rename from old.go") || !strings.Contains(c.diff, "+changed line 20") {
		t.Fatalf("rename: %#v", c)
	}
	full, err := r.load()
	if err != nil || !full.equal(cache.snapshot) {
		t.Fatalf("full load differs: %#v, %v", full, err)
	}
	m := stagingModel(t, r, true)
	if m.rows[0].text != "   old.go" || m.rows[1].text != " → new.go" {
		t.Fatalf("headers: %#v", m.rows[:2])
	}
	stageAt(t, m, " → new.go")
	out, err := r.command("show", ":old.go")
	if err != nil || string(out) != original {
		t.Fatalf("old path not restored: %q, %v", out, err)
	}
	if _, err := r.command("show", ":new.go"); err == nil {
		t.Fatal("new path remains in index")
	}
	if _, err := os.Stat(filepath.Join(r.root, "new.go")); err != nil {
		t.Fatalf("working file removed: %v", err)
	}
}

func TestCommitRenameContextStatsAndPicker(t *testing.T) {
	r, _ := renameFixture(t, "old.go", "new.go", true)
	if _, err := r.action("commit", "-m", "rename"); err != nil {
		t.Fatal(err)
	}
	out, err := r.command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(out))
	changes, err := r.commitChanges(hash)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes: %#v, %v", changes, err)
	}
	c := changes[0]
	if c.oldPath != "old.go" || c.path != "new.go" || !strings.Contains(c.diff, "+changed line 20") {
		t.Fatalf("rename: %#v", c)
	}
	context, err := r.fullContext(contextKey{path: c.path, commit: hash}, c.diff, c.oldPath)
	if err != nil || !strings.Contains(context, " original line 0") || !strings.Contains(context, "-original line 20") {
		t.Fatalf("expanded context: %q, %v", context, err)
	}
	stats, err := r.historyStats([]string{hash})
	if err != nil || len(stats) != 1 || stats[0].added != 1 || stats[0].deleted != 1 {
		t.Fatalf("stats: %#v, %v", stats, err)
	}
	m := model{rows: buildRows(changes, 80, true)}
	entries := filterFiles(m.fileEntries(), "old.go")
	if len(entries) != 1 || entries[0].path != "new.go" {
		t.Fatalf("picker: %#v", entries)
	}
}

func TestPureRenameAndQuotedPaths(t *testing.T) {
	r, _ := renameFixture(t, "old name.txt", "new ü\"name.txt", false)
	cache, _, err := r.loadCached(nil)
	if err != nil || len(cache.snapshot.staged) != 1 {
		t.Fatalf("staged: %#v, %v", cache, err)
	}
	if cache.snapshot.staged[0].oldPath != "old name.txt" || cache.snapshot.staged[0].path != "new ü\"name.txt" {
		t.Fatalf("staged paths: %#v", cache.snapshot.staged[0])
	}
	if _, err := r.action("commit", "-m", "rename"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges("HEAD")
	if err != nil || len(changes) != 1 || changes[0].oldPath != "old name.txt" || changes[0].path != "new ü\"name.txt" {
		t.Fatalf("changes: %#v, %v", changes, err)
	}
	rows := buildRows(changes, 80, true)
	if len(rows) != 3 || rows[2].text != "  Renamed, no content changes" {
		t.Fatalf("rows: %#v", rows)
	}
	stats, err := r.historyStats([]string{"HEAD"})
	if err != nil || len(stats) != 1 || stats[0].added != 0 || stats[0].deleted != 0 {
		t.Fatalf("stats: %#v, %v", stats, err)
	}
}

func TestRenameHeadersWrapAndBehaveAsOneFile(t *testing.T) {
	c := change{oldPath: "old/a/very/long/filename.go", path: "new/a/very/long/filename.go", diff: "similarity index 100%\n"}
	m := model{width: 20, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{c}}}
	m.rebuild()
	if len(m.fileEntries()) != 1 {
		t.Fatalf("duplicate picker entries: %#v", m.fileEntries())
	}
	var old, new strings.Builder
	for _, r := range m.rows {
		if r.kind == 'f' || r.kind == 'F' {
			if strings.Contains(r.text, "→") || new.Len() > 0 {
				new.WriteString(strings.TrimPrefix(r.text, " "))
			} else {
				old.WriteString(strings.TrimPrefix(r.text, " "))
			}
		}
	}
	if !strings.Contains(old.String(), c.oldPath) || !strings.Contains(new.String(), c.path) {
		t.Fatalf("wrapped paths: %q, %q", old.String(), new.String())
	}
	m.move(1)
	if m.rows[m.pos].kind == 'F' {
		t.Fatal("navigation stopped inside the header")
	}
	m.pos = 1
	updated, _ := m.changeContext("h")
	collapsed := updated.(model)
	if len(collapsed.rows) < 2 || collapsed.rows[1].kind != 'F' {
		t.Fatalf("collapsed rename lost its header: %#v", collapsed.rows)
	}
	m.pos = len(m.rows) - 1
	if pinned := m.fileHeaderAt(m.pos); pinned == nil || !strings.Contains(pinned.text, c.path) {
		t.Fatalf("pinned header does not show destination: %#v", pinned)
	}
}

func TestRenameUnstageDoesNotOverwriteRestagedSource(t *testing.T) {
	r, _ := renameFixture(t, "old", "new", false)
	writeFile(t, r, "old", "separate content\n")
	if err := r.stage("old", false); err != nil {
		t.Fatal(err)
	}
	err := r.stageChange(change{path: "new", oldPath: "old"}, true)
	if err == nil || !strings.Contains(err.Error(), "separate staged changes") {
		t.Fatalf("error: %v", err)
	}
	out, err := r.command("show", ":old")
	if err != nil || string(out) != "separate content\n" {
		t.Fatalf("source overwritten: %q, %v", out, err)
	}
}

func TestUnstageHunkKeepsRename(t *testing.T) {
	for _, name := range []string{"new ü\".go", "new name.go", "new\tname.go"} {
		t.Run(name, func(t *testing.T) {
			r, original := renameFixture(t, "old.go", name, true)
			stageAt(t, stagingModel(t, r, true), "changed line 20")
			out, err := r.command("show", ":"+name)
			if err != nil || string(out) != original {
				t.Fatalf("renamed index content: %q, %v", out, err)
			}
			if _, err := r.command("show", ":old.go"); err == nil {
				t.Fatal("unstaging a hunk undid the rename")
			}
			s, err := r.load()
			if err != nil || len(s.staged) != 1 || s.staged[0].oldPath != "old.go" || len(s.unstaged) != 1 || !strings.Contains(s.unstaged[0].diff, "+changed line 20") {
				t.Fatalf("changes: %#v, %v", s, err)
			}
		})
	}
}

func TestRenameStatisticsWithMultipleCommitsAndTabPaths(t *testing.T) {
	r, _ := renameFixture(t, "old\tfile", "new\tfile", true)
	if _, err := r.action("commit", "-m", "rename"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "additional", "another\n")
	if _, err := r.commit("another"); err != nil {
		t.Fatal(err)
	}
	stats, err := r.historyStats([]string{"HEAD", "HEAD~1", "HEAD~2"})
	if err != nil || len(stats) != 3 || stats[0].added != 1 || stats[1].added != 1 || stats[1].deleted != 1 || stats[2].added != 40 {
		t.Fatalf("stats: %#v, %v", stats, err)
	}
	changes, err := r.commitChanges("HEAD~1")
	if err != nil || len(changes) != 1 || changes[0].oldPath != "old\tfile" || changes[0].path != "new\tfile" {
		t.Fatalf("changes: %#v, %v", changes, err)
	}
}

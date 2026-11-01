package ui

import (
	"context"
	"fmt"
	"github.com/jeffbrennan/pane/internal/git"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func renameFixture(t *testing.T, oldPath, newPath string, edit bool) (git.Repo, string) {
	t.Helper()
	r := testRepo(t)
	var content strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&content, "original line %d\n", i)
	}
	writeFile(t, r, oldPath, content.String())
	if err := r.Stage(oldPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("mv", "--", oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if edit {
		writeFile(t, r, newPath, strings.Replace(content.String(), "original line 20", "changed line 20", 1))
		if err := r.Stage(newPath, false); err != nil {
			t.Fatal(err)
		}
	}
	return r, content.String()
}

func TestStagedRenameAndUnstage(t *testing.T) {
	r, original := renameFixture(t, "old.go", "new.go", true)
	cache, _, err := r.LoadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Snapshot.Staged) != 1 {
		t.Fatalf("staged: %#v", cache.Snapshot.Staged)
	}
	c := cache.Snapshot.Staged[0]
	if c.OldPath != "old.go" || c.Path != "new.go" || !strings.Contains(c.Diff, "rename from old.go") || !strings.Contains(c.Diff, "+changed line 20") {
		t.Fatalf("rename: %#v", c)
	}
	full, err := r.Load()
	if err != nil || !full.Equal(cache.Snapshot) {
		t.Fatalf("full load differs: %#v, %v", full, err)
	}
	m := stagingModel(t, r, true)
	if m.rows[0].text != "   old.go" || m.rows[1].text != " → new.go" {
		t.Fatalf("headers: %#v", m.rows[:2])
	}
	stageAt(t, m, " → new.go")
	out, err := r.Command("show", ":old.go")
	if err != nil || string(out) != original {
		t.Fatalf("old path not restored: %q, %v", out, err)
	}
	if _, err := r.Command("show", ":new.go"); err == nil {
		t.Fatal("new path remains in index")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "new.go")); err != nil {
		t.Fatalf("working file removed: %v", err)
	}
}

func TestCommitRenameContextStatsAndPicker(t *testing.T) {
	r, _ := renameFixture(t, "old.go", "new.go", true)
	if _, err := r.Action("commit", "-m", "rename"); err != nil {
		t.Fatal(err)
	}
	out, err := r.Command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(out))
	changes, err := r.CommitChanges(hash)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes: %#v, %v", changes, err)
	}
	c := changes[0]
	if c.OldPath != "old.go" || c.Path != "new.go" || !strings.Contains(c.Diff, "+changed line 20") {
		t.Fatalf("rename: %#v", c)
	}
	context, err := r.FullContext(git.Context{Path: c.Path, Commit: hash}, c.Diff, c.OldPath)
	if err != nil || !strings.Contains(context, " original line 0") || !strings.Contains(context, "-original line 20") {
		t.Fatalf("expanded context: %q, %v", context, err)
	}
	stats, err := r.HistoryStats([]string{hash})
	if err != nil || len(stats) != 1 || stats[0].Added != 1 || stats[0].Deleted != 1 {
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
	cache, _, err := r.LoadCached(nil)
	if err != nil || len(cache.Snapshot.Staged) != 1 {
		t.Fatalf("staged: %#v, %v", cache, err)
	}
	if cache.Snapshot.Staged[0].OldPath != "old name.txt" || cache.Snapshot.Staged[0].Path != "new ü\"name.txt" {
		t.Fatalf("staged paths: %#v", cache.Snapshot.Staged[0])
	}
	if _, err := r.Action("commit", "-m", "rename"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges("HEAD")
	if err != nil || len(changes) != 1 || changes[0].OldPath != "old name.txt" || changes[0].Path != "new ü\"name.txt" {
		t.Fatalf("changes: %#v, %v", changes, err)
	}
	rows := buildRows(changes, 80, true)
	if len(rows) != 3 || rows[2].text != "  Renamed, no content changes" {
		t.Fatalf("rows: %#v", rows)
	}
	stats, err := r.HistoryStats([]string{"HEAD"})
	if err != nil || len(stats) != 1 || stats[0].Added != 0 || stats[0].Deleted != 0 {
		t.Fatalf("stats: %#v, %v", stats, err)
	}
}

func TestHistoryAndChangesIgnoreRenameLimitWarning(t *testing.T) {
	r := testRepo(t)
	for i := 0; i < 4; i++ {
		writeFile(t, r, fmt.Sprintf("old%d", i), fmt.Sprintf("old content %d\n", i))
	}
	if _, err := r.Commit("base"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := os.Remove(filepath.Join(r.Root, fmt.Sprintf("old%d", i))); err != nil {
			t.Fatal(err)
		}
		writeFile(t, r, fmt.Sprintf("new%d", i), fmt.Sprintf("new content %d\n", i))
	}
	if _, err := r.Commit("replace"); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Command("config", "diff.renameLimit", "1"); err != nil {
		t.Fatalf("config: %s: %v", out, err)
	}
	out, err := r.Command("log", "-1", "--numstat", "-M", "--format=", "HEAD")
	if err != nil || !strings.Contains(string(out), "exhaustive rename detection was skipped") {
		t.Fatalf("expected rename warning: %q, %v", out, err)
	}
	stats, err := r.HistoryStats([]string{"HEAD"})
	if err != nil || len(stats) != 1 || stats[0].Added != 4 || stats[0].Deleted != 4 {
		t.Fatalf("stats: %#v, %v", stats, err)
	}
	for name, load := range map[string]func() ([]git.Change, error){
		"commit": func() ([]git.Change, error) { return r.CommitChangesWithoutSyntax("HEAD") },
		"base": func() ([]git.Change, error) {
			var loaded []git.Change
			for chunk := range r.StreamBase(context.Background(), "HEAD~1", 200) {
				if chunk.Err != nil {
					return nil, chunk.Err
				}
				loaded = append(loaded, chunk.Changes...)
			}
			return loaded, nil
		},
	} {
		changes, err := load()
		if err != nil || len(changes) != 8 {
			t.Fatalf("%s changes: %#v, %v", name, changes, err)
		}
		for _, c := range changes {
			if strings.Contains(c.Diff, "exhaustive rename detection was skipped") {
				t.Fatalf("%s diff contains warning: %q", name, c.Diff)
			}
		}
	}
}

func TestRenameHeadersWrapAndBehaveAsOneFile(t *testing.T) {
	c := git.Change{OldPath: "old/a/very/long/filename.go", Path: "new/a/very/long/filename.go", Diff: "similarity index 100%\n"}
	m := model{width: 20, height: 8, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{c}}}
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
	if !strings.Contains(old.String(), c.OldPath) || !strings.Contains(new.String(), c.Path) {
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
	if pinned := m.fileHeaderAt(m.pos); pinned == nil || !strings.Contains(pinned.text, c.Path) {
		t.Fatalf("pinned header does not show destination: %#v", pinned)
	}
}

func TestRenameUnstageDoesNotOverwriteRestagedSource(t *testing.T) {
	r, _ := renameFixture(t, "old", "new", false)
	writeFile(t, r, "old", "separate content\n")
	if err := r.Stage("old", false); err != nil {
		t.Fatal(err)
	}
	err := r.StageChange(git.Change{Path: "new", OldPath: "old"}, true)
	if err == nil || !strings.Contains(err.Error(), "separate staged changes") {
		t.Fatalf("error: %v", err)
	}
	out, err := r.Command("show", ":old")
	if err != nil || string(out) != "separate content\n" {
		t.Fatalf("source overwritten: %q, %v", out, err)
	}
}

func TestUnstageHunkKeepsRename(t *testing.T) {
	for _, name := range []string{"new ü\".go", "new name.go", "new\tname.go"} {
		t.Run(name, func(t *testing.T) {
			r, original := renameFixture(t, "old.go", name, true)
			stageAt(t, stagingModel(t, r, true), "changed line 20")
			out, err := r.Command("show", ":"+name)
			if err != nil || string(out) != original {
				t.Fatalf("renamed index content: %q, %v", out, err)
			}
			if _, err := r.Command("show", ":old.go"); err == nil {
				t.Fatal("unstaging a hunk undid the rename")
			}
			s, err := r.Load()
			if err != nil || len(s.Staged) != 1 || s.Staged[0].OldPath != "old.go" || len(s.Unstaged) != 1 || !strings.Contains(s.Unstaged[0].Diff, "+changed line 20") {
				t.Fatalf("changes: %#v, %v", s, err)
			}
		})
	}
}

func TestRenameStatisticsWithMultipleCommitsAndTabPaths(t *testing.T) {
	r, _ := renameFixture(t, "old\tfile", "new\tfile", true)
	if _, err := r.Action("commit", "-m", "rename"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "additional", "another\n")
	if _, err := r.Commit("another"); err != nil {
		t.Fatal(err)
	}
	stats, err := r.HistoryStats([]string{"HEAD", "HEAD~1", "HEAD~2"})
	if err != nil || len(stats) != 3 || stats[0].Added != 1 || stats[1].Added != 1 || stats[1].Deleted != 1 || stats[2].Added != 40 {
		t.Fatalf("stats: %#v, %v", stats, err)
	}
	changes, err := r.CommitChanges("HEAD~1")
	if err != nil || len(changes) != 1 || changes[0].OldPath != "old\tfile" || changes[0].Path != "new\tfile" {
		t.Fatalf("changes: %#v, %v", changes, err)
	}
}

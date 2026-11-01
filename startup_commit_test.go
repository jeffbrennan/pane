package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInitialModelOpensCommitRevision(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file.txt", "first\n")
	if _, err := r.Commit("first commit"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("tag", "-a", "release", "-m", "release"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "second\n")
	if _, err := r.Commit("second commit"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file.txt", "uncommitted\n")
	for _, revision := range []string{hash, hash[:8], "release", "HEAD~1"} {
		t.Run(revision, func(t *testing.T) {
			m, err := initialModel(r, []string{revision})
			if err != nil {
				t.Fatal(err)
			}
			if m.screen != "commit" || !m.history.loading || m.history.current.Hash != hash || m.history.current.Subject != "first commit" {
				t.Fatalf("incorrect startup model: %#v", m.history)
			}
			batch, ok := m.Init()().(tea.BatchMsg)
			if !ok || len(batch) != 4 {
				t.Fatalf("missing startup commands: %#v", batch)
			}
			result, ok := batch[2]().(commitStreamLoaded)
			if !ok || result.chunk.Err != nil || len(result.chunk.Changes) != 1 || !strings.Contains(result.chunk.Changes[0].Diff, "+first") {
				t.Fatalf("incorrect commit diff: %#v", result)
			}
			m.width, m.height = 60, 12
			updated, _ := m.Update(result)
			m = updated.(model)
			if !strings.Contains(m.View(), "first commit") || !strings.Contains(m.View(), "first") {
				t.Fatalf("missing commit review: %q", m.View())
			}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if got := updated.(model); got.screen != "history" || !got.history.loading || cmd == nil {
				t.Fatalf("history not loaded on return: %#v", got.history)
			}
		})
	}
}

func TestInitialModelRejectsInvalidCommitArguments(t *testing.T) {
	r := testRepo(t)
	for _, args := range [][]string{{"missing"}, {""}, {"--help"}, {"HEAD", "extra"}} {
		if _, err := initialModel(r, args); err == nil {
			t.Fatalf("accepted invalid arguments: %q", args)
		}
	}
	writeFile(t, r, "file", "text\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	blob, err := r.Action("rev-parse", "HEAD:file")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initialModel(r, []string{blob}); err == nil {
		t.Fatal("accepted a non-commit object")
	}
}

func TestDirectCommitBackBeforeLoading(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "text\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	m, err := initialModel(r, []string{"HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	request := m.history.request
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != "history" || cmd == nil || m.history.request == request {
		t.Fatal("back did not load history and invalidate the commit request")
	}
	updated, _ = m.Update(commitLoaded{request: request, changes: []Change{{Path: "stale"}}})
	if got := updated.(model); got.screen != "history" || len(got.history.changes) != 0 {
		t.Fatal("stale commit response changed history")
	}
}

func TestInitialModelOpensCommitRange(t *testing.T) {
	r := testRepo(t)
	var hashes []string
	for _, content := range []string{"first", "second", "third"} {
		writeFile(t, r, "file", content+"\n")
		if _, err := r.Commit(content); err != nil {
			t.Fatal(err)
		}
		hash, err := r.Action("rev-parse", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	writeFile(t, r, "file", "uncommitted\n")
	for _, revision := range []string{hashes[0][:8] + ".." + hashes[2][:8], "HEAD~2..HEAD", "HEAD~2.."} {
		t.Run(revision, func(t *testing.T) {
			m, err := initialModel(r, []string{revision})
			if err != nil {
				t.Fatal(err)
			}
			if m.screen != "sequence" || len(m.sequence.commits) != 2 || m.sequence.commits[0].Hash != hashes[2] || m.sequence.commits[1].Hash != hashes[1] {
				t.Fatalf("incorrect range order or boundaries: %#v", m.sequence.commits)
			}
			batch := m.Init()().(tea.BatchMsg)
			first := batch[2]().(sequenceChunkLoaded)
			last := batch[3]().(sequenceChunkLoaded)
			if first.chunk.Err != nil || last.chunk.Err != nil || first.chunk.Meta.Hash != hashes[2] || last.chunk.Meta.Hash != hashes[1] {
				t.Fatalf("range diffs did not load: %#v %#v", first.chunk, last.chunk)
			}
		})
	}
	for _, revision := range []string{"missing..HEAD", "HEAD..HEAD", "--all..HEAD"} {
		if _, err := initialModel(r, []string{revision}); err == nil {
			t.Errorf("accepted invalid or empty range %q", revision)
		}
	}
}

func TestInitialModelOpensSymmetricRange(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base", "base\n")
	if _, err := r.Commit("base"); err != nil {
		t.Fatal(err)
	}
	base, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "feature", "feature\n")
	if _, err := r.Commit("feature"); err != nil {
		t.Fatal(err)
	}
	feature, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("checkout", "--detach", base); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "other", "other\n")
	if _, err := r.Commit("other"); err != nil {
		t.Fatal(err)
	}
	other, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	m, err := initialModel(r, []string{"HEAD...feature"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, commit := range m.sequence.commits {
		got[commit.Hash] = true
	}
	if m.screen != "sequence" || len(m.sequence.commits) != 2 || !got[feature] || !got[other] || got[base] {
		t.Fatalf("incorrect symmetric range: %#v", m.sequence.commits)
	}
}

func TestCommitFirstScreenIsHighlighted(t *testing.T) {
	r := testRepo(t)
	body := func(word string) string {
		return "package main\n" + strings.Repeat("var "+word+" = 1\n", startupSyntaxLines)
	}
	writeFile(t, r, "a.go", body("a"))
	writeFile(t, r, "b.go", body("b"))
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChangesFirstScreen(hash, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].SyntaxDiff == "" || changes[1].SyntaxDiff != "" {
		t.Fatal("only the first screen's file should be highlighted when a commit opens")
	}
	focused, err := r.CommitChangesFirstScreen(hash, "b.go")
	if err != nil {
		t.Fatal(err)
	}
	if focused[0].SyntaxDiff != "" || focused[1].SyntaxDiff == "" {
		t.Fatal("the first screen should start at the focused file")
	}
	msg, ok := loadCommitSyntax(r, hash, 1, changes)().(commitSyntaxLoaded)
	if !ok || msg.changes[0].SyntaxDiff != changes[0].SyntaxDiff || msg.changes[1].SyntaxDiff == "" {
		t.Fatal("follow-up load should highlight the deferred file")
	}
	if loadCommitSyntax(r, hash, 1, msg.changes) != nil {
		t.Fatal("nothing should be left to highlight")
	}
}

func TestSkippedFilesHighlightNearViewport(t *testing.T) {
	r := testRepo(t)
	// a.go fills the screen. The rest exceed the syntax budget, so they
	// highlight on demand: b.go through d.go as lookahead, f.go once visible.
	writeFile(t, r, "a.go", "package main\n"+strings.Repeat("var a = 1\n", 30))
	for _, name := range []string{"b", "f"} {
		writeFile(t, r, name+".go", "package main\n"+strings.Repeat("var "+name+" = 1\n", syntaxBudgetLines))
	}
	for _, name := range []string{"c", "d", "e"} {
		writeFile(t, r, name+".go", "package main\nvar "+name+" = 1\n")
	}
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	hash, err := r.Action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	m, err := initialModel(r, []string{hash})
	if err != nil {
		t.Fatal(err)
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	changes, err := r.CommitChangesFirstScreen(hash, "")
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range changes {
		if c.SyntaxSkipped != (i > 0) || (c.SyntaxDiff != "") != (i == 0) {
			t.Fatalf("%s: skipped %t, highlighted %t", c.Path, c.SyntaxSkipped, c.SyntaxDiff != "")
		}
	}
	highlighted := func(m model, path string) bool {
		for _, r := range m.history.rows {
			if r.path == path && r.kind == '+' && len(r.syntaxSpans()) == 0 {
				return false
			}
		}
		return true
	}
	updated, cmd := m.Update(commitLoaded{changes, nil, m.history.request})
	m = runCmd(t, updated.(model), cmd)
	for path, want := range map[string]bool{"a.go": true, "b.go": true, "c.go": true, "d.go": true, "e.go": false, "f.go": false} {
		if highlighted(m, path) != want {
			t.Fatalf("%s highlighted %t after opening", path, !want)
		}
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if cmd == nil {
		t.Fatal("no syntax load for the visible file")
	}
	m = runCmd(t, updated.(model), cmd)
	if !highlighted(m, "f.go") {
		t.Fatal("visible skipped file was not highlighted")
	}
	if updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}); cmd != nil {
		t.Fatal("loaded file was requested again")
	}
	m = keys(t, updated.(model), "w", "w")
	for _, path := range []string{"b.go", "f.go"} {
		if !highlighted(m, path) {
			t.Fatalf("rebuild lost on-demand highlighting of %s", path)
		}
	}
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(model)
}

// runCmd runs cmd, including batched commands, and applies their messages.
func runCmd(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = runCmd(t, m, c)
		}
	case nil:
	default:
		m = update(t, m, msg)
	}
	return m
}

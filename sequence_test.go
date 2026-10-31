package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPipedCommitReview(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "shared.txt", strings.Repeat("first\n", 20))
	if _, err := r.commit("first commit"); err != nil {
		t.Fatal(err)
	}
	first, err := r.action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "shared.txt", "second\n")
	if _, err := r.commit("second commit"); err != nil {
		t.Fatal(err)
	}
	second, err := r.action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf("%s fake subject\n%s another subject\n", second[:8], first[:8])
	m, err := initialSequence(r, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if m.screen != "sequence" || len(m.sequence.commits) != 2 || m.sequence.commits[0].subject != "second commit" || m.sequence.commits[1].hash != first {
		t.Fatalf("incorrect commit input: %#v", m.sequence.commits)
	}
	batch := m.Init()().(tea.BatchMsg)
	result := batch[2]().(sequenceLoaded)
	if result.err != nil || len(result.changes) != 2 {
		t.Fatalf("could not load commits: %#v", result)
	}
	m.width, m.height = 60, 8
	updated, _ := m.Update(result)
	m = updated.(model)
	if !strings.Contains(m.View(), "second commit") || !strings.Contains(m.View(), "shared.txt") {
		t.Fatalf("missing initial review: %q", m.View())
	}
	if strings.Count(m.View(), "second commit") != 1 {
		t.Fatalf("duplicate commit heading: %q", m.View())
	}
	for i, row := range m.rows {
		if row.commitIndex == 1 && row.kind == '+' && row.sourceLine == 10 {
			m.pos, m.viewTop = i, i-1
			break
		}
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "first commit") || !strings.Contains(lines[1], "shared.txt") || !strings.Contains(view, "first") || strings.Contains(lines[0], "second commit") {
		t.Fatalf("incorrect pinned headers: %q", view)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != "history" || cmd == nil || !m.history.loading {
		t.Fatalf("back did not open history: %#v", m.history)
	}
	m.screen = ""
	if len(m.rows) != 0 {
		t.Fatalf("sequence rows leaked into working tree: %#v", m.rows)
	}
}

func TestPipedCommitInputValidation(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "text\n")
	if _, err := r.commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "\n ", "not-a-hash subject\n", "--help\n", "deadbeef subject\n", strings.Repeat("x", 70000)} {
		if _, err := initialSequence(r, strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid input %q", input[:min(len(input), 40)])
		}
	}
}

func TestSequenceHeadersAndResizeKeepCommitIdentity(t *testing.T) {
	m := model{width: 30, height: 8, wrap: true, screen: "sequence", sequence: sequenceState{
		commits: []commitInfo{{hash: "11111111", subject: "first"}, {hash: "22222222", subject: "second"}},
		changes: [][]change{{longFile("same.go")}, {longFile("same.go")}},
	}}
	m.rebuildSequence()
	for i, row := range m.rows {
		if row.commitIndex == 1 && row.kind == '+' && row.sourceLine == 5 {
			m.pos, m.viewTop = i, i-2
			break
		}
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = updated.(model)
	if r := m.rows[m.pos]; r.commitIndex != 1 || r.sourceLine != 5 {
		t.Fatalf("resize lost the selected commit: %#v", r)
	}
	for i, row := range m.rows {
		if row.commitIndex == 1 && row.kind == 'f' && m.fileHeaderAt(i) != nil {
			t.Fatal("previous file header leaks into next commit")
		}
	}
}

func TestSequenceDeletedFilesAreVisible(t *testing.T) {
	diff := "diff --git a/file b/file\ndeleted file mode 100644\n--- a/file\n+++ /dev/null\n@@ -1 +0,0 @@\n-removed\n"
	m := model{width: 40, height: 8, wrap: true, screen: "sequence", sequence: sequenceState{
		commits: []commitInfo{{hash: "11111111", subject: "deletion"}},
		changes: [][]change{{{path: "file", diff: diff}}},
	}}
	m.rebuildSequence()
	if !strings.Contains(m.View(), "removed") {
		t.Fatalf("deleted content missing from review: %q", m.View())
	}
}

func TestEmptyCommitDoesNotPinPreviousFile(t *testing.T) {
	m := model{width: 40, height: 6, wrap: true, screen: "sequence", sequence: sequenceState{
		commits: []commitInfo{{hash: "11111111", subject: "first"}, {hash: "22222222", subject: "empty"}},
		changes: [][]change{{longFile("file.go")}, nil},
	}}
	m.rebuildSequence()
	last := len(m.rows) - 1
	if m.rows[last].text != "  No file changes" || m.fileHeaderAt(last) != nil {
		t.Fatalf("empty commit inherited previous file header: %#v", m.rows[last])
	}
}

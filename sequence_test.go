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
	if m.screen != "sequence" || len(m.sequence.commits) != 2 || m.sequence.commits[0].hash != second[:8] || m.sequence.commits[1].hash != first[:8] {
		t.Fatalf("incorrect commit input: %#v", m.sequence.commits)
	}
	batch := m.Init()().(tea.BatchMsg)
	result := batch[2]().(sequenceLoaded)
	if result.err != nil || result.index != 0 || len(result.changes) != 1 {
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
	result = batch[3]().(sequenceLoaded)
	updated, _ = m.Update(result)
	m = updated.(model)
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
	if strings.Contains(lines[0], "pane") || strings.Contains(lines[0], "COMMIT") || !strings.Contains(lines[0], first[:8]) {
		t.Fatalf("unexpected commit heading: %q", lines[0])
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
	for _, input := range []string{"", "\n ", "not-a-hash subject\n", "--help\n", strings.Repeat("x", 70000)} {
		if _, err := initialSequence(r, strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid input %q", input[:min(len(input), 40)])
		}
	}
	m, err := initialSequence(r, strings.NewReader("deadbeef subject\n"))
	if err != nil {
		t.Fatal(err)
	}
	batch := m.Init()().(tea.BatchMsg)
	result := batch[2]().(sequenceLoaded)
	if result.err == nil {
		t.Fatal("invalid hash was not reported")
	}
	updated, _ := m.Update(result)
	m = updated.(model)
	if m.sequence.states[0] != sequenceFailed || !strings.Contains(m.sequence.errors[0], "invalid commit") {
		t.Fatalf("invalid commit not shown in sequence: %#v", m.sequence)
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

func TestSequenceKeepsGutterWidthPerCommit(t *testing.T) {
	m := model{width: 50, height: 10, wrap: true, screen: "sequence", sequence: sequenceState{
		commits: []commitInfo{{hash: "one"}, {hash: "two"}},
		changes: [][]change{
			{{path: "one", diff: "@@ -0,0 +1 @@\n+first\n"}},
			{{path: "two", diff: "@@ -0,0 +10000 @@\n+second\n"}},
		},
	}}
	for iteration := 0; iteration < 2; iteration++ {
		m.rebuildSequence()
		if len(m.sequence.gutterWidths) != 2 || m.sequence.gutterWidths[0] != 3 || m.sequence.gutterWidths[1] != 5 {
			t.Fatalf("incorrect per-commit widths: %v", m.sequence.gutterWidths)
		}
		view := m
		view.screen, view.sequenceGutters, view.relativeLines = "", true, true
		for index, r := range m.rows {
			if r.kind != '+' {
				continue
			}
			want := "  1 + "
			if r.commitIndex == 1 {
				want = "10000 + "
			}
			if got := rowGutter(r, view.rowGutterWidth(r)); got != want {
				t.Fatalf("commit %d gutter = %q, want %q", r.commitIndex, got, want)
			}
			view.pos = index
			if rendered := plain(view.renderNumberedDiffRow(r, index)); !strings.Contains(rendered, want+r.text) {
				t.Fatalf("commit %d rendered with incorrect alignment: %q", r.commitIndex, rendered)
			}
		}
		if iteration == 0 {
			m.sequence.gutterWidths = nil
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

func TestSequencePickerUsesHoveredCommit(t *testing.T) {
	for _, back := range []string{"esc", "q"} {
		t.Run(back, func(t *testing.T) {
			m := model{width: 80, height: 12, wrap: true, screen: "sequence", sequence: sequenceState{
				commits: []commitInfo{{hash: "11111111", subject: "first"}, {hash: "22222222", subject: "second"}},
				changes: [][]change{
					{longFile("shared.go"), longFile("only-first.go")},
					{longFile("shared.go"), longFile("only-second.go")},
				},
			}}
			m.rebuildSequence()
			for i, r := range m.rows {
				if r.commitIndex == 1 && r.path == "shared.go" && r.kind == '+' {
					m.pos = i
					break
				}
			}
			m.viewTop = 0
			m = keys(t, m, "f")
			if m.modal != "files" || !m.picker.commitScoped || m.picker.commitIndex != 1 {
				t.Fatalf("picker has wrong scope: %#v", m.picker)
			}
			view := m.View()
			if !strings.Contains(view, "22222222") || !strings.Contains(view, "only-second.go") || strings.Contains(view, "only-first.go") || !strings.Contains(plain(view), "22222222 · 2/2") {
				t.Fatalf("picker shows wrong files: %q", view)
			}
			m = keys(t, m, "/", "o", "n", "l", "y", "-", "s", "e", "c", "o", "n", "d", "enter")
			if m.modal != "" || m.rows[m.pos].path != "only-second.go" || m.rows[m.pos].commitIndex != 1 {
				t.Fatalf("selected wrong file: %#v", m.rows[m.pos])
			}
			m = keys(t, m, back)
			if m.modal != "files" || m.picker.query != "only-second" || m.picker.commitIndex != 1 {
				t.Fatalf("picker context not restored: %#v", m.picker)
			}
			m = keys(t, m, back)
			if m.screen != "history" || m.modal != "" || len(m.rows) != 0 {
				t.Fatalf("picker back did not restore history and working diff: screen=%q rows=%#v", m.screen, m.rows)
			}
		})
	}
}

func TestSequencePickerEmptyCommit(t *testing.T) {
	m := model{width: 60, height: 10, screen: "sequence", sequence: sequenceState{
		commits: []commitInfo{{hash: "11111111", subject: "first"}, {hash: "22222222", subject: "empty"}},
		changes: [][]change{{longFile("first.go")}, nil},
	}}
	m.rebuildSequence()
	m.pos = len(m.rows) - 1
	m = keys(t, m, "f")
	if view := m.View(); !strings.Contains(view, "No matching files") || strings.Contains(view, "first.go") {
		t.Fatalf("empty commit picker shows other commits: %q", view)
	}
	m = keys(t, m, "esc")
	if m.screen != "sequence" || m.modal != "" {
		t.Fatal("closing initial picker should return to sequence")
	}
}

func TestSequenceLoadsFirstAndLastBeforeMiddle(t *testing.T) {
	m, err := initialSequence(repository{}, strings.NewReader("aaaa first\nbbbb second\ncccc third\ndddd last\n"))
	if err != nil {
		t.Fatal(err)
	}
	batch := m.Init()().(tea.BatchMsg)
	if len(batch) != 4 || m.sequence.states[0] != sequenceLoading || m.sequence.states[3] != sequenceLoading || m.sequence.states[1] != sequencePending || m.sequence.states[2] != sequencePending {
		t.Fatalf("first and last were not prioritized: %#v", m.sequence.states)
	}
	m.width, m.height = 40, 8
	m.rebuildSequence()
	m = keys(t, m, "G")
	if !m.sequence.endPending || m.rows[m.pos].commitIndex != 3 {
		t.Fatal("G did not select the last loading commit")
	}
	updated, next := m.Update(sequenceLoaded{request: 1, index: 3, commit: commitInfo{hash: "dddd", subject: "last"}, changes: []change{longFile("last.go")}})
	m = updated.(model)
	if next == nil || m.sequence.states[1] != sequenceLoading || m.rows[m.pos].commitIndex != 3 || m.rows[m.pos].sourceLine != 20 || m.sequence.endPending {
		t.Fatalf("last commit did not load at the end or schedule the next: %#v row=%#v", m.sequence.states, m.rows[m.pos])
	}
	oldOffset := m.pos - m.viewTop
	updated, next = m.Update(sequenceLoaded{request: 1, index: 0, commit: commitInfo{hash: "aaaa", subject: "first"}, changes: []change{longFile("first.go")}})
	m = updated.(model)
	if next == nil || m.sequence.states[2] != sequenceLoading || m.rows[m.pos].commitIndex != 3 || m.rows[m.pos].sourceLine != 20 || m.pos-m.viewTop != oldOffset {
		t.Fatalf("first commit load moved the selected last line: pos=%d top=%d row=%#v", m.pos, m.viewTop, m.rows[m.pos])
	}
	updated, _ = m.Update(sequenceLoaded{request: 1, index: 1, commit: commitInfo{hash: "bbbb"}, changes: []change{longFile("middle.go")}})
	m = updated.(model)
	if m.rows[m.pos].commitIndex != 3 || m.rows[m.pos].sourceLine != 20 {
		t.Fatal("loading another commit lost the selection")
	}
	updated, _ = m.Update(sequenceLoaded{request: 1, index: 2, commit: commitInfo{hash: "cccc"}})
	m = updated.(model)
	if m.sequence.loading || m.sequence.states[2] != sequenceReady {
		t.Fatal("sequence remained loading after all commits completed")
	}
}

func TestSequenceSingleCommitStartsOneLoad(t *testing.T) {
	m, err := initialSequence(repository{}, strings.NewReader("aaaa\n"))
	if err != nil {
		t.Fatal(err)
	}
	batch := m.Init()().(tea.BatchMsg)
	if len(batch) != 3 {
		t.Fatalf("single commit started multiple loads: %d", len(batch))
	}
}

package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func streamModel() model {
	return model{width: 80, height: 12, wrap: true, screen: "commit", history: historyState{request: 1, loading: true, streamActive: true, current: git.CommitInfo{Hash: "commit"}}}
}

func deliverChunk(t *testing.T, m model, chunk git.CommitChunk) model {
	t.Helper()
	updated, _ := m.update(commitStreamLoaded{request: m.history.request, chunk: chunk})
	return updated.(model)
}

func TestStreamedRowsMatchFullBuildAndWidenGutter(t *testing.T) {
	changes := []git.Change{
		{Path: "one", Diff: "@@ -0,0 +1,2 @@\n+first\n+second\n"},
		{Path: "two", Diff: "@@ -1 +10001 @@\n-old\n+new\n"},
		{Path: "three", Diff: "@@ -0,0 +1 @@\n+last\n"},
	}
	m := streamModel()
	paths := []string{"one", "two", "three"}
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: changes[:1], Loaded: 1})
	if m.history.streamGutter != 4 || m.history.gutterWidth != 4 || len(m.history.rows) == 0 {
		t.Fatal("first streaming chunk did not use a four-column gutter")
	}
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: changes[1:2], Loaded: 2})
	if m.history.streamGutter != 5 || m.history.gutterWidth != 5 {
		t.Fatal("later large line number did not widen the gutter")
	}
	for _, r := range m.history.rows {
		if r.path == "one" && r.kind == '+' && r.sourceLine == 1 && rowGutter(r, m.commitModel().rowGutterWidth(r)) != "    1 + " {
			t.Fatal("existing rows did not adopt the wider gutter")
		}
	}
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: changes[2:], Loaded: 3, Done: true})
	want, _ := buildRowsDisplay(changes, m.commitModel().diffContentWidth(), m.wrap, nil, 5, false)
	if !reflect.DeepEqual(m.history.rows, want) {
		t.Fatalf("streamed rows differ from a complete build:\ngot %#v\nwant %#v", m.history.rows, want)
	}
	if m.history.loading || m.history.streamActive {
		t.Fatal("stream remained active after completion")
	}
}

func TestSingleChunkKeepsNormalGutter(t *testing.T) {
	m := deliverChunk(t, streamModel(), git.CommitChunk{Paths: []string{"one"}, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+line\n"}}, Loaded: 1, Done: true})
	if m.history.streamGutter != 3 || m.history.gutterWidth != 3 {
		t.Fatal("single chunk changed the normal gutter width")
	}
}

func TestStreamedRowsRespectCollapsedFiles(t *testing.T) {
	m := streamModel()
	m.collapsedFiles = map[contextKey]bool{{commit: "commit", path: "two"}: true}
	changes := []git.Change{
		{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"},
		{Path: "two", Diff: "@@ -0,0 +1 @@\n+second\n"},
	}
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Changes: changes[:1], Loaded: 1})
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Changes: changes[1:], Loaded: 2, Done: true})
	view := m.commitModel()
	view.rebuild()
	if !reflect.DeepEqual(m.history.rows, view.rows) {
		t.Fatal("incremental rows differ from a full build with a collapsed file")
	}
}

func TestVeryLargeStreamAvoidsLateGutterRebuild(t *testing.T) {
	m := streamModel()
	paths := make([]string, 5000)
	paths[0] = "one"
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+line\n"}}, Loaded: 1})
	if m.history.streamGutter != 5 {
		t.Fatal("very large stream did not reserve a wider gutter")
	}
}

func TestStreamPickerJumpAndSearch(t *testing.T) {
	m := streamModel()
	paths := []string{"one", "two"}
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}}, Loaded: 1})
	view := m.commitModel()
	entries := view.fileEntries()
	if len(entries) != 2 || entries[1].row != -1 {
		t.Fatalf("picker does not include unloaded file: %#v", entries)
	}
	view.openPicker()
	view.picker.selected = 1
	view.handlePickerKey("enter", nil)
	if view.picker.pendingPath != "two" {
		t.Fatal("unloaded picker selection did not wait")
	}
	m.picker = view.picker
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: []git.Change{{Path: "two", Diff: "@@ -0,0 +1 @@\n+needle\n"}}, Loaded: 2, Done: true})
	if m.picker.pendingPath != "" || m.history.rows[m.history.pos].path != "two" {
		t.Fatal("pending picker jump was not taken")
	}
	m = streamModel()
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}}, Loaded: 1})
	m = m.inDiff(func(view *model) { view.startSearch(); view.search.draft = "needle"; view.updateSearchDraft() })
	m = deliverChunk(t, m, git.CommitChunk{Paths: paths, Changes: []git.Change{{Path: "two", Diff: "@@ -0,0 +1 @@\n+needle\n"}}, Loaded: 2, Done: true})
	if !strings.Contains(m.history.rows[m.history.pos].text, "needle") {
		t.Fatal("search did not find a newly streamed match")
	}
}

func TestStreamEndAndStaleMessages(t *testing.T) {
	m := streamModel()
	first := git.Change{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Changes: []git.Change{first}, Loaded: 1})
	updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(model)
	if !m.history.streamEndPending {
		t.Fatal("G did not follow the end while loading")
	}
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Changes: []git.Change{{Path: "two", Diff: "@@ -0,0 +1 @@\n+last\n"}}, Loaded: 2, Done: true})
	if m.history.pos != len(m.history.rows)-1 || m.history.streamEndPending {
		t.Fatal("G did not finish at the end")
	}
	updated, _ = m.update(commitStreamLoaded{request: m.history.request - 1, chunk: git.CommitChunk{Changes: []git.Change{{Path: "stale"}}}})
	if len(updated.(model).history.changes) != 2 {
		t.Fatal("stale chunk changed the commit")
	}
}

func TestStreamNavigationWaitsPastLoadedEnd(t *testing.T) {
	for _, keys := range [][]string{{"J"}, {"]", "c"}} {
		m := streamModel()
		m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}}, Loaded: 1})
		m.history.pos = len(m.history.rows) - 1
		for _, key := range keys {
			updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			m = updated.(model)
		}
		if !m.history.streamEndPending {
			t.Fatalf("%v did not wait at the loaded end", keys)
		}
	}
}

func TestCountedStreamNavigationWaitsPastLoadedEnd(t *testing.T) {
	for _, keys := range [][]string{{"2", "J"}, {"2", "]", "c"}} {
		m := streamModel()
		changes := []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}, {Path: "two", Diff: "@@ -0,0 +1 @@\n+second\n"}}
		m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two", "three"}, Changes: changes, Loaded: 2})
		m.history.pos = 1
		for _, key := range keys {
			updated, _ := m.handleCommitKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			m = updated.(model)
		}
		if !m.history.streamEndPending {
			t.Fatalf("%v did not wait after exhausting loaded matches", keys)
		}
	}
}

func TestStreamErrorKeepsLoadedRows(t *testing.T) {
	m := streamModel()
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}}, Loaded: 1})
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one", "two"}, Loaded: 1, Done: true, Err: errors.New("read failed")})
	if m.history.streamActive || len(m.history.rows) == 0 || !strings.Contains(m.View(), "read failed") {
		t.Fatal("stream error did not retain the loaded diff and show the error")
	}
}

func TestWhitespaceRestartsAndCancelsStream(t *testing.T) {
	m := streamModel()
	repo := m.startCommitLoad()
	m = deliverChunk(t, m, git.CommitChunk{Paths: []string{"one"}, Changes: []git.Change{{Path: "one", Diff: "@@ -0,0 +1 @@\n+first\n"}}, Loaded: 1})
	oldRequest := m.history.request
	updated, cmd := m.toggleWhitespace()
	m = updated.(model)
	defer m.cancelCommitLoad()
	if repo.Context.Err() == nil || m.history.request == oldRequest || !m.history.streamActive || len(m.history.rows) != 0 || cmd == nil {
		t.Fatal("whitespace toggle did not cancel and restart the stream")
	}
	updated, _ = m.update(commitStreamLoaded{request: oldRequest, chunk: git.CommitChunk{Changes: []git.Change{{Path: "stale"}}}})
	if len(updated.(model).history.rows) != 0 {
		t.Fatal("old whitespace stream changed the new view")
	}
}

func TestCommitViewConsumesRealStream(t *testing.T) {
	r := testRepo(t)
	for i := 0; i < 30; i++ {
		writeFile(t, r, fmt.Sprintf("file-%02d.txt", i), strings.Repeat("line\n", 10))
	}
	if _, err := r.Commit("streamed"); err != nil {
		t.Fatal(err)
	}
	m, err := initialModel(r, []string{"HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.cancelCommitLoad()
	m.width, m.height = 80, 12
	cmd := startCommitStream(m.commitRepository(), m.history.current.Hash, m.history.request, m.height)
	chunks := 0
	for cmd != nil {
		msg := cmd().(commitStreamLoaded)
		chunks++
		updated, next := m.update(msg)
		m, cmd = updated.(model), next
	}
	if chunks < 2 || m.history.loading || len(m.history.changes) != 30 || len(m.history.rows) == 0 {
		t.Fatalf("streamed commit incomplete: chunks=%d changes=%d rows=%d", chunks, len(m.history.changes), len(m.history.rows))
	}
}

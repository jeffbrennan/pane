package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func whitespaceRepo(t *testing.T) git.Repo {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "file", "one\ntwo\nthree\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWhitespaceDiffAndCache(t *testing.T) {
	r := whitespaceRepo(t)
	writeFile(t, r, "file", "  one\ntwo changed\nthree\n")
	before, _, err := r.LoadCached(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before.Snapshot.Unstaged[0].Diff, "+  one") {
		t.Fatal("normal diff omitted whitespace change")
	}
	r.IgnoreSpace = true
	after, unchanged, err := r.LoadCached(before)
	if err != nil || unchanged {
		t.Fatalf("cache was not invalidated: %v %v", unchanged, err)
	}
	diff := after.Snapshot.Unstaged[0].Diff
	if strings.Contains(diff, "+  one") || !strings.Contains(diff, "+two changed") {
		t.Fatalf("unexpected whitespace diff: %q", diff)
	}
	writeFile(t, r, "file", "  one\ntwo\nthree\n")
	only, err := r.Load()
	if err != nil || len(only.Unstaged) != 1 || only.Unstaged[0].Diff != "Whitespace-only changes" {
		t.Fatalf("whitespace-only diff: %#v %v", only, err)
	}
	rows := buildRows(only.Unstaged, 80, false)
	if len(rows) < 2 || !strings.Contains(rows[1].text, "Whitespace-only changes") {
		t.Fatalf("missing whitespace metadata: %#v", rows)
	}
}

func TestWhitespaceToggleGuardsActionsAndRefresh(t *testing.T) {
	r := whitespaceRepo(t)
	writeFile(t, r, "file", "  one\ntwo changed\nthree\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, snapshot: s, width: 80, height: 15}
	m.rebuild()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	m = updated.(model)
	if !m.repo.IgnoreSpace || !m.whitespaceRefreshing || cmd == nil {
		t.Fatalf("toggle: %#v", m)
	}
	if view := strings.ToLower(m.View()); strings.Count(view, "ignoring whitespace") != 1 {
		t.Fatalf("expected one whitespace indicator: %q", view)
	}
	updated, cmd = m.stageSelection(false)
	if cmd != nil || !strings.Contains(updated.(model).flash, "Wait") {
		t.Fatal("staging was not blocked while refreshing")
	}
	updated, _ = m.Update(load(m.repo, m.generation, nil)())
	m = updated.(model)
	if m.whitespaceRefreshing || !strings.Contains(m.View(), "ignoring whitespace") {
		t.Fatalf("refresh or badge: %q", m.View())
	}
	for i, row := range m.rows {
		if row.kind == '+' {
			m.pos = i
			break
		}
	}
	updated, cmd = m.stageSelection(true)
	if cmd != nil || !strings.Contains(updated.(model).flash, "Turn off whitespace") {
		t.Fatal("hunk staging was not blocked")
	}
	updated, cmd = m.startDiscard()
	if cmd != nil || !strings.Contains(updated.(model).flash, "Turn off whitespace") {
		t.Fatal("discard was not blocked")
	}
	updated, cmd = m.stageSelection(false)
	if cmd == nil {
		t.Fatal("whole-file staging was blocked")
	}
	if result := cmd().(completed); result.err != nil {
		t.Fatal(result.err)
	}
	staged, err := r.Command("diff", "--cached")
	if err != nil || !strings.Contains(string(staged), "+  one") || !strings.Contains(string(staged), "+two changed") {
		t.Fatalf("whole-file stage: %q %v", staged, err)
	}
}

func TestWhitespaceInCommitAndSequence(t *testing.T) {
	r := whitespaceRepo(t)
	writeFile(t, r, "file", "  one\ntwo changed\nthree\n")
	if _, err := r.Commit("change"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	r.IgnoreSpace = true
	changes, err := r.CommitChanges(commit.Hash)
	if err != nil || len(changes) != 1 || strings.Contains(changes[0].Diff, "+  one") || !strings.Contains(changes[0].Diff, "+two changed") {
		t.Fatalf("historical diff: %#v %v", changes, err)
	}
	m := sequenceStreamModel([]git.CommitInfo{{Hash: commit.Hash}})
	m.repo = r
	m.sequence.states[0] = sequenceLoading
	message := m.startSequenceStream(0)().(sequenceChunkLoaded)
	if message.chunk.Err != nil || len(message.chunk.Changes) != 1 || strings.Contains(message.chunk.Changes[0].Diff, "+  one") {
		t.Fatalf("sequence diff: %#v", message.chunk)
	}
	m.resetSequenceStream()
}

func TestWhitespaceToggleRejectsStaleLoads(t *testing.T) {
	r := whitespaceRepo(t)
	writeFile(t, r, "file", "  one\ntwo\nthree\n")
	m := model{repo: r, width: 80, height: 15}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	m = updated.(model)
	oldRepo, oldGeneration := m.repo, m.generation
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	m = updated.(model)
	if m.flash != "Showing whitespace" {
		t.Fatalf("missing disabled message: %q", m.flash)
	}
	updated, cmd := m.Update(load(oldRepo, oldGeneration, nil)())
	m = updated.(model)
	if cmd == nil || !m.whitespaceRefreshing || m.repo.IgnoreSpace {
		t.Fatalf("stale load applied: %#v", m)
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if m.whitespaceRefreshing || len(m.snapshot.Unstaged) != 1 || !strings.Contains(m.snapshot.Unstaged[0].Diff, "+  one") {
		t.Fatalf("latest diff not restored: %#v", m.snapshot)
	}
}

func TestWhitespaceToggleReloadsHistoricalViews(t *testing.T) {
	r := whitespaceRepo(t)
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, screen := range []string{"commit", "sequence"} {
		t.Run(screen, func(t *testing.T) {
			m := model{repo: r, width: 80, height: 15, screen: screen, history: historyState{current: commit, request: 3}, sequence: sequenceState{commits: []git.CommitInfo{commit}, states: []sequenceLoadState{sequenceReady}, changes: [][]git.Change{{}}, request: 3}}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
			m = updated.(model)
			if cmd == nil || !m.repo.IgnoreSpace {
				t.Fatal("toggle did not start reload")
			}
			if screen == "commit" {
				if m.history.request != 4 || !m.history.loading {
					t.Fatalf("commit not reloading: %#v", m.history)
				}
				updated, _ = m.Update(commitLoaded{request: 3, changes: []git.Change{{Path: "stale"}}})
				if len(updated.(model).history.changes) != 0 {
					t.Fatal("accepted stale commit load")
				}
			} else {
				if m.sequence.request != 4 || m.sequence.states[0] != sequenceLoading {
					t.Fatalf("sequence not reloading: %#v", m.sequence)
				}
				updated, _ = m.Update(sequenceLoaded{request: 3, index: 0, changes: []git.Change{{Path: "stale"}}})
				if updated.(model).sequence.states[0] != sequenceLoading {
					t.Fatal("accepted stale sequence load")
				}
			}
		})
	}
}

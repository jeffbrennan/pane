package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestDiffVerticalMotions(t *testing.T) {
	m := model{width: 40, height: 12, wrap: true, rows: make([]row, 50)}
	steps := []struct {
		key  string
		want int
	}{
		{"ctrl+d", 5}, {"ctrl+u", 0}, {"ctrl+f", 10}, {"ctrl+b", 0},
		{"G", 49}, {"g", 49}, {"g", 0}, {"end", 49}, {"home", 0},
		{"pgdown", 10}, {"pgup", 0},
	}
	for _, step := range steps {
		if !m.handleDiffNavigation(step.key) || m.pos != step.want {
			t.Fatalf("%s: position %d, want %d", step.key, m.pos, step.want)
		}
	}
}

func TestViewportAlignmentSurvivesRefresh(t *testing.T) {
	diff := "@@ -0,0 +1,40 @@\n" + strings.Repeat("+line\n", 40)
	s := git.Snapshot{Unstaged: []git.Change{{Path: "file", Diff: diff}}}
	m := model{width: 40, height: 12, wrap: true, snapshot: s}
	m.rebuild()
	m.move(20)
	m.handleDiffNavigation("z")
	m.handleDiffNavigation("t")
	updated, _ := m.Update(loaded{snapshot: s, generation: m.generation})
	got := updated.(model)
	if got.pos != 20 || got.viewportStart(10) != 20 {
		t.Fatalf("alignment changed on refresh: pos %d top %d", got.pos, got.viewportStart(10))
	}
}

func TestDiffViewportMotions(t *testing.T) {
	m := model{width: 40, height: 12, wrap: true, rows: make([]row, 50), pos: 20}
	for _, test := range []struct {
		keys []string
		top  int
	}{
		{[]string{"z", "t"}, 20}, {[]string{"z", "z"}, 15}, {[]string{"z", "b"}, 11},
	} {
		for _, key := range test.keys {
			m.handleDiffNavigation(key)
		}
		if m.pos != 20 || m.viewportStart(10) != test.top {
			t.Fatalf("%v: pos %d top %d", test.keys, m.pos, m.viewportStart(10))
		}
	}
	m.handleDiffNavigation("z")
	m.handleDiffNavigation("z")
	m.handleDiffNavigation("ctrl+e")
	if m.viewTop != 16 || m.pos != 20 {
		t.Fatalf("scroll down: top %d pos %d", m.viewTop, m.pos)
	}
	m.handleDiffNavigation("ctrl+y")
	if m.viewTop != 15 || m.pos != 20 {
		t.Fatalf("scroll up: top %d pos %d", m.viewTop, m.pos)
	}
	for i := 0; i < 10; i++ {
		m.handleDiffNavigation("ctrl+e")
	}
	if m.pos < m.viewTop || m.pos >= m.viewTop+10 {
		t.Fatal("active line left viewport")
	}
}

func TestHorizontalVimMotions(t *testing.T) {
	m := model{width: 40}
	for _, key := range []string{"z", "l", "z", "L"} {
		m.handleDiffNavigation(key)
	}
	if m.horizontal != 24 {
		t.Fatalf("horizontal offset: %d", m.horizontal)
	}
	for _, key := range []string{"z", "h", "ctrl+h"} {
		m.handleDiffNavigation(key)
	}
	if m.horizontal != 16 {
		t.Fatalf("horizontal offset: %d", m.horizontal)
	}
	m.wrap = true
	m.handleDiffNavigation("l")
	if m.horizontal != 16 {
		t.Fatal("horizontal scrolling changed while wrapped")
	}
}

func TestCommitDiffNavigation(t *testing.T) {
	m := model{width: 40, height: 12, wrap: true, screen: "commit", history: historyState{rows: make([]row, 50)}}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlD}, {Type: tea.KeyRunes, Runes: []rune("z")}, {Type: tea.KeyRunes, Runes: []rune("t")}} {
		updated, _ := m.Update(key)
		m = updated.(model)
	}
	if m.history.pos != 5 || m.history.viewTop != 5 {
		t.Fatalf("commit position %d top %d", m.history.pos, m.history.viewTop)
	}
	for _, key := range []string{"g", "g"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(model)
	}
	if m.history.pos != 0 || m.screen != "commit" {
		t.Fatal("gg failed in commit viewer")
	}
}

func TestHistoryNavigationAndSearchInput(t *testing.T) {
	m := model{screen: "history", height: 15, history: historyState{commits: make([]git.CommitInfo, 20), list: listState{selected: 10}}}
	for _, key := range []string{"g", "g"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(model)
	}
	if m.history.list.selected != 0 {
		t.Fatal("history gg failed")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(model)
	if m.history.list.selected != 19 {
		t.Fatal("history G failed")
	}
	m.history.list.filtering = true
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if updated.(model).history.list.draft != "g" {
		t.Fatal("navigation intercepted search text")
	}
}

func TestCountedMotionsAndSourceLines(t *testing.T) {
	m := model{width: 40, height: 10, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{
		{Path: "first", Diff: "@@ -10,3 +10,3 @@\n ten\n-old\n+new\n twelve\n"},
		{Path: "second", Diff: "@@ -30 +30 @@\n-other\n+next\n"},
	}}}
	m.rebuild()
	m = keys(t, m, "3", "j")
	if m.pos != 3 {
		t.Fatalf("3j: %d", m.pos)
	}
	m = keys(t, m, "2", "k")
	if m.pos != 1 {
		t.Fatalf("2k: %d", m.pos)
	}
	m = keys(t, m, "1", "2", "G")
	if m.rows[m.pos].path != "first" || m.rows[m.pos].target != 12 {
		t.Fatalf("12G: %#v", m.rows[m.pos])
	}
	m = keys(t, m, "1", "0", "g", "g")
	if m.rows[m.pos].target != 10 {
		t.Fatalf("10gg: %#v", m.rows[m.pos])
	}
	m = keys(t, m, "1", "J")
	if m.rows[m.pos].path != "second" {
		t.Fatal("counted file jump failed")
	}
	m = keys(t, m, "1", "G")
	if m.rows[m.pos].path != "second" || m.rows[m.pos].target != 30 {
		t.Fatalf("1G should select nearest current-file line: %#v", m.rows[m.pos])
	}
}

func TestChangeBlockMotionsAndJumpList(t *testing.T) {
	m := model{width: 50, height: 12, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{
		{Path: "first", Diff: "@@ -1,5 +1,5 @@\n keep\n-old\n+new\n middle\n-before\n+after\n"},
		{Path: "second", Diff: "@@ -1 +1 @@\n-old\n+new\n"},
	}}}
	m.rebuild()
	m = keys(t, m, "2", "]", "c")
	if m.rows[m.pos].text != "before" {
		t.Fatalf("2]c: %#v", m.rows[m.pos])
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = updated.(model)
	if m.pos != 0 {
		t.Fatalf("ctrl-o: %d", m.pos)
	}
	m = keys(t, m, "g", "i")
	if m.rows[m.pos].text != "before" {
		t.Fatalf("gi: %#v", m.rows[m.pos])
	}
	m = keys(t, m, "[", "c")
	if m.rows[m.pos].text != "old" {
		t.Fatalf("[c: %#v", m.rows[m.pos])
	}
	m = keys(t, m, "]", "c", "]", "c")
	if m.rows[m.pos].path != "second" {
		t.Fatalf("cross-file ]c: %#v", m.rows[m.pos])
	}
}

func TestScreenRelativeMotions(t *testing.T) {
	m := model{width: 40, height: 12, rows: make([]row, 50), pos: 20, viewTop: 15}
	m = keys(t, m, "g", "H")
	if m.pos != 15 || m.viewTop != 15 {
		t.Fatalf("gH: %d %d", m.pos, m.viewTop)
	}
	m = keys(t, m, "g", "M")
	if m.pos != 19 || m.viewTop != 15 {
		t.Fatalf("gM: %d %d", m.pos, m.viewTop)
	}
	m = keys(t, m, "g", "L")
	if m.pos != 24 || m.viewTop != 15 {
		t.Fatalf("gL: %d %d", m.pos, m.viewTop)
	}
}

func TestReverseSearchAndJumpBack(t *testing.T) {
	m := model{width: 40, height: 10, rows: []row{{text: "match"}, {text: "other"}, {text: "match"}, {text: "other"}, {text: "match"}}, pos: 3}
	m = keys(t, m, "g", "?", "m", "a", "t", "c", "h", "enter")
	if m.pos != 2 {
		t.Fatalf("reverse search: %d", m.pos)
	}
	m = keys(t, m, "n")
	if m.pos != 0 {
		t.Fatalf("reverse n: %d", m.pos)
	}
	m = keys(t, m, "N")
	if m.pos != 2 {
		t.Fatalf("reverse N: %d", m.pos)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = updated.(model)
	if m.pos != 0 {
		t.Fatalf("search ctrl-o: %d", m.pos)
	}
	m = keys(t, m, "g", "i")
	if m.pos != 2 {
		t.Fatalf("search gi: %d", m.pos)
	}
}

func TestHistoryCounts(t *testing.T) {
	m := model{screen: "history", height: 15, history: historyState{commits: make([]git.CommitInfo, 30)}}
	m = keys(t, m, "1", "0", "j")
	if m.history.list.selected != 10 {
		t.Fatalf("10j: %d", m.history.list.selected)
	}
	m = keys(t, m, "5", "G")
	if m.history.list.selected != 4 {
		t.Fatalf("5G: %d", m.history.list.selected)
	}
}

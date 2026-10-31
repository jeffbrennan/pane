package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	s := snapshot{unstaged: []change{{path: "file", diff: diff}}}
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
	m := model{screen: "history", height: 15, history: historyState{commits: make([]commitInfo, 20), selected: 10}}
	for _, key := range []string{"g", "g"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(model)
	}
	if m.history.selected != 0 {
		t.Fatal("history gg failed")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(model)
	if m.history.selected != 19 {
		t.Fatal("history G failed")
	}
	m.history.searching = true
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if updated.(model).history.draft != "g" {
		t.Fatal("navigation intercepted search text")
	}
}

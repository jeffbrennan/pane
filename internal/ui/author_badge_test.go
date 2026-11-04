package ui

import (
	"strings"
	"testing"

	"github.com/jeffbrennan/pane/internal/git"
)

func TestAuthorInitials(t *testing.T) {
	for name, want := range map[string]string{
		"Jeff Brennan":          "JB",
		"ada lovelace-king":     "AK",
		"Cher":                  "CH",
		"X":                     "X ",
		"":                      "? ",
		"dependabot[bot]":       "DE",
		"Jean-Luc van der Berg": "JB",
		"李小龙":                   "李",
	} {
		if got := authorInitials(name); got != want {
			t.Errorf("authorInitials(%q) = %q, want %q", name, got, want)
		}
		if got := stringWidth(authorInitials(name)); got != 2 {
			t.Errorf("authorInitials(%q) is %d cells wide, want 2", name, got)
		}
	}
}

func TestAuthorColorIsStableByName(t *testing.T) {
	m := model{}
	a := m.authorColor(git.CommitInfo{Author: "Jeff Brennan", Email: "jeff@example.com"})
	b := m.authorColor(git.CommitInfo{Author: "jeff  brennan", Email: "1+jeff@users.noreply.github.com"})
	if a != b {
		t.Fatalf("one name across emails should share a color: %q vs %q", a, b)
	}
	if got := m.authorColor(git.CommitInfo{Author: "renovate[bot]"}); got != styleDim {
		t.Fatalf("bots should be dimmed, got %q", got)
	}
	m.lightTheme = true
	if m.authorColor(git.CommitInfo{Author: "Jeff Brennan"}) == a {
		t.Fatal("light theme should use its own palette")
	}
}

func TestHistoryAuthorGutterJoinsRuns(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", history: historyState{commits: []git.CommitInfo{
		{Hash: "aaaaaaaa", Author: "Alice Ng", Email: "alice@x", Subject: "first", StatsLoaded: true},
		{Hash: "bbbbbbbb", Author: "Alice Ng", Email: "alice@x", Subject: "second", StatsLoaded: true},
		{Hash: "cccccccc", Author: "Bob Ray", Email: "bob@x", Subject: "third", StatsLoaded: true},
	}}}
	view := plain(m.historyView())
	for _, want := range []string{
		" AN ▸ first\n │    ",
		" │  \n AN   second",  // same author: the bar carries across the gap
		"\n    \n BR   third", // new author: the gap stays empty
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in:\n%s", want, view)
		}
	}
}

func TestHistoryAuthorBadgesToggle(t *testing.T) {
	m := model{width: 60, height: 12, screen: "history", history: historyState{commits: []git.CommitInfo{
		{Hash: "aaaaaaaa", Author: "Alice Ng", Subject: "first", StatsLoaded: true},
	}}}
	m, _ = press(t, m, "a")
	if !m.hideAuthorBadges || m.flash != "Author badges hidden" {
		t.Fatalf("a should hide badges, flash %q", m.flash)
	}
	if view := plain(m.historyView()); strings.Contains(view, "AN") || !strings.Contains(view, "▸ first") {
		t.Fatalf("hidden badges still rendered:\n%s", view)
	}
	m, _ = press(t, m, "a")
	if m.hideAuthorBadges {
		t.Fatal("a should show badges again")
	}
	m.width = authorGutterMinWidth - 1
	if m.historyGutter() != 0 {
		t.Fatal("narrow terminals should drop the gutter")
	}
}

func TestHistoryBotCommits(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", history: historyState{commits: []git.CommitInfo{
		{Hash: "aaaaaaaa", Author: "Alice Ng", Subject: "first", StatsLoaded: true},
		{Hash: "bbbbbbbb", Author: "ghostty-vouch[bot]", Subject: "vouch", StatsLoaded: true},
		{Hash: "cccccccc", Author: "Bob Ray", Subject: "third", StatsLoaded: true},
	}}}
	if view := plain(m.historyView()); strings.Contains(view, "vouch") || !strings.Contains(view, "bots hidden") {
		t.Fatalf("bot commits should be hidden by default and say so:\n%s", view)
	}
	m.history.list.selected = 1 // third, with the bot hidden
	m, _ = press(t, m, "B")
	view := plain(m.historyView())
	if !m.showBotCommits || !strings.Contains(view, " ⚙    vouch") || strings.Contains(view, "bots hidden") {
		t.Fatalf("B should show bot commits with a gear badge:\n%s", view)
	}
	if got := m.filteredHistory()[m.history.list.selected].Hash; got != "cccccccc" {
		t.Fatalf("selection should stay on the same commit, got %s", got)
	}
	m, _ = press(t, m, "B")
	if got := m.filteredHistory()[m.history.list.selected].Hash; got != "cccccccc" {
		t.Fatalf("selection should survive hiding bots, got %s", got)
	}
}

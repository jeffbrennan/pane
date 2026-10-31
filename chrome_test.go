package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var styles = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plain removes styles so tests can check rendered text.
func plain(s string) string { return styles.ReplaceAllString(s, "") }

func TestBarAlignsAndDropsRightSide(t *testing.T) {
	if got := bar(20, "left", "right"); got != "left           right" {
		t.Fatalf("unexpected bar: %q", got)
	}
	if got := bar(10, "left", "right"); got != "left" {
		t.Fatalf("right side should be dropped when crowded: %q", got)
	}
	if got := bar(6, styleBold+"truncated"+styleReset, ""); ansi.StringWidth(got) != 6 || !strings.HasSuffix(got, styleReset) {
		t.Fatalf("styled left side should be truncated and reset: %q", got)
	}
}

func TestEveryScreenSharesTitleAndStatusBars(t *testing.T) {
	diff := model{width: 60, height: 10, snapshot: snapshot{unstaged: []change{{path: "a.go", diff: "@@ -1 +1 @@\n-a\n+b\n"}}}}
	diff.rebuild()
	history := model{width: 60, height: 10, screen: "history"}
	commit := model{width: 60, height: 10, screen: "commit", history: historyState{current: commitInfo{hash: "123456789", subject: "subject"}}}
	for name, tt := range map[string]struct {
		m            model
		title, hints string
	}{
		"diff":    {diff, " unstaged 1 · staged 0", "? help"},
		"history": {history, " ", "↵ open  / search  ? help"},
		"commit":  {commit, " 12345678  subject", "esc back  ? help"},
	} {
		lines := strings.Split(plain(tt.m.View()), "\n")
		if len(lines) != 10 || !strings.HasPrefix(lines[0], tt.title) || !strings.HasSuffix(lines[len(lines)-1], tt.hints) {
			t.Errorf("%s: unexpected chrome %q / %q", name, lines[0], lines[len(lines)-1])
		}
	}
}

func TestBusyActionShowsInTitleBar(t *testing.T) {
	for _, screen := range []string{"", "history"} {
		m := model{width: 60, height: 10, screen: screen, busy: true, activeAction: "pull"}
		if title := plain(strings.Split(m.View(), "\n")[0]); !strings.HasSuffix(title, "Pulling…") {
			t.Errorf("%q: missing action in title %q", screen, title)
		}
	}
}

func TestEmptyDiffPointsToOtherView(t *testing.T) {
	m := model{width: 60, height: 10, snapshot: snapshot{staged: []change{{path: "a.go", diff: "@@ -1 +1 @@\n-a\n+b\n"}}}}
	m.rebuild()
	if !strings.Contains(m.View(), "No unstaged changes · tab shows 1 staged") {
		t.Fatalf("missing empty state hint: %q", m.View())
	}
	m.startup, m.loading = true, true
	if strings.Contains(m.View(), "No ") {
		t.Fatal("empty state should wait for the first load")
	}
}

func TestDiscardAndErrorUseDialogs(t *testing.T) {
	m := model{width: 60, height: 12, modal: "discard", discard: discardRequest{change: change{path: "a.go"}}}
	view := plain(m.View())
	if !strings.Contains(view, "┌") || !strings.Contains(view, "Discard hunk?") || !strings.Contains(view, "y discard  n/esc cancel") || !strings.HasPrefix(view, " unstaged 0") {
		t.Fatalf("discard is not a dialog: %q", view)
	}
	m = model{width: 60, height: 12, modal: "error", errorText: strings.Repeat("line\n", 30)}
	view = plain(m.View())
	if !strings.Contains(view, "Git error") || !strings.Contains(view, "↓") {
		t.Fatalf("error is not a scrollable dialog: %q", view)
	}
	for i := 0; i < 100; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = updated.(model)
	}
	if m.errorPos != m.errorMaxPos() || strings.Contains(plain(m.View()), "↓") {
		t.Fatalf("error scrolled past its end: %d", m.errorPos)
	}
}

func TestCommitDialogShowsScope(t *testing.T) {
	m := model{width: 60, height: 20, modal: "commit", snapshot: snapshot{unstaged: []change{{path: "a"}, {path: "b"}}}}
	if !strings.Contains(plain(m.View()), "all 2 files") {
		t.Fatalf("missing stage-all scope: %q", m.View())
	}
	m.snapshot.staged = []change{{path: "a"}}
	if !strings.Contains(plain(m.View()), "1 file staged") {
		t.Fatalf("missing staged scope: %q", m.View())
	}
}

func TestFilePickerKeepsSizeWhileFiltering(t *testing.T) {
	m := keys(t, toolModel(), "f")
	m.height = 30
	before := len(strings.Split(strings.TrimRight(plain(m.View()), "\n "), "\n"))
	_, visible := m.pickerViewport(len(m.pickerItems()))
	m = keys(t, m, "/", "t", "y", "p")
	_, filtered := m.pickerViewport(len(m.pickerItems()))
	if visible != filtered || visible >= m.height-8 {
		t.Fatalf("picker should fit its files and keep its size: %d then %d", visible, filtered)
	}
	if after := len(strings.Split(strings.TrimRight(plain(m.View()), "\n "), "\n")); after != before {
		t.Fatalf("picker moved while filtering: %d then %d", before, after)
	}
}

func TestHeaderNumberAlignsWithLineNumbers(t *testing.T) {
	changes := []change{{path: "a.go", diff: "@@ -1000,2 +1000,2 @@\n x\n y\n"}}
	m := model{width: 40, height: 10, wrap: true, screen: "commit", history: historyState{changes: changes}}
	m.rebuildCommit()
	m.history.pos = 2
	lines := strings.Split(plain(m.View()), "\n")
	if !strings.HasPrefix(lines[1], "     2  a.go") || !strings.HasPrefix(lines[2], "     1   x") {
		t.Fatalf("header number misaligned: %q", lines[1:3])
	}
}

func TestNumberColumnSharedAcrossFiles(t *testing.T) {
	changes := []change{
		{path: "small.go", diff: "@@ -1,1 +1,1 @@\n x\n"},
		{path: "large.go", diff: "@@ -1000,1 +1000,1 @@\n y\n"},
	}
	m := model{width: 40, height: 10, wrap: true, screen: "commit", history: historyState{changes: changes}}
	m.rebuildCommit()
	lines := strings.Split(plain(m.View()), "\n")
	if !strings.HasPrefix(lines[2], "     1   x") || !strings.HasPrefix(lines[4], "     3  large.go") || !strings.HasPrefix(lines[5], "     4   y") {
		t.Fatalf("number column moved between files: %q", lines[1:6])
	}
}

func TestSyntaxLinesAreCached(t *testing.T) {
	diff := "@@ -1,1 +1,1 @@\n-func a() {}\n+func b() {}\n"
	old, _ := syntaxLines("cached.go", diff)
	again, _ := syntaxLines("cached.go", diff)
	if len(old) == 0 || fmt.Sprintf("%p", old) != fmt.Sprintf("%p", again) {
		t.Fatal("tokenized diff was not reused")
	}
}

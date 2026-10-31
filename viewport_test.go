package main

import (
	"strings"
	"testing"
)

func longFile(path string) change {
	return change{path: path, diff: "@@ -0,0 +1,20 @@\n" + strings.Repeat("+content\n", 20)}
}

func TestFileHeaderPinsWithoutMovingActiveLine(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{longFile("first.go")}}}
	m.rebuild()
	m.pos, m.viewTop = 10, 8
	start, visible, pinned := m.diffViewport(6)
	if start != 8 || visible != 5 || pinned == nil || pinned.path != "first.go" {
		t.Fatalf("viewport: %d %d %#v", start, visible, pinned)
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 8 || !strings.Contains(lines[1], "first.go") || strings.Contains(lines[1], "▸") {
		t.Fatalf("invalid pinned header: %q", m.View())
	}
	if !strings.Contains(m.View(), "▸") || m.pos != 10 {
		t.Fatal("active line was lost")
	}
	m.scrollViewport(1)
	start, _, _ = m.diffViewport(6)
	if start != 9 {
		t.Fatalf("scroll did not move viewport: %d", start)
	}
}

func TestPinnedHeaderChangesBetweenFiles(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{longFile("first.go"), longFile("second.go")}}}
	m.rebuild()
	second := 0
	for i, row := range m.rows {
		if row.kind == 'f' && row.path == "second.go" {
			second = i
			break
		}
	}
	m.pos, m.viewTop = second, second
	_, _, pinned := m.diffViewport(6)
	if pinned != nil || strings.Count(m.View(), "second.go") != 1 {
		t.Fatalf("duplicate header: %q", m.View())
	}
	m.pos, m.viewTop = second+2, second+2
	_, _, pinned = m.diffViewport(6)
	if pinned == nil || pinned.path != "second.go" {
		t.Fatalf("wrong pinned header: %#v", pinned)
	}
	if !strings.Contains(strings.Split(m.View(), "\n")[1], "second.go") {
		t.Fatal("second header not pinned")
	}
}

func TestPinnedHeaderRemainsUntilNextHeaderArrives(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{longFile("first.go"), longFile("second.go")}}}
	m.rebuild()
	second := 0
	for i, row := range m.rows {
		if row.kind == 'f' && row.path == "second.go" {
			second = i
			break
		}
	}
	m.pos, m.viewTop = second+1, second-1
	start, _, pinned := m.diffViewport(6)
	if start != second-1 || pinned == nil || pinned.path != "first.go" {
		t.Fatalf("old header disappeared at spacer: start=%d pinned=%#v", start, pinned)
	}
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[1], "first.go") || !strings.Contains(lines[3], "second.go") {
		t.Fatalf("expected old header above incoming header: %q", m.View())
	}
	m.viewTop = second
	start, _, pinned = m.diffViewport(6)
	if start != second || pinned != nil || !strings.Contains(strings.Split(m.View(), "\n")[1], "second.go") {
		t.Fatalf("next header did not replace old header: %q", m.View())
	}
}

func TestPinnedHeaderInCommitViewAndSmallPane(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, screen: "commit", history: historyState{changes: []change{longFile("commit.go")}}}
	m.rebuildCommit()
	m.history.pos, m.history.viewTop = 10, 8
	if !strings.Contains(strings.Split(m.View(), "\n")[1], "commit.go") {
		t.Fatal("commit header not pinned")
	}
	m.height = 3
	view := m.commitModel()
	_, visible, pinned := view.diffViewport(1)
	if visible != 1 || pinned != nil {
		t.Fatal("small pane should reserve its only row for the diff")
	}
}

func TestVimPositioningAccountsForPinnedHeader(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{longFile("file.go")}}}
	m.rebuild()
	m.pos = 15
	for _, test := range []struct {
		command string
		want    int
	}{{"t", 15}, {"z", 13}, {"b", 11}} {
		m.positionViewport(test.command)
		start, visible, pinned := m.diffViewport(6)
		if start != test.want || visible != 5 || pinned == nil {
			t.Fatalf("z%s: start %d visible %d pinned %#v", test.command, start, visible, pinned)
		}
	}
}

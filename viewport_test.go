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
	m.pos, m.viewTop = second+3, second+2
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
	if !strings.Contains(lines[1], "first.go") || !strings.Contains(lines[2], "second.go") {
		t.Fatalf("expected old header above incoming header: %q", m.View())
	}
	m.viewTop = second
	start, _, pinned = m.diffViewport(6)
	if start != second || pinned != nil || !strings.Contains(strings.Split(m.View(), "\n")[1], "second.go") {
		t.Fatalf("next header did not replace old header: %q", m.View())
	}
}

func TestFileBoundaryScrollsOneScreenRowAtATime(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{longFile("first.go"), longFile("second.go")}}}
	m.rebuild()
	second := 0
	for i, r := range m.rows {
		if r.kind == 'f' && r.path == "second.go" {
			second = i
			break
		}
	}
	for _, direction := range []int{1, -1} {
		start := second - 3
		if direction < 0 {
			start = second + 1
		}
		previous := -1
		for top := start; top >= second-3 && top <= second+1; top += direction {
			m.viewTop, m.pos = top, top+3
			lines := strings.Split(m.View(), "\n")
			position := -1
			for i, line := range lines {
				if strings.Contains(line, "second.go") {
					position = i
					break
				}
			}
			if previous >= 0 && position >= 0 && (position-previous)*direction < -1 {
				t.Fatalf("direction %d top %d: header jumped from %d to %d", direction, top, previous, position)
			}
			previous = position
		}
	}

	m.viewTop, m.pos = second-1, second-1
	if !strings.Contains(strings.Split(m.View(), "\n")[1], "▸") {
		t.Fatal("selected separator is hidden by the pinned header")
	}
}

func TestRepeatedMovementAcrossFileBoundary(t *testing.T) {
	m := model{width: 40, height: 8, wrap: true, snapshot: snapshot{unstaged: []change{longFile("first.go"), longFile("second.go")}}}
	m.rebuild()
	for _, direction := range []int{1, -1} {
		previous := -1
		for range m.rows {
			lines := strings.Split(m.View(), "\n")
			position := -1
			for i, line := range lines {
				if strings.Contains(line, "second.go") {
					position = i
					break
				}
			}
			if previous >= 0 && position >= 0 && (position-previous)*direction < -1 {
				t.Fatalf("direction %d pos %d: header jumped from %d to %d", direction, m.pos, previous, position)
			}
			if !strings.Contains(m.View(), "▸") {
				t.Fatalf("direction %d pos %d: active row is hidden", direction, m.pos)
			}
			previous = position
			m.move(direction)
		}
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

func TestPickerJumpDoesNotScrollBackwardOnNextLine(t *testing.T) {
	for _, screen := range []string{"", "commit"} {
		t.Run(screen, func(t *testing.T) {
			changes := []change{longFile("first.go"), longFile("second.go"), longFile("third.go")}
			m := model{width: 60, height: 10, wrap: true, screen: screen}
			if screen == "commit" {
				m.history.changes = changes
				m.rebuildCommit()
			} else {
				m.snapshot.unstaged = changes
				m.rebuild()
			}
			m = keys(t, m, "f", "/", "s", "e", "c", "o", "n", "d", "enter")
			before := strings.Split(m.View(), "\n")
			if !strings.Contains(before[1], "second.go") || !strings.Contains(before[1], "▸") {
				t.Fatalf("picker did not select the file header: %q", m.View())
			}
			m = keys(t, m, "j")
			after := strings.Split(m.View(), "\n")
			if !strings.Contains(after[1], "second.go") || strings.Contains(after[1], "▸") || !strings.Contains(after[2], "▸") || strings.Contains(m.View(), "first.go") {
				t.Fatalf("moving down shifted the viewport: %q", m.View())
			}
		})
	}
}

func TestMovementKeepsExplicitViewportAlignment(t *testing.T) {
	m := model{width: 40, height: 10, wrap: true, snapshot: snapshot{unstaged: []change{longFile("file.go"), longFile("next.go")}}}
	m.rebuild()
	m.pos = 10
	m.positionViewport("b")
	top, _, _ := m.diffViewport(m.height - 2)
	m.move(-1)
	if after, _, _ := m.diffViewport(m.height - 2); after != top {
		t.Fatalf("moving up scrolled down: before %d after %d", top, after)
	}
	m.positionViewport("t")
	top, _, _ = m.diffViewport(m.height - 2)
	m.move(1)
	if after, _, _ := m.diffViewport(m.height - 2); after != top {
		t.Fatalf("moving down scrolled up: before %d after %d", top, after)
	}
	for range 10 {
		m.move(1)
	}
	if after, _, _ := m.diffViewport(m.height - 2); after <= top {
		t.Fatalf("viewport did not scroll when approaching the bottom: before %d after %d", top, after)
	}
}

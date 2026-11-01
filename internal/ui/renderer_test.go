package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestCommitReviewHybridLineNumbers(t *testing.T) {
	changes := []git.Change{{Path: "file.txt", Diff: "@@ -100,3 +100,3 @@\n first\n second\n third\n"}}
	m := model{width: 40, height: 10, wrap: true, screen: "commit", history: historyState{changes: changes}}
	m.rebuildCommit()
	m.history.pos = 2
	view := m.commitModel()
	strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, tc := range []struct {
		index int
		want  string
	}{{0, "2"}, {1, "1"}, {2, "101"}, {3, "1"}} {
		line := strip.ReplaceAllString(view.renderNumberedDiffRow(view.rows[tc.index], tc.index), "")
		fields := strings.Fields(line)
		if tc.index == view.pos {
			fields = fields[1:]
		}
		if len(fields) == 0 || fields[0] != tc.want {
			t.Fatalf("row %d: got %q, want number %s", tc.index, line, tc.want)
		}
	}
	if got := view.diffContentWidth(); got != m.width-diffMargin(m.width) {
		t.Fatalf("hybrid gutter should not consume extra columns, got width %d", got)
	}
	if got := strip.ReplaceAllString(view.renderNumberedDiffRow(view.rows[0], -1), ""); strings.Contains(got, "  2 ") {
		t.Fatalf("pinned header has a relative number: %q", got)
	}
	if !m.commitModel().relativeLines || (model{screen: "sequence", height: 10}).relativeGutterWidth() == 0 {
		t.Fatal("commit review should enable relative numbers")
	}
}

type renderTestModel struct{ model }

func (m renderTestModel) Init() tea.Cmd { return nil }

func (m renderTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.model.Update(msg)
	return renderTestModel{updated.(model)}, cmd
}

type renderCapture struct{ writes chan string }

func (c renderCapture) Write(p []byte) (int, error) {
	select {
	case c.writes <- string(p):
	default:
	}
	return len(p), nil
}

func nextDiffFrame(t *testing.T, writes <-chan string) string {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case output := <-writes:
			if strings.Contains(output, "content") {
				return output
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for a rendered diff frame")
		}
	}
}

func TestDiffRedrawDoesNotClearLinesBeforePainting(t *testing.T) {
	m := model{width: 60, height: 12, wrap: true, snapshot: git.Snapshot{Unstaged: []git.Change{longFile("first.go"), longFile("second.go")}}}
	m.rebuild()
	writes := make(chan string, 256)
	p := tea.NewProgram(renderTestModel{m}, tea.WithInput(nil), tea.WithOutput(renderCapture{writes}), tea.WithAltScreen(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	t.Cleanup(func() {
		p.Quit()
		if err := <-done; err != nil {
			t.Errorf("program failed: %v", err)
		}
	})
	p.Send(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	nextDiffFrame(t, writes)
	for _, key := range []string{strings.Repeat("j", 30), strings.Repeat("k", 30)} {
		for _, r := range key {
			p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			frame := nextDiffFrame(t, writes)
			if strings.Contains(frame, "\x1b[2K") {
				t.Fatalf("%c redraw clears entire lines before painting: %q", r, frame)
			}
		}
	}
}

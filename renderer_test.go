package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
	m := model{width: 60, height: 12, wrap: true, snapshot: snapshot{unstaged: []change{longFile("first.go"), longFile("second.go")}}}
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

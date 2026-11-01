package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

type flashed struct{ text string }

// editorRunning pauses terminal background queries, whose replies would
// otherwise be typed into the editor.
var editorRunning atomic.Bool

// editorCommand runs the editor on the terminal itself. Pane's input reader
// wraps the terminal, so Bubble Tea would otherwise hand the editor a pipe.
type editorCommand struct{ *exec.Cmd }

func (c editorCommand) Run() error {
	editorRunning.Store(true)
	defer editorRunning.Store(false)
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		c.Stdin = tty
	}
	return c.Cmd.Run()
}

func (c editorCommand) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c editorCommand) SetStdout(w io.Writer) {
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c editorCommand) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}

type editorClosed struct{ err error }

// editorArgs builds the command opening path at line using the syntax the
// editor understands; line 0 opens the file without a position.
func editorArgs(editor, path string, line int) []string {
	args := strings.Fields(editor)
	if len(args) == 0 {
		args = []string{"vi"}
	}
	if line <= 0 {
		return append(args, path)
	}
	switch filepath.Base(args[0]) {
	case "code", "code-insiders", "cursor", "codium", "windsurf":
		return append(args, "-g", fmt.Sprintf("%s:%d", path, line))
	case "subl", "zed", "hx", "helix", "micro":
		return append(args, fmt.Sprintf("%s:%d", path, line))
	default:
		return append(args, fmt.Sprintf("+%d", line), path)
	}
}

func (m model) openEditor() (model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}
	r := m.rows[m.pos]
	path := filepath.Join(m.repo.Root, r.path)
	if _, err := os.Stat(path); err != nil {
		m.flash = syntax.Clean(r.path) + " does not exist in the working tree"
		return m, nil
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	args := editorArgs(editor, path, r.target)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = m.repo.Root
	return m, tea.Exec(editorCommand{cmd}, func(err error) tea.Msg { return editorClosed{err} })
}

func copyCmd(text, label string) tea.Cmd {
	return func() tea.Msg {
		if err := copyToClipboard(text); err != nil {
			return flashed{err.Error()}
		}
		return flashed{"Copied " + label}
	}
}

func copyToClipboard(text string) error {
	for _, args := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return git.CommandError(out, err)
		}
		return nil
	}
	return fmt.Errorf("no clipboard command found (pbcopy, wl-copy, xclip or xsel)")
}

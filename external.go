package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type flashed struct{ text string }
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
	path := filepath.Join(m.repo.root, r.path)
	if _, err := os.Stat(path); err != nil {
		m.flash = clean(r.path) + " does not exist in the working tree"
		return m, nil
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	args := editorArgs(editor, path, r.target)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = m.repo.root
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorClosed{err} })
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
			return gitError(out, err)
		}
		return nil
	}
	return fmt.Errorf("no clipboard command found (pbcopy, wl-copy, xclip or xsel)")
}

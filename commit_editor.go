package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type commitEditorClosed struct {
	path string
	err  error
}

func (m model) openCommitEditor() (tea.Model, tea.Cmd) {
	path, err := m.prepareCommitEditor()
	if err != nil {
		m.message = "Editor: " + err.Error()
		return m, nil
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	args := editorArgs(editor, path, 0)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = m.repo.Root
	return m, tea.Exec(editorCommand{cmd}, func(err error) tea.Msg {
		return commitEditorClosed{path: path, err: err}
	})
}

func (m model) prepareCommitEditor() (string, error) {
	out, err := m.repo.Command("rev-parse", "--git-path", "PANE_COMMIT_EDITMSG")
	if err != nil {
		return "", git.GitError(out, err)
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.repo.Root, path)
	}
	file, err := os.CreateTemp(filepath.Dir(path), "PANE_COMMIT_EDITMSG-*")
	if err != nil {
		return "", err
	}
	var content strings.Builder
	content.WriteString(strings.TrimRight(m.input+m.inputBody, "\n"))
	content.WriteString("\n\n# Lines starting with # are ignored. An empty message aborts.\n")
	if m.amendHash == "" {
		changes := m.snapshot.Staged
		label := "Changes to be committed"
		if len(changes) == 0 {
			changes = m.snapshot.Unstaged
			label = "All changes to be committed"
		}
		fmt.Fprintf(&content, "# %s:\n", label)
		for _, change := range changes {
			fmt.Fprintf(&content, "#   %s\n", strings.ReplaceAll(change.Path, "\n", "\\n"))
		}
	}
	_, err = file.WriteString(content.String())
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func cleanCommitMessage(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, strings.TrimRight(line, " \t\r"))
		}
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func (m model) finishCommitEditor(msg commitEditorClosed) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errorText = fmt.Sprintf("Editor: %v\nMessage retained at %s", msg.err, msg.path)
		m.errorPos = 0
		m.modal = "error"
		return m, nil
	}
	content, err := os.ReadFile(msg.path)
	if err != nil {
		m.errorText = fmt.Sprintf("Editor: %v\nMessage file: %s", err, msg.path)
		m.errorPos = 0
		m.modal = "error"
		return m, nil
	}
	message := cleanCommitMessage(string(content))
	if strings.TrimSpace(message) == "" {
		os.Remove(msg.path)
		m.modal = ""
		m.flash = "Commit message empty, aborted"
		return m, nil
	}
	m.commitEditorPath = msg.path
	return m.submitCommit(message)
}

func (m model) submitCommit(message string) (tea.Model, tea.Cmd) {
	amendHash := m.amendHash
	m.modal = ""
	m.busy = true
	if amendHash != "" {
		m.activeAction = "amend"
	}
	m.generation++
	repo := m.repo
	return m, func() tea.Msg {
		if amendHash != "" {
			output, err := repo.AmendMessage(amendHash, message)
			return completed{message: output, commitMessage: message, err: err}
		}
		output, err := repo.Commit(message)
		return completed{message: output, commitMessage: message, err: err}
	}
}

package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestCommitEditorMessageAndSubmission(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "contents\n")
	m := model{repo: r, modal: "commit", input: "draft", snapshot: git.Snapshot{Unstaged: []git.Change{{Path: "file"}}}}
	path, err := m.prepareCommitEditor()
	if err != nil {
		t.Fatal(err)
	}
	initial, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(initial), "draft\n") || !strings.Contains(string(initial), "#   file") {
		t.Fatalf("editor contents: %q: %v", initial, err)
	}
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'subject  \\n\\nbody\\n# ignored\\n' > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(editor, path).CombinedOutput(); err != nil {
		t.Fatalf("fake editor: %s: %v", out, err)
	}
	updated, cmd := m.finishCommitEditor(commitEditorClosed{path: path})
	if cmd == nil || !updated.(model).busy {
		t.Fatal("editor did not submit the commit")
	}
	result := cmd().(completed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	updated, _ = updated.(model).Update(result)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary message not removed: %v", err)
	}
	message, err := r.Command("log", "-1", "--format=%B")
	if err != nil || strings.TrimSpace(string(message)) != "subject\n\nbody" {
		t.Fatalf("commit message %q: %v", message, err)
	}
}

func TestCommitEditorCancelAndFailure(t *testing.T) {
	r := testRepo(t)
	m := model{repo: r, modal: "commit", input: "draft"}
	for _, tt := range []struct {
		name    string
		content string
		err     error
	}{
		{"empty", "# comment\n  \n", nil},
		{"failed", "new message\n", errors.New("editor failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path, err := m.prepareCommitEditor()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			updated, cmd := m.finishCommitEditor(commitEditorClosed{path: path, err: tt.err})
			got := updated.(model)
			if cmd != nil || got.busy || (tt.err == nil && (got.modal != "" || got.flash != "Commit message empty, aborted")) || (tt.err != nil && (got.modal != "error" || got.input != "draft" || !strings.Contains(got.errorText, "editor failed") || !strings.Contains(got.errorText, path))) {
				t.Fatalf("unexpected state: %#v", got)
			}
			if tt.err != nil {
				if content, err := os.ReadFile(path); err != nil || string(content) != tt.content {
					t.Fatalf("editor output not retained: %q %v", content, err)
				}
			}
		})
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil || updated.(model).modal != "commit" {
		t.Fatal("ctrl-g did not open editor")
	}
}

func TestCommitEditorRetainsMessageOnCommitFailure(t *testing.T) {
	r := testRepo(t)
	m := model{repo: r, modal: "commit", input: "message"}
	path, err := m.prepareCommitEditor()
	if err != nil {
		t.Fatal(err)
	}
	updated, cmd := m.finishCommitEditor(commitEditorClosed{path: path})
	result := cmd().(completed)
	if result.err == nil {
		t.Fatal("expected commit to fail with no changes")
	}
	updated, _ = updated.(model).Update(result)
	if got := updated.(model); got.modal != "error" || !strings.Contains(got.errorText, path) {
		t.Fatalf("missing recovery path: %#v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("message not retained: %v", err)
	}
}

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

// pushFixture makes a repository with one commit and a bare remote, leaving
// the current branch without an upstream.
func pushFixture(t *testing.T, remotes ...string) (git.Repo, string) {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "file.txt", "content\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, remote := range remotes {
		bare := filepath.Join(t.TempDir(), remote+".git")
		if out, err := r.CommandStdout("init", "--bare", bare); err != nil {
			t.Fatalf("init bare: %s %v", out, err)
		}
		if _, err := r.Action("remote", "add", remote, bare); err != nil {
			t.Fatal(err)
		}
	}
	branch, err := r.CommandStdout("symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return r, strings.TrimSpace(string(branch))
}

func upstream(t *testing.T, r git.Repo) string {
	t.Helper()
	out, err := r.CommandStdout("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func probePush(t *testing.T, m model) model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("p did not start a push probe")
	}
	msg, ok := cmd().(pushProbed)
	if !ok {
		t.Fatalf("push probe produced %#v", cmd())
	}
	updated, _ = m.Update(msg)
	return updated.(model)
}

func TestPushWithoutUpstreamAsksBeforePushing(t *testing.T) {
	r, branch := pushFixture(t, "origin")
	m := probePush(t, model{width: 100, height: 30, repo: r})
	if m.modal != "push-upstream" || m.push.branch != branch || m.push.remote != "origin" || m.busy {
		t.Fatalf("push dialog: modal=%q %#v busy=%v", m.modal, m.push, m.busy)
	}
	if view := plain(m.View()); !strings.Contains(view, "Push "+branch+" and track it?") || !strings.Contains(view, "--set-upstream origin") {
		t.Fatalf("push dialog does not explain the command: %q", view)
	}
	if got := upstream(t, r); got != "" {
		t.Fatalf("probe pushed without asking: %s", got)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.modal != "" || cmd == nil {
		t.Fatal("enter did not start the push")
	}
	msg := cmd()
	if completed, ok := msg.(completed); !ok || completed.err != nil {
		t.Fatalf("push failed: %#v", msg)
	}
	if got, want := upstream(t, r), "origin/"+branch; got != want {
		t.Fatalf("upstream is %q, want %q", got, want)
	}
}

func TestPushWithoutUpstreamCanBeCancelled(t *testing.T) {
	r, _ := pushFixture(t, "origin")
	m := probePush(t, model{width: 100, height: 30, repo: r})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.modal != "" || cmd != nil || upstream(t, r) != "" {
		t.Fatalf("cancel changed something: modal=%q cmd=%v upstream=%q", m.modal, cmd, upstream(t, r))
	}
}

func TestPushWithAnUpstreamGoesStraightThrough(t *testing.T) {
	r, branch := pushFixture(t, "origin")
	if _, err := r.Action("push", "--set-upstream", "origin", branch); err != nil {
		t.Fatal(err)
	}
	m := probePush(t, model{width: 100, height: 30, repo: r})
	if m.modal != "" || !m.busy || m.activeAction != "push" {
		t.Fatalf("existing upstream should push directly: modal=%q busy=%v action=%q", m.modal, m.busy, m.activeAction)
	}
}

func TestPushWithSeveralRemotesAsksWhich(t *testing.T) {
	r, branch := pushFixture(t, "team", "fork")
	m := probePush(t, model{width: 100, height: 30, repo: r})
	if m.modal != "push-upstream" || m.push.remote != "" {
		t.Fatalf("ambiguous remotes: modal=%q %#v", m.modal, m.push)
	}
	if view := plain(m.View()); !strings.Contains(view, "to which remote?") || !strings.Contains(view, "1 fork") || !strings.Contains(view, "2 team") {
		t.Fatalf("dialog does not list remotes: %q", view)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = updated.(model)
	if m.modal != "" || cmd == nil {
		t.Fatal("choosing a remote did not push")
	}
	if msg := cmd(); msg.(completed).err != nil {
		t.Fatalf("push failed: %#v", msg)
	}
	if got, want := upstream(t, r), "team/"+branch; got != want {
		t.Fatalf("upstream is %q, want %q", got, want)
	}
}

func TestPushAlwaysSetsAutoSetupRemote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	r, branch := pushFixture(t, "origin")
	m := probePush(t, model{width: 100, height: 30, repo: r})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(model)
	if m.modal != "" || cmd == nil {
		t.Fatal("a did not push")
	}
	if msg := cmd(); msg.(completed).err != nil {
		t.Fatalf("push failed: %#v", msg)
	}
	config := filepath.Join(home, ".gitconfig")
	out, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(out), "autoSetupRemote = true") {
		t.Fatalf("global config not written to %s: %s %v", config, out, err)
	}
	if got, want := upstream(t, r), "origin/"+branch; got != want {
		t.Fatalf("upstream is %q, want %q", got, want)
	}
}

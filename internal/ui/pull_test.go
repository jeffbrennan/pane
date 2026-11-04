package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPullWithDeletedUpstreamOffersDefaultBranch mimics a merged pull request:
// the feature branch is deleted on the remote while main moves ahead.
func TestPullWithDeletedUpstreamOffersDefaultBranch(t *testing.T) {
	r, main := pushFixture(t, "origin")
	if _, err := r.Action("push", "--set-upstream", "origin", main); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("switch", "-c", "feat/x"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "feature.txt", "feature\n")
	if _, err := r.Commit("feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("push", "--set-upstream", "origin", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("push", "origin", "feat/x:"+main, ":feat/x"); err != nil {
		t.Fatal(err)
	}

	m := model{width: 100, height: 30, repo: r}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	m = updated.(model)
	msg, ok := cmd().(pullFailed)
	if !ok {
		t.Fatal("pull with a deleted upstream did not offer a switch")
	}
	updated, _ = m.Update(msg)
	m = updated.(model)
	if m.modal != "pull-issue" || m.pull.Target != main || m.pull.Upstream != "origin/feat/x" || m.busy {
		t.Fatalf("pull dialog: modal=%q %#v busy=%v", m.modal, m.pull, m.busy)
	}
	if view := plain(m.View()); !strings.Contains(view, "Switch to "+main+"?") || !strings.Contains(view, "feat/x was merged into "+main) {
		t.Fatalf("pull dialog does not explain the switch: %q", view)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.modal != "" || cmd == nil {
		t.Fatal("enter did not switch")
	}
	if done, ok := cmd().(completed); !ok || done.err != nil {
		t.Fatalf("switch and pull failed: %#v", done)
	}
	head, _ := r.CommandStdout("symbolic-ref", "--short", "HEAD")
	if got := strings.TrimSpace(string(head)); got != main {
		t.Fatalf("on %q, want %q", got, main)
	}
	if out, err := r.CommandStdout("rev-parse", "HEAD", "origin/"+main); err != nil || len(strings.Fields(string(out))) != 2 || strings.Fields(string(out))[0] != strings.Fields(string(out))[1] {
		t.Fatalf("%s was not pulled: %s %v", main, out, err)
	}
}

func TestPullFailureWithLiveUpstreamShowsGitError(t *testing.T) {
	r, branch := pushFixture(t, "origin")
	if _, err := r.Action("push", "--set-upstream", "origin", branch); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("remote", "set-url", "origin", "/nonexistent"); err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 30, repo: r}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if done, ok := cmd().(completed); !ok || done.err == nil {
		t.Fatalf("unreachable remote should fail as a plain error: %#v", done)
	}
}

func TestPullWithUnmergedDeletedUpstreamDoesNotClaimMerge(t *testing.T) {
	r, main := pushFixture(t, "origin")
	if _, err := r.Action("push", "--set-upstream", "origin", main); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("switch", "-c", "feat/x"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "feature.txt", "feature\n")
	if _, err := r.Commit("feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("push", "--set-upstream", "origin", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("push", "origin", ":feat/x"); err != nil {
		t.Fatal(err)
	}
	m := model{width: 100, height: 30, repo: r}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	msg, ok := cmd().(pullFailed)
	if !ok {
		t.Fatal("pull with a deleted upstream did not offer a switch")
	}
	updated, _ := m.Update(msg)
	m = updated.(model)
	view := plain(m.View())
	if m.pull.Merged || strings.Contains(view, "merged") || !strings.Contains(view, "origin/feat/x was deleted on the remote") {
		t.Fatalf("unmerged branch described as merged: %#v %q", m.pull, view)
	}
}

package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBranchStatusTracksAheadAndBehind(t *testing.T) {
	r := testRepo(t)
	s, err := r.load()
	if err != nil {
		t.Fatal(err)
	}
	if !s.branch.available || s.branch.name == "" || s.branch.upstream != "" || s.branch.countsKnown {
		t.Fatalf("unborn branch: %#v", s.branch)
	}
	writeFile(t, r, "file", "base\n")
	if _, err := r.commit("base"); err != nil {
		t.Fatal(err)
	}
	branchBytes, err := r.command("symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(string(branchBytes))
	baseBytes, err := r.command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(baseBytes))
	if _, err := r.action("remote", "add", "origin", r.root); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("update-ref", "refs/remotes/origin/"+branch, base); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("branch", "--set-upstream-to=origin/"+branch, branch); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "local\n")
	if _, err := r.commit("local"); err != nil {
		t.Fatal(err)
	}
	treeBytes, err := r.command("rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	remoteBytes, err := r.command("commit-tree", strings.TrimSpace(string(treeBytes)), "-p", base, "-m", "remote")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("update-ref", "refs/remotes/origin/"+branch, strings.TrimSpace(string(remoteBytes))); err != nil {
		t.Fatal(err)
	}
	s, err = r.load()
	if err != nil {
		t.Fatal(err)
	}
	if s.branch.name != branch || s.branch.upstream != "origin/"+branch || !s.branch.countsKnown || s.branch.ahead != 1 || s.branch.behind != 1 {
		t.Fatalf("divergent branch: %#v", s.branch)
	}
}

func TestBranchStatusWithoutAvailableUpstreamCounts(t *testing.T) {
	status := parseBranchStatus([]byte("# branch.head main\x00# branch.upstream origin/main\x00"))
	if !status.available || status.upstream != "origin/main" || status.countsKnown {
		t.Fatalf("status: %#v", status)
	}
	m := model{width: 60, height: 20, screen: "history", snapshot: snapshot{branch: status}}
	if !strings.Contains(plain(m.View()), "upstream unknown · clean") {
		t.Fatalf("view: %q", m.View())
	}
}

func TestHistoryLandingStatusAndActions(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", snapshot: snapshot{
		branch:   branchStatus{available: true, name: "main", upstream: "origin/main", countsKnown: true, ahead: 2, behind: 3},
		unstaged: []change{{path: "one"}}, staged: []change{{path: "two"}},
	}, history: historyState{commits: []commitInfo{{subject: "recent commit"}}}}
	view := plain(m.View())
	for _, text := range []string{"main", "↑2 ahead · ↓3 behind · 1 unstaged · 1 staged", "origin/main", "recent commit"} {
		if !strings.Contains(view, text) {
			t.Errorf("missing %q in %q", text, view)
		}
	}
	firstLine := strings.Split(view, "\n")[0]
	if !strings.Contains(firstLine, "main") || !strings.Contains(firstLine, "origin/main") || strings.Contains(firstLine, "HISTORY") {
		t.Fatalf("branch and upstream should share the heading: %q", firstLine)
	}
	if strings.Contains(strings.ToLower(strings.Join(m.historyHeader(), "\n")), "history") {
		t.Fatal("unnecessary history heading is visible")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd == nil || !updated.(model).busy || updated.(model).modal != "" {
		t.Fatal("push did not start in history")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if cmd == nil || !updated.(model).busy || updated.(model).modal != "" {
		t.Fatal("pull did not start in history")
	}
	m.width = 24
	if !strings.Contains(m.View(), "↑2 ↓3") {
		t.Fatalf("narrow view: %q", m.View())
	}
}

func TestPullRefreshesHistory(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", busy: true, history: historyState{selected: 1, filter: "fix"}}
	updated, cmd := m.Update(completed{action: "pull", message: "Updated"})
	got := updated.(model)
	if cmd == nil || got.modal != "" || !got.history.loading || got.history.filter != "fix" || got.history.selected != 1 {
		t.Fatalf("history not refreshed after pull: %#v", got)
	}
}

func TestHistoryReloadsWhenHeadChanges(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", snapshot: snapshot{branch: branchStatus{available: true, head: "old"}}}
	updated, cmd := m.Update(loaded{snapshot: snapshot{branch: branchStatus{available: true, head: "new"}}})
	got := updated.(model)
	if cmd == nil || !got.history.loading || got.snapshot.branch.head != "new" {
		t.Fatal("history did not reload")
	}
}

func TestHistoryReloadsAfterHeadChangesDuringLoad(t *testing.T) {
	m := model{screen: "history", snapshot: snapshot{branch: branchStatus{available: true, head: "old"}}, history: historyState{loading: true, request: 1}}
	updated, cmd := m.Update(loaded{snapshot: snapshot{branch: branchStatus{available: true, head: "new"}}})
	m = updated.(model)
	if cmd != nil || !m.history.stale {
		t.Fatal("head change during load was not recorded")
	}
	updated, cmd = m.Update(historyLoaded{request: 1})
	got := updated.(model)
	if cmd == nil || !got.history.loading || got.history.stale || got.history.request != 2 {
		t.Fatal("stale history was not reloaded")
	}
}

func TestTrackingCountsRefreshWithoutReloadingHistory(t *testing.T) {
	m := model{screen: "history", snapshot: snapshot{branch: branchStatus{available: true, head: "same", countsKnown: true}}}
	next := snapshot{branch: branchStatus{available: true, head: "same", countsKnown: true, ahead: 2, behind: 3}}
	updated, cmd := m.Update(loaded{snapshot: next})
	if cmd != nil || updated.(model).snapshot.branch.ahead != 2 || updated.(model).history.loading {
		t.Fatal("tracking update was not applied")
	}
}

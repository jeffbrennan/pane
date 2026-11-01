package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func baseFixture(t *testing.T) (repository, string) {
	t.Helper()
	r := testRepo(t)
	writeFile(t, r, "file", "base\n")
	if _, err := r.commit("base"); err != nil {
		t.Fatal(err)
	}
	base, err := r.action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("branch", "review-base"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "committed", "commit change\n")
	if _, err := r.commit("feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "second", "second commit\n")
	if _, err := r.commit("second feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "staged", "staged change\n")
	if err := r.stage("staged", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "working change\n")
	writeFile(t, r, "untracked", "untracked change\n")
	return r, base
}

// drainBase runs the commands a base refresh queues, including the stream the
// UI follows asynchronously.
func drainBase(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	for steps := 0; cmd != nil && steps < 10000; steps++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				if sub != nil {
					m = drainBase(t, m, sub)
				}
			}
			return m
		}
		updated, next := m.Update(msg)
		m, cmd = updated.(model), next
	}
	return m
}

func TestBaseReviewStartupAndContents(t *testing.T) {
	r, base := baseFixture(t)
	for _, args := range [][]string{{"--base", "review-base"}, {"-b", "review-base"}, {"review-base..."}} {
		m, err := initialModel(r, args)
		if err != nil || m.screen != "base" || m.base.hash != base {
			t.Fatalf("startup %q: %#v %v", args, m.base, err)
		}
		m.width, m.height = 100, 20
		updated, cmd := m.Update(load(r, m.generation, nil)())
		if cmd == nil {
			t.Fatal("base review did not load")
		}
		m = drainBase(t, updated.(model), cmd)
		view := m.View()
		for _, want := range []string{"review-base…working tree", shortHash(base), "commit change", "second commit", "staged change", "working change", "untracked change"} {
			if !strings.Contains(view, want) {
				t.Errorf("%q missing %q: %q", args, want, view)
			}
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if updated.(model).screen != "history" || cmd == nil {
			t.Fatal("base review did not return to history")
		}
	}
	for _, args := range [][]string{{"--base"}, {"--base", "missing"}, {"-b", "--bad"}, {"..."}, {"a", "b", "c"}} {
		if _, err := initialModel(r, args); err == nil {
			t.Errorf("accepted invalid arguments %q", args)
		}
	}
}

func TestBaseReviewTracksMovingBase(t *testing.T) {
	r, base := baseFixture(t)
	m, err := initialModel(r, []string{"review-base..."})
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 100, 25
	updated, cmd := m.Update(load(r, m.generation, nil)())
	m = drainBase(t, updated.(model), cmd)
	if m.base.hash != base {
		t.Fatal("incorrect initial merge base")
	}
	if _, err := r.action("branch", "-f", "review-base", "HEAD"); err != nil {
		t.Fatal(err)
	}
	head, err := r.action("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	updated, cmd = m.Update(load(r, m.generation, m.refreshCache)())
	if cmd == nil {
		t.Fatal("unchanged status did not check base reference")
	}
	m = drainBase(t, updated.(model), cmd)
	if m.base.hash != head || strings.Contains(m.View(), "commit change") || !strings.Contains(m.View(), "working change") {
		t.Fatalf("base move not reflected: %q", m.View())
	}
}

func TestBaseReviewContextAndRefresh(t *testing.T) {
	r, base := baseFixture(t)
	m, err := initialModel(r, []string{"--base", "review-base"})
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 20
	updated, cmd := m.Update(load(r, m.generation, nil)())
	m = drainBase(t, updated.(model), cmd)
	context, err := r.fullContext(contextKey{path: "file", commit: base, base: true}, "", "")
	if err != nil || !strings.Contains(context, "-base") || !strings.Contains(context, "+working change") {
		t.Fatalf("base context: %q %v", context, err)
	}
	writeFile(t, r, "file", "updated change\n")
	updated, cmd = m.Update(load(r, m.generation, m.refreshCache)())
	if cmd == nil {
		t.Fatal("updated worktree did not request base refresh")
	}
	m = drainBase(t, updated.(model), cmd)
	if !strings.Contains(m.View(), "updated change") {
		t.Fatalf("updated diff missing: %q", m.View())
	}
	for _, key := range []string{"s", " ", "d", "c"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil || updated.(model).modal != "" {
			t.Fatalf("read-only review accepted %q", key)
		}
	}
}

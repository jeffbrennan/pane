package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestStaleUnchangedRefreshDoesNotReplaceCache(t *testing.T) {
	current := &git.RefreshCache{}
	m := model{generation: 2, loading: true, refreshCache: current}
	updated, cmd := m.Update(loadedUnchanged{generation: 1, cache: &git.RefreshCache{}})
	next := updated.(model)
	if next.refreshCache != current || !next.loading || cmd == nil {
		t.Fatal("stale refresh should trigger a new load without replacing the cache")
	}
}

func TestBlurredPaneRefreshesLessOften(t *testing.T) {
	tickUntilLoading := func(m model) int {
		for i := 1; i <= 20; i++ {
			updated, _ := m.Update(tick{})
			m = updated.(model)
			if m.loading {
				return i
			}
		}
		return -1
	}
	if got := tickUntilLoading(model{}); got != refreshTicks {
		t.Fatalf("focused refresh after %d ticks, want %d", got, refreshTicks)
	}
	updated, _ := model{}.Update(tea.BlurMsg{})
	if got := tickUntilLoading(updated.(model)); got != blurredRefreshTicks {
		t.Fatalf("blurred refresh after %d ticks, want %d", got, blurredRefreshTicks)
	}
}

func TestFocusRefreshesImmediately(t *testing.T) {
	updated, cmd := model{blurred: true, ticks: 5}.Update(tea.FocusMsg{})
	m := updated.(model)
	if m.blurred || !m.loading || cmd == nil {
		t.Fatal("regaining focus should refresh immediately")
	}
}

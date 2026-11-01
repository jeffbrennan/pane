package ui

import (
	"context"
	"testing"

	"github.com/jeffbrennan/pane/internal/git"
)

func TestCommitLoadCancellation(t *testing.T) {
	m := model{}
	first := m.startCommitLoad()
	second := m.startCommitLoad()
	if first.Context.Err() != context.Canceled || second.Context.Err() != nil {
		t.Fatal("starting a new load must cancel the previous one")
	}
	_, _ = m.returnToHistory()
	if second.Context.Err() != context.Canceled {
		t.Fatal("leaving the commit must cancel its load")
	}
}

func TestCancelledSyntaxDemandCanBeRetried(t *testing.T) {
	key := git.Context{Commit: "abc", Path: "file.go"}
	m := model{syntaxDemand: &syntaxDemand{loaded: make(map[git.Context]demandSyntax), pending: map[git.Context]bool{key: true}}}
	m.applySyntaxDemandLoaded(syntaxDemandLoaded{results: []demandResult{{key: key, diff: "diff", cancelled: true}}})
	if m.syntaxDemand.pending[key] || len(m.syntaxDemand.loaded) != 0 {
		t.Fatal("cancelled syntax must not be cached")
	}
}

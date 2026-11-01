package main

import (
	"context"
	"testing"
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
	key := contextKey{commit: "abc", path: "file.go"}
	m := model{syntaxDemand: &syntaxDemand{loaded: make(map[contextKey]demandSyntax), pending: map[contextKey]bool{key: true}}}
	m.applySyntaxDemandLoaded(syntaxDemandLoaded{results: []demandResult{{key: key, diff: "diff", cancelled: true}}})
	if m.syntaxDemand.pending[key] || len(m.syntaxDemand.loaded) != 0 {
		t.Fatal("cancelled syntax must not be cached")
	}
}

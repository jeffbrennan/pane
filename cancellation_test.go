package main

import (
	"context"
	"os"
	"testing"
	"time"
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

func TestGitCommandStopsWhenCommitIsCancelled(t *testing.T) {
	r := testRepo(t)
	m := model{repo: r}
	repo := m.startCommitLoad()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd := repo.GitCommand("cat-file", "--batch")
	cmd.Stdin = reader
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	m.cancelCommitLoad()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled Git command exited successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Git command did not stop after cancellation")
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

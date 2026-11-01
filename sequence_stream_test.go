package main

import (
	"context"
	"github.com/jeffbrennan/pane/internal/git"
	"testing"
)

func sequenceStreamModel(commits []git.CommitInfo) model {
	return model{
		width: 100, height: 20, wrap: true, screen: "sequence",
		sequence: sequenceState{
			commits: commits,
			changes: make([][]git.Change, len(commits)),
			states:  make([]sequenceLoadState, len(commits)),
			errors:  make([]string, len(commits)),
			loading: true, request: 1,
		},
	}
}

func TestSequenceStreamsCommitChunks(t *testing.T) {
	m := sequenceStreamModel([]git.CommitInfo{{Hash: "abcdef"}})
	m.sequence.states[0] = sequenceLoading
	first := git.CommitChunk{
		Paths:   []string{"one.go", "two.go"},
		Changes: []git.Change{{Path: "one.go", Diff: "@@ -0,0 +1 @@\n+one\n"}},
		Loaded:  1,
		Meta:    git.CommitInfo{Hash: "abcdef", Author: "Alice", Subject: "subject", Message: "subject\n\nbody"},
	}
	updated, cmd := m.Update(sequenceChunkLoaded{index: 0, request: 1, chunk: first})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("first chunk did not re-arm the stream")
	}
	if m.sequence.commits[0].Author != "Alice" || len(m.sequence.changes[0]) != 1 || len(m.rows) == 0 {
		t.Fatalf("first chunk state: %#v rows=%d", m.sequence.commits[0], len(m.rows))
	}
	done := git.CommitChunk{
		Paths:   []string{"one.go", "two.go"},
		Changes: []git.Change{{Path: "two.go", Diff: "@@ -0,0 +1 @@\n+two\n"}},
		Loaded:  2, Done: true,
	}
	updated, cmd = m.Update(sequenceChunkLoaded{index: 0, request: 1, chunk: done})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("done chunk should not re-arm the stream")
	}
	if m.sequence.states[0] != sequenceReady || m.sequence.loading || len(m.sequence.changes[0]) != 2 {
		t.Fatalf("done state: %#v", m.sequence)
	}
	found := false
	for _, row := range m.rows {
		if row.path == "two.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("final chunk rows are missing")
	}
}

func TestSequenceStreamMarksErrorsAndContinues(t *testing.T) {
	m := sequenceStreamModel([]git.CommitInfo{{Hash: "aaaa"}, {Hash: "bbbb"}})
	m.sequence.states[0], m.sequence.states[1] = sequenceLoading, sequencePending
	updated, _ := m.Update(sequenceChunkLoaded{index: 0, request: 1, chunk: git.CommitChunk{Done: true, Err: context.Canceled}})
	m = updated.(model)
	if m.sequence.states[0] != sequenceFailed || m.sequence.errors[0] == "" {
		t.Fatalf("failed state: %#v", m.sequence)
	}
}

func TestSequenceStreamStaleChunksIgnored(t *testing.T) {
	m := sequenceStreamModel([]git.CommitInfo{{Hash: "aaaa"}})
	m.sequence.states[0] = sequenceLoading
	updated, _ := m.Update(sequenceChunkLoaded{index: 0, request: 0, chunk: git.CommitChunk{Changes: []git.Change{{Path: "x", Diff: "+x"}}, Loaded: 1}})
	m = updated.(model)
	if len(m.sequence.changes[0]) != 0 {
		t.Fatal("stale chunk was applied")
	}
}

func TestSequenceStreamLoadsRealCommit(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "one.txt", "one\n")
	if _, err := r.Commit("first"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.ResolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	m := sequenceStreamModel([]git.CommitInfo{{Hash: commit.Hash}})
	m.repo = r
	m.sequence.states[0] = sequenceLoading
	cmd := m.startSequenceStream(0)
	if cmd == nil {
		t.Fatal("no stream command")
	}
	msg := cmd().(sequenceChunkLoaded)
	if msg.chunk.Err != nil || len(msg.chunk.Changes) == 0 || msg.chunk.Meta.Hash != commit.Hash {
		t.Fatalf("first chunk: %#v", msg.chunk)
	}
	m.resetSequenceStream()
}

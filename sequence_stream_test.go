package main

import (
	"context"
	"testing"
)

func sequenceStreamModel(commits []commitInfo) model {
	return model{
		width: 100, height: 20, wrap: true, screen: "sequence",
		sequence: sequenceState{
			commits: commits,
			changes: make([][]change, len(commits)),
			states:  make([]sequenceLoadState, len(commits)),
			errors:  make([]string, len(commits)),
			loading: true, request: 1,
		},
	}
}

func TestSequenceStreamsCommitChunks(t *testing.T) {
	m := sequenceStreamModel([]commitInfo{{hash: "abcdef"}})
	m.sequence.states[0] = sequenceLoading
	first := commitChunk{
		paths:   []string{"one.go", "two.go"},
		changes: []change{{path: "one.go", diff: "@@ -0,0 +1 @@\n+one\n"}},
		loaded:  1,
		meta:    commitInfo{hash: "abcdef", author: "Alice", subject: "subject", message: "subject\n\nbody"},
	}
	updated, cmd := m.Update(sequenceChunkLoaded{index: 0, request: 1, chunk: first})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("first chunk did not re-arm the stream")
	}
	if m.sequence.commits[0].author != "Alice" || len(m.sequence.changes[0]) != 1 || len(m.rows) == 0 {
		t.Fatalf("first chunk state: %#v rows=%d", m.sequence.commits[0], len(m.rows))
	}
	done := commitChunk{
		paths:   []string{"one.go", "two.go"},
		changes: []change{{path: "two.go", diff: "@@ -0,0 +1 @@\n+two\n"}},
		loaded:  2, done: true,
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
	m := sequenceStreamModel([]commitInfo{{hash: "aaaa"}, {hash: "bbbb"}})
	m.sequence.states[0], m.sequence.states[1] = sequenceLoading, sequencePending
	updated, _ := m.Update(sequenceChunkLoaded{index: 0, request: 1, chunk: commitChunk{done: true, err: context.Canceled}})
	m = updated.(model)
	if m.sequence.states[0] != sequenceFailed || m.sequence.errors[0] == "" {
		t.Fatalf("failed state: %#v", m.sequence)
	}
}

func TestSequenceStreamStaleChunksIgnored(t *testing.T) {
	m := sequenceStreamModel([]commitInfo{{hash: "aaaa"}})
	m.sequence.states[0] = sequenceLoading
	updated, _ := m.Update(sequenceChunkLoaded{index: 0, request: 0, chunk: commitChunk{changes: []change{{path: "x", diff: "+x"}}, loaded: 1}})
	m = updated.(model)
	if len(m.sequence.changes[0]) != 0 {
		t.Fatal("stale chunk was applied")
	}
}

func TestSequenceStreamLoadsRealCommit(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "one.txt", "one\n")
	if _, err := r.commit("first"); err != nil {
		t.Fatal(err)
	}
	commit, err := r.resolveCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	m := sequenceStreamModel([]commitInfo{{hash: commit.hash}})
	m.repo = r
	m.sequence.states[0] = sequenceLoading
	cmd := m.startSequenceStream(0)
	if cmd == nil {
		t.Fatal("no stream command")
	}
	msg := cmd().(sequenceChunkLoaded)
	if msg.chunk.err != nil || len(msg.chunk.changes) == 0 || msg.chunk.meta.hash != commit.hash {
		t.Fatalf("first chunk: %#v", msg.chunk)
	}
	m.resetSequenceStream()
}

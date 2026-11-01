package git

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestGitCommandStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := testRepo(t)
	r.Context = ctx
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd := r.gitCommand("cat-file", "--batch")
	cmd.Stdin = reader
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
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

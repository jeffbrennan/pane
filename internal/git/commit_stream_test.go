package git

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestPatchReaderMatchesSplitPatch(t *testing.T) {
	patch := "preamble\n" +
		"diff --git a/one b/one\n--- a/one\n+++ b/one\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/old b/new\nsimilarity index 100%\nrename from old\nrename to new\n" +
		"diff --git \"a/quoted file\" \"b/quoted file\"\nBinary files differ\n" +
		"diff --git a/empty b/empty\nnew file mode 100644\n" +
		"diff --git a/long b/long\n@@ -0,0 +1 @@\n+" + strings.Repeat("x", 10000) + "\n" +
		"Submodule space name contains modified content\nSubmodule space name abc1234..def5678:\n  > subject\n" +
		"Submodule new module 0000000...abcdef0 (new submodule)\n" +
		"diff --git a/one b/one\n@@ -1 +1 @@\n-again\n+final"
	want := splitPatch(patch)
	for _, reader := range []io.Reader{strings.NewReader(patch), iotest.OneByteReader(strings.NewReader(patch)), iotest.HalfReader(strings.NewReader(patch))} {
		stream := newPatchReader(reader)
		got := make(map[string]string)
		for {
			path, section, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if path != "" {
				got[path] += section
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("streamed sections differ:\ngot %#v\nwant %#v", got, want)
		}
	}
}

func TestStreamShape(t *testing.T) {
	dir := os.Getenv("PANE_PERF_REPO")
	if dir == "" || os.Getenv("PANE_PERF") != "1" {
		t.Skip("set PANE_PERF=1 and PANE_PERF_REPO")
	}
	repo := Repo{Root: dir}
	revision := os.Getenv("PANE_PERF_COMMIT")
	if revision == "" {
		revision = "HEAD"
	}
	for chunk := range repo.StreamCommit(context.Background(), revision, StartupSyntaxLines) {
		lines := 0
		for _, c := range chunk.Changes {
			lines += strings.Count(c.Diff, "\n")
		}
		t.Logf("loaded=%d files=%d lines=%d done=%t", chunk.Loaded, len(chunk.Changes), lines, chunk.Done)
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
	}
}

func TestStreamCommitMatchesCompleteLoad(t *testing.T) {
	r := testRepo(t)
	for i := 0; i < 30; i++ {
		writeFile(t, r, fmt.Sprintf("file-%02d.txt", i), strings.Repeat("some lines\n", 10))
	}
	writeFile(t, r, "quoted-界.txt", "quoted\n")
	if _, err := r.Commit("many files"); err != nil {
		t.Fatal(err)
	}
	want, err := r.CommitChangesWithoutSyntax("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got []Change
	chunks := 0
	for chunk := range r.StreamCommit(ctx, "HEAD", 20) {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		chunks++
		got = append(got, chunk.Changes...)
	}
	if chunks < 2 {
		t.Fatal("expected an early chunk")
	}
	byPath := make(map[string]Change)
	for _, c := range got {
		c.SyntaxSkipped = false
		byPath[c.Path] = c
	}
	if len(byPath) != len(want) {
		t.Fatalf("got %d files, want %d", len(byPath), len(want))
	}
	for _, c := range want {
		if byPath[c.Path] != c {
			t.Fatalf("change mismatch for %q", c.Path)
		}
	}
}

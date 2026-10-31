package main

import (
	"context"
	"fmt"
	"io"
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
		"Submodule space name contains modified content\nSubmodule space name abc1234..def5678:\n  > subject\n" +
		"Submodule new module 0000000...abcdef0 (new submodule)\n" +
		"diff --git a/one b/one\n@@ -1 +1 @@\n-again\n+final"
	want := splitPatch(patch)
	for _, reader := range []io.Reader{strings.NewReader(patch), iotest.OneByteReader(strings.NewReader(patch)), iotest.HalfReader(strings.NewReader(patch))} {
		stream := newPatchReader(reader)
		got := make(map[string]string)
		for {
			path, section, err := stream.next()
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

func TestStreamCommitMatchesCompleteLoad(t *testing.T) {
	r := testRepo(t)
	for i := 0; i < 30; i++ {
		writeFile(t, r, fmt.Sprintf("file-%02d.txt", i), strings.Repeat("some lines\n", 10))
	}
	writeFile(t, r, "quoted-界.txt", "quoted\n")
	if _, err := r.commit("many files"); err != nil {
		t.Fatal(err)
	}
	want, err := r.commitChangesWithoutSyntax("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got []change
	chunks := 0
	for chunk := range r.streamCommit(ctx, "HEAD", 20) {
		if chunk.err != nil {
			t.Fatal(chunk.err)
		}
		chunks++
		got = append(got, chunk.changes...)
	}
	if chunks < 2 {
		t.Fatal("expected an early chunk")
	}
	byPath := make(map[string]change)
	for _, c := range got {
		c.syntaxSkipped = false
		byPath[c.path] = c
	}
	if len(byPath) != len(want) {
		t.Fatalf("got %d files, want %d", len(byPath), len(want))
	}
	for _, c := range want {
		if byPath[c.path] != c {
			t.Fatalf("change mismatch for %q", c.path)
		}
	}
}

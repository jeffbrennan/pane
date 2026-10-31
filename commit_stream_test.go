package main

import (
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

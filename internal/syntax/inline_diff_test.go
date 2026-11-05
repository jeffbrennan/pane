package syntax

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestInlineChangesPairRemovedAndAddedLines(t *testing.T) {
	diff := "@@ -1 +1 @@\n-Hello, café world!\n+Hello, café there!\n"
	changes := InlineChanges(strings.Split(diff, "\n"))
	lines := strings.Split(diff, "\n")
	for _, tt := range []struct {
		index int
		word  string
	}{{1, "world"}, {2, "there"}} {
		spans := changes[tt.index]
		text := lines[tt.index][1:]
		if len(spans) != 1 || text[spans[0].Start:spans[0].End] != tt.word {
			t.Fatalf("incorrect inline ranges for line %d: %#v", tt.index, spans)
		}
	}
}

func TestInlineChangesSkipUnpairedAndUnrelatedLines(t *testing.T) {
	for _, diff := range []string{
		"@@ -1 +1,2 @@\n-one\n+one edited\n+extra\n",
		"@@ -1 +1 @@\n-completely different\n+unrelated text\n",
	} {
		for index, spans := range InlineChanges(strings.Split(diff, "\n")) {
			if len(spans) > 0 {
				t.Fatalf("unexpected inline highlight on line %d: %#v", index, spans)
			}
		}
	}
}

func TestInlineChangesStayBoundedOnHugeBlocks(t *testing.T) {
	var diff strings.Builder
	diff.WriteString("@@ -1,100 +1,100 @@\n")
	for i := 0; i < 100; i++ {
		diff.WriteString("-old line\n")
	}
	for i := 0; i < 100; i++ {
		diff.WriteString("+new line\n")
	}
	for index, spans := range InlineChanges(strings.Split(diff.String(), "\n")) {
		if len(spans) > 0 {
			t.Fatalf("line %d paired a block beyond the inline limit", index)
		}
	}
}

func TestChangedSpansRequireCommonText(t *testing.T) {
	if removed, added := ChangedSpans("completely different", "unrelated text"); removed != nil || added != nil {
		t.Fatalf("unrelated lines produced spans: %#v %#v", removed, added)
	}
	removed, added := ChangedSpans("hello beautiful world", "hello beautiful earth")
	if len(removed) != 1 || len(added) != 1 {
		t.Fatalf("missing changed spans: %#v %#v", removed, added)
	}
}

func TestChangedSpansCacheStaysBounded(t *testing.T) {
	for i := 0; i < inlineSpanCacheLimit+32; i++ {
		ChangedSpans(fmt.Sprintf("old line %d with common text", i), fmt.Sprintf("new line %d with common text", i))
	}
	inlineSpanCache.Lock()
	size := len(inlineSpanCache.entries)
	inlineSpanCache.Unlock()
	if size > inlineSpanCacheLimit {
		t.Fatalf("inline span cache grew to %d entries", size)
	}
	old, new := "hello beautiful world", "hello beautiful earth"
	removed, added := ChangedSpans(old, new)
	wantRemoved, wantAdded := changedSpans(old, new)
	if !slices.Equal(removed, wantRemoved) || !slices.Equal(added, wantAdded) {
		t.Fatalf("cached spans differ: %#v %#v != %#v %#v", removed, added, wantRemoved, wantAdded)
	}
}

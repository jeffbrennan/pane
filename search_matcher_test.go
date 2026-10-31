package main

import (
	"math/rand"
	"runtime"
	"strings"
	"testing"
)

func TestSearchMatcherMatchesOriginalSemantics(t *testing.T) {
	texts := []string{"", "Hello WORLD", "café Kelvin", "abc\xffdef", "abababa", "İstanbul"}
	queries := []string{"", "hello", "WORLD", "world", "é", "kelvin", "K", "aba", "missing", "istanbul"}
	for _, text := range texts {
		for _, query := range queries {
			want := query != "" && strings.Contains(searchFold(text, query), searchFold(query, query))
			if got := searchMatches(text, query); got != want {
				t.Errorf("searchMatches(%q, %q) = %v, want %v", text, query, got, want)
			}
		}
	}
}

func TestSearchMatcherRandomText(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	alphabet := []string{"a", "B", "z", " ", "K", "İ", "é", "界", "\xff"}
	makeText := func() string {
		var text strings.Builder
		for n := random.Intn(30); n > 0; n-- {
			text.WriteString(alphabet[random.Intn(len(alphabet))])
		}
		return text.String()
	}
	for i := 0; i < 5000; i++ {
		text, query := makeText(), makeText()
		want := query != "" && strings.Contains(searchFold(text, query), searchFold(query, query))
		if got := searchMatches(text, query); got != want {
			t.Fatalf("text=%q query=%q got %t want %t", text, query, got, want)
		}
	}
}

func TestParallelSearchMatchesSequentialOrder(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	rows := make([]row, 100013)
	for i := range rows {
		rows[i].text = "ordinary text"
	}
	for _, index := range []int{1, 2048, 15001, 50000, 99999} {
		rows[index].text = "Finding needle here"
	}
	rows[75000] = row{kind: 'f', text: "header", path: "dir/needle.go", oldPath: "old/previous.go"}
	m := model{rows: rows}
	random := rand.New(rand.NewSource(42))
	starts := []int{-1, 0, 1, 2047, 2048, 49999, 50000, len(rows) - 1, len(rows)}
	for i := 0; i < 20; i++ {
		starts = append(starts, random.Intn(len(rows)))
	}
	for _, direction := range []int{-1, 1} {
		for _, from := range starts {
			for _, query := range []string{"needle", "Needle", "previous", "missing", ""} {
				wantIndex, wantWrap := sequentialFindMatch(rows, query, from, direction)
				gotIndex, gotWrap := m.findMatch(query, from, direction)
				if gotIndex != wantIndex || gotWrap != wantWrap {
					t.Fatalf("query=%q from=%d direction=%d: got %d,%t want %d,%t", query, from, direction, gotIndex, gotWrap, wantIndex, wantWrap)
				}
			}
		}
	}
}

func sequentialFindMatch(rows []row, query string, from, direction int) (int, bool) {
	matcher := newSearchMatcher(query)
	for step := 1; step <= len(rows); step++ {
		index := from + direction*step
		wrapped := index < 0 || index >= len(rows)
		index = ((index % len(rows)) + len(rows)) % len(rows)
		if matcher.rowMatches(&rows[index]) {
			return index, wrapped
		}
	}
	return -1, false
}

func TestSearchMissNarrowingInvalidatesForChangedRows(t *testing.T) {
	m := model{height: 10, rows: []row{{text: "other"}, {text: "one"}}}
	m.startSearch()
	m.search.draft = "missing"
	m.updateSearchDraft()
	if m.search.missQuery != "missing" {
		t.Fatal("miss was not remembered")
	}
	m.search.draft = "missingx"
	m.updateSearchDraft()
	if m.search.missQuery != "missing" {
		t.Fatal("extension should reuse the miss")
	}
	m.rows = []row{{text: "other"}, {text: "missingx"}}
	m.updateSearchDraft()
	if m.pos != 1 || m.search.missQuery != "" {
		t.Fatal("changed rows must be searched again")
	}
}

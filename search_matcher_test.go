package main

import (
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

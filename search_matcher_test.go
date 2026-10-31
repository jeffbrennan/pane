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

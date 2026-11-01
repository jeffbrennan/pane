package main

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/mattn/go-runewidth"
)

func TestWrapLineMatchesOriginalBehavior(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	alphabet := []string{"a", "Z", " ", "界", "é", "\u0301", "\u200d", "😀", "\ufffd", "\xff", "\xc2", "\n", "\t"}
	for i := 0; i < 5000; i++ {
		var input strings.Builder
		for n := random.Intn(30); n > 0; n-- {
			input.WriteString(alphabet[random.Intn(len(alphabet))])
		}
		text := input.String()
		for _, width := range []int{-1, 0, 1, 2, 4, 10, 80} {
			got, want := wrapLine(text, width), referenceWrapLine(text, width)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("input=%q width=%d: got %q, want %q", text, width, got, want)
			}
		}
	}
}

func referenceWrapLine(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	var parts []string
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w > 0 && w+rw > width {
			parts = append(parts, b.String())
			b.Reset()
			w = 0
		}
		b.WriteRune(r)
		w += rw
	}
	return append(parts, b.String())
}

func TestWrapLineReusesValidText(t *testing.T) {
	text := "first second"
	parts := wrapLine(text, 5)
	if len(parts) < 2 || unsafe.StringData(parts[0]) != unsafe.StringData(text) {
		t.Fatal("wrapped parts should reuse the valid source text")
	}
}

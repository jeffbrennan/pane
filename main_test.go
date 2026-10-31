package main

import "testing"

func TestNavigationAndRefreshAnchor(t *testing.T) {
	m := model{width: 20, height: 10, wrap: true, snapshot: snapshot{unstaged: []change{{path: "one", diff: "+first\n+second\n"}, {path: "two", diff: "+other\n"}}}}
	m.rebuild()
	m.jump(1)
	if m.rows[m.pos].path != "two" {
		t.Fatal("next file")
	}
	m.jump(-1)
	if m.rows[m.pos].path != "one" || m.pos != 0 {
		t.Fatal("previous file")
	}
	m.move(2)
	m.rebuild()
	if m.rows[m.pos].path != "one" || m.rows[m.pos].line != 1 {
		t.Fatal("refresh anchor")
	}
}

func TestWrapAndSanitize(t *testing.T) {
	parts := wrapLine("ab界cd", 4)
	if len(parts) != 2 || parts[0] != "ab界" || parts[1] != "cd" {
		t.Fatalf("%q", parts)
	}
	if got := clean("a\x1b[31m"); got != "a�[31m" {
		t.Fatalf("unsafe text: %q", got)
	}
}

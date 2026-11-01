package ui

import "testing"

func TestUntrackedDiffPresentation(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "new.txt", "first\nsecond\n")
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	rows := buildRows(s.Unstaged, 80, true)
	if len(rows) != 3 || rowGutter(rows[1], 3) != "  1 + " || rows[1].text != "first" || rows[2].text != "second" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

package main

import (
	"strings"
	"testing"
)

func TestCommitReceiptStacksOutput(t *testing.T) {
	m := model{width: 60, height: 15, receipt: &commitReceipt{message: "Make review nicer", output: "[main abc1234] Make review nicer\n 2 files changed, 5 insertions(+), 1 deletion(-)"}}
	view := m.View()
	for _, want := range []string{"Commit created", "Make review nicer", "[main abc1234]", "2 files changed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %q", want, view)
		}
	}
	if strings.Count(view, "Make review nicer") != 1 || strings.Contains(view, "No changes") {
		t.Fatalf("unexpected receipt: %q", view)
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[4], "Make review nicer") || !strings.Contains(lines[5], "[main abc1234]") || !strings.Contains(lines[6], "2 files changed") {
		t.Fatalf("receipt is not stacked: %q", view)
	}
}

func TestCommitReceiptClearsOnChanges(t *testing.T) {
	m := model{width: 60, height: 15}
	updated, _ := m.Update(completed{message: "[main abc] message\n 1 file changed", commitMessage: "message"})
	m = updated.(model)
	if m.receipt == nil {
		t.Fatal("missing receipt")
	}
	baseline := snapshot{unstaged: []change{{path: "remaining", diff: "original"}}}
	updated, _ = m.Update(loaded{snapshot: baseline, generation: m.generation})
	m = updated.(model)
	if m.receipt == nil || m.receipt.baseline == nil {
		t.Fatal("missing baseline")
	}
	updated, _ = m.Update(loaded{snapshot: baseline, generation: m.generation})
	m = updated.(model)
	if m.receipt == nil {
		t.Fatal("receipt disappeared without changes")
	}
	changed := snapshot{unstaged: []change{{path: "remaining", diff: "edited"}}}
	updated, _ = m.Update(loaded{snapshot: changed, generation: m.generation})
	if updated.(model).receipt != nil {
		t.Fatal("receipt remains after edit")
	}
}

func TestCommitReceiptKeepsRemainingDiffVisible(t *testing.T) {
	m := model{width: 60, height: 15, receipt: &commitReceipt{message: "message"}, rows: []row{{text: "remaining diff"}}}
	if !strings.Contains(m.View(), "remaining diff") {
		t.Fatal("remaining diff hidden by receipt")
	}
}

func TestCommitReceiptColors(t *testing.T) {
	r := commitReceipt{message: "message", output: "[main abc] message\n 2 files changed, 5 insertions(+), 1 deletion(-)"}
	view := strings.Join(r.lines(80), "\n")
	for _, want := range []string{"\x1b[1;32m", "\x1b[1m  message", "\x1b[36m  [main abc]", "\x1b[32m5 insertions(+)", "\x1b[31m1 deletion(-)"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing style %q in %q", want, view)
		}
	}
}

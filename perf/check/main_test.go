package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadRunKeepsNewest(t *testing.T) {
	log := "date\ta\tcommit\tbuild_ns\t100\told\n" +
		"date\tb\tcommit\tbuild_ns\t300\tnew\n" +
		"date\tb\tcommit\trows\t42\tnew\n"
	current, err := readRun(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if current.id != "new" || len(current.values) != 2 {
		t.Fatalf("run=%#v", current)
	}
}

func TestReadBaselineIgnoresComments(t *testing.T) {
	baseline, err := readBaseline(strings.NewReader("# comment\n\ncommit\tbuild_ns\t100\n"))
	if err != nil || baseline[metricKey{"commit", "build_ns"}] != 100 {
		t.Fatalf("baseline=%v err=%v", baseline, err)
	}
}

func TestCheckFailsOnDoubledMetric(t *testing.T) {
	baseline := map[metricKey]int64{
		{"commit", "build_ns"}: 100_000_000,
		{"commit", "rows"}:     100,
	}
	current := run{id: "run", values: map[metricKey]int64{
		{"commit", "build_ns"}: 250_000_000,
		{"commit", "rows"}:     150,
	}}
	var output bytes.Buffer
	if failures := check(&output, baseline, current); failures != 1 {
		t.Fatalf("failures=%d output=%s", failures, output.String())
	}
	if !strings.Contains(output.String(), "regressed\tcommit\tbuild_ns") {
		t.Fatal(output.String())
	}
}

func TestRegressedIgnoresTinyDurationNoise(t *testing.T) {
	if regressed(100_000, 250_000, "build_ns") {
		t.Fatal("a sub-millisecond growth should not fail the gate")
	}
	if !regressed(10_000_000, 25_000_000, "build_ns") {
		t.Fatal("a 15 ms growth should fail")
	}
	if regressed(100, 150, "rows") {
		t.Fatal("1.5x is within the gate")
	}
}

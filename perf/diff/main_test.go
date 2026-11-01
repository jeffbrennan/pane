package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadAndCompareRuns(t *testing.T) {
	log := "old\tlegacy\tcommit\ttime\t10\n" +
		"date\ta\tcommit\ttime_ns\t100\trun1\n" +
		"date\ta\tcommit\tzero\t0\trun1\n" +
		"date\tb\tcommit\ttime_ns\t75\trun2\n" +
		"date\tb\tcommit\tzero\t1\trun2\n"
	runs, err := readRuns(strings.NewReader(log))
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%v err=%v", runs, err)
	}
	var output bytes.Buffer
	printDiff(&output, runs[0], runs[1])
	if !strings.Contains(output.String(), "100\t75\t-25.0%\tnoise") || !strings.Contains(output.String(), "0\t1\tn/a\tn/a") {
		t.Fatal(output.String())
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		metric string
		before int64
		after  int64
		want   string
	}{
		{"build_ns", 20_000_000, 21_500_000, "noise"},     // under the 2 ms floor
		{"build_ns", 20_000_000, 23_000_000, "regressed"}, // beyond 2 ms
		{"build_ns", 100_000_000, 90_000_000, "improved"}, // 10% faster
		{"build_ns", 100_000_000, 104_000_000, "noise"},   // 4% slower
		{"build_ns", 100_000_000, 106_000_000, "regressed"},
		{"heap_bytes", 1000, 1040, "noise"}, // 5% rule, no time floor
		{"heap_bytes", 1000, 1100, "regressed"},
	}
	for _, c := range cases {
		if got := classify(c.metric, c.before, c.after); got != c.want {
			t.Errorf("classify(%s, %d, %d) = %s, want %s", c.metric, c.before, c.after, got, c.want)
		}
	}
}

func TestIdleMetricsSkippedForShortOrUnequalWindows(t *testing.T) {
	log := "date\ta\tcommit\tidle_duration_ns\t1000000000\trun1\n" +
		"date\ta\tcommit\tidle_cpu_ns\t50000000\trun1\n" +
		"date\tb\tcommit\tidle_duration_ns\t60000000000\trun2\n" +
		"date\tb\tcommit\tidle_cpu_ns\t900000000\trun2\n" +
		"date\ta\thistory\tidle_duration_ns\t60000000000\trun1\n" +
		"date\ta\thistory\tidle_cpu_ns\t100000000\trun1\n" +
		"date\tb\thistory\tidle_duration_ns\t60000000000\trun2\n" +
		"date\tb\thistory\tidle_cpu_ns\t110000000\trun2\n"
	runs, err := readRuns(strings.NewReader(log))
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%v err=%v", runs, err)
	}
	var output bytes.Buffer
	printDiff(&output, runs[0], runs[1])
	text := output.String()
	if !strings.Contains(text, "skipping idle metrics for commit") {
		t.Fatalf("missing warning: %s", text)
	}
	if strings.Contains(text, "commit\tidle_cpu_ns") {
		t.Fatalf("compared idle CPU over different windows: %s", text)
	}
	if !strings.Contains(text, "history\tidle_cpu_ns") {
		t.Fatalf("dropped a comparable idle metric: %s", text)
	}
}

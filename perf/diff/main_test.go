package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadAndCompareRuns(t *testing.T) {
	log := "old\tlegacy\tcommit\ttime\t10\n" +
		"date\ta\tcommit\ttime\t100\trun1\n" +
		"date\ta\tcommit\tzero\t0\trun1\n" +
		"date\tb\tcommit\ttime\t75\trun2\n" +
		"date\tb\tcommit\tzero\t1\trun2\n"
	runs, err := readRuns(strings.NewReader(log))
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%v err=%v", runs, err)
	}
	var output bytes.Buffer
	printDiff(&output, runs[0], runs[1])
	if !strings.Contains(output.String(), "100\t75\t-25.0%") || !strings.Contains(output.String(), "0\t1\tn/a") {
		t.Fatal(output.String())
	}
}

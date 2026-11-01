package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type metricKey struct{ scenario, metric string }
type perfRun struct {
	id, revision string
	values       map[metricKey]int64
}

// idleDependentMetrics change with the length of the observed idle window, so
// they only compare meaningfully when both runs observed for the same time.
var idleDependentMetrics = map[string]bool{
	"idle_cpu_ns":         true,
	"idle_terminal_bytes": true,
}

const minComparableIdle = 30_000_000_000 // 30 s in nanoseconds

func readRuns(input io.Reader) ([]perfRun, error) {
	reader := csv.NewReader(input)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	var runs []perfRun
	indexes := make(map[string]int)
	for {
		fields, err := reader.Read()
		if err == io.EOF {
			return runs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(fields) < 6 || fields[5] == "" {
			continue
		}
		value, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, err
		}
		index, ok := indexes[fields[5]]
		if !ok {
			index = len(runs)
			indexes[fields[5]] = index
			runs = append(runs, perfRun{id: fields[5], revision: fields[1], values: make(map[metricKey]int64)})
		}
		runs[index].values[metricKey{fields[2], fields[3]}] = value
	}
}

// skipIdleMetrics reports the scenarios whose idle observations cannot be
// compared: a missing or unequal window, or one shorter than 30 s.
func skipIdleMetrics(previous, current perfRun) map[string]bool {
	skipped := make(map[string]bool)
	scenarios := make(map[string]bool)
	for key := range current.values {
		if key.metric == "idle_duration_ns" {
			scenarios[key.scenario] = true
		}
	}
	for key := range previous.values {
		if key.metric == "idle_duration_ns" {
			scenarios[key.scenario] = true
		}
	}
	for scenario := range scenarios {
		key := metricKey{scenario, "idle_duration_ns"}
		before, beforeOK := previous.values[key]
		after, afterOK := current.values[key]
		if !beforeOK || !afterOK || before != after || after < minComparableIdle {
			skipped[scenario] = true
		}
	}
	return skipped
}

func printDiff(output io.Writer, previous, current perfRun) {
	fmt.Fprintf(output, "%s (%s) -> %s (%s)\n", previous.id, previous.revision, current.id, current.revision)
	skipped := skipIdleMetrics(previous, current)
	if len(skipped) > 0 {
		scenarios := make([]string, 0, len(skipped))
		for scenario := range skipped {
			scenarios = append(scenarios, scenario)
		}
		sort.Strings(scenarios)
		fmt.Fprintf(output, "warning: skipping idle metrics for %s: the idle window differs between runs or is under 30s\n", strings.Join(scenarios, ", "))
	}
	fmt.Fprintln(output, "scenario\tmetric\tbefore\tafter\tchange\tstatus")
	keys := make([]metricKey, 0, len(current.values))
	for key := range current.values {
		if _, ok := previous.values[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].scenario != keys[j].scenario {
			return keys[i].scenario < keys[j].scenario
		}
		return keys[i].metric < keys[j].metric
	})
	for _, key := range keys {
		if skipped[key.scenario] && idleDependentMetrics[key.metric] {
			continue
		}
		before, after := previous.values[key], current.values[key]
		change := "n/a"
		if before != 0 {
			change = fmt.Sprintf("%+.1f%%", (float64(after)-float64(before))*100/float64(before))
		}
		fmt.Fprintf(output, "%s\t%s\t%d\t%d\t%s\t%s\n", key.scenario, key.metric, before, after, change, classify(key.metric, before, after))
	}
}

// classify labels a change against plan 14's noise rule: 5%, or 2 ms for
// durations under 40 ms. Values without a baseline cannot be classified.
func classify(metric string, before, after int64) string {
	if before == 0 {
		return "n/a"
	}
	threshold := math.Abs(float64(before)) * 0.05
	if strings.HasSuffix(metric, "_ns") && before < 40_000_000 {
		threshold = math.Max(threshold, 2_000_000)
	}
	change := float64(after) - float64(before)
	switch {
	case math.Abs(change) <= threshold:
		return "noise"
	case change < 0:
		return "improved"
	default:
		return "regressed"
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: perf-diff <log>")
		os.Exit(2)
	}
	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	runs, err := readRuns(file)
	if err != nil || len(runs) < 2 {
		fmt.Fprintf(os.Stderr, "need two performance runs: %v\n", err)
		os.Exit(1)
	}
	printDiff(os.Stdout, runs[len(runs)-2], runs[len(runs)-1])
}

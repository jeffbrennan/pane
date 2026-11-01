// Command perf-check compares the newest performance run in a log against a
// checked-in baseline and fails when a metric more than doubles.
package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

type metricKey struct{ scenario, metric string }

type run struct {
	id     string
	values map[metricKey]int64
}

const maxRatio = 2.0

func readRun(input io.Reader) (run, error) {
	reader := csv.NewReader(input)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	var latest run
	lastID := ""
	for {
		fields, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return run{}, err
		}
		if len(fields) < 6 || fields[5] == "" {
			continue
		}
		value, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return run{}, err
		}
		if fields[5] != lastID {
			lastID = fields[5]
			latest = run{id: lastID, values: make(map[metricKey]int64)}
		}
		latest.values[metricKey{fields[2], fields[3]}] = value
	}
	if lastID == "" {
		return run{}, fmt.Errorf("no runs with an id")
	}
	return latest, nil
}

func readBaseline(input io.Reader) (map[metricKey]int64, error) {
	values := make(map[metricKey]int64)
	scanner := bufio.NewScanner(input)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("line %d: want scenario, metric, value", line)
		}
		value, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		values[metricKey{fields[0], fields[1]}] = value
	}
	return values, scanner.Err()
}

// regressed reports whether after is more than twice before. Durations also
// have to grow by more than a millisecond so that microsecond noise on very
// small measurements does not fail the gate.
func regressed(before, after int64, metric string) bool {
	if before <= 0 || float64(after) <= float64(before)*maxRatio {
		return false
	}
	if strings.HasSuffix(metric, "_ns") && after-before < 1_000_000 {
		return false
	}
	return true
}

func check(output io.Writer, baseline map[metricKey]int64, current run) int {
	fmt.Fprintf(output, "run %s\n", current.id)
	keys := make([]metricKey, 0, len(baseline))
	for key := range baseline {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].scenario != keys[j].scenario {
			return keys[i].scenario < keys[j].scenario
		}
		return keys[i].metric < keys[j].metric
	})
	failures := 0
	for _, key := range keys {
		before := baseline[key]
		after, ok := current.values[key]
		if !ok {
			fmt.Fprintf(output, "missing\t%s\t%s\tbaseline %d\n", key.scenario, key.metric, before)
			continue
		}
		ratio := 0.0
		if before > 0 {
			ratio = float64(after) / float64(before)
		}
		status := "ok"
		if regressed(before, after, key.metric) {
			status = "regressed"
			failures++
		}
		fmt.Fprintf(output, "%s\t%s\t%s\t%d\t%d\t%.2fx\n", status, key.scenario, key.metric, before, after, ratio)
	}
	if failures > 0 {
		fmt.Fprintf(output, "%d metric(s) more than %.0fx worse than the baseline\n", failures, maxRatio)
	} else {
		fmt.Fprintln(output, "all metrics within the baseline")
	}
	return failures
}

func main() {
	baselinePath := flag.String("baseline", "perf/baseline.tsv", "baseline metrics")
	logPath := flag.String("log", ".scratch/perf-check.tsv", "results log")
	flag.Parse()
	baselineFile, err := os.Open(*baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer baselineFile.Close()
	baseline, err := readBaseline(baselineFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logFile, err := os.Open(*logPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer logFile.Close()
	current, err := readRun(logFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if check(os.Stdout, baseline, current) > 0 {
		os.Exit(1)
	}
}

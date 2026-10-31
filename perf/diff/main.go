package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
)

type metricKey struct{ scenario, metric string }
type perfRun struct {
	id, revision string
	values       map[metricKey]int64
}

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

func printDiff(output io.Writer, previous, current perfRun) {
	fmt.Fprintf(output, "%s (%s) -> %s (%s)\n", previous.id, previous.revision, current.id, current.revision)
	idle := metricKey{"commit", "idle_duration_ns"}
	if before, ok := previous.values[idle]; ok {
		if after, ok := current.values[idle]; ok && before != after {
			fmt.Fprintln(output, "warning: idle observation durations differ")
		}
	}
	fmt.Fprintln(output, "scenario\tmetric\tbefore\tafter\tchange")
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
		before, after := previous.values[key], current.values[key]
		change := "n/a"
		if before != 0 {
			change = fmt.Sprintf("%+.1f%%", (float64(after)-float64(before))*100/float64(before))
		}
		fmt.Fprintf(output, "%s\t%s\t%d\t%d\t%s\n", key.scenario, key.metric, before, after, change)
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

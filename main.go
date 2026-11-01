package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"

	"github.com/jeffbrennan/pane/internal/ui"
)

func main() {
	if debug.SetMemoryLimit(-1) > 1<<40 {
		// Large diffs otherwise let the runtime grow the heap well past what
		// the rows need; 1 GiB keeps collection responsive without thrashing.
		debug.SetMemoryLimit(1 << 30)
	}
	stopProfile := startCPUProfile(os.Getenv("PANE_CPUPROFILE"))
	stopTrace := startTrace(os.Getenv("PANE_TRACE"))
	err := ui.Run(os.Args[1:])
	stopTrace()
	stopProfile()
	if path := os.Getenv("PANE_MEMPROFILE"); path != "" {
		if profileErr := writeHeapProfile(path); profileErr != nil {
			fmt.Fprintln(os.Stderr, profileErr)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func startTrace(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := trace.Start(file); err != nil {
		file.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return func() {
		trace.Stop()
		file.Close()
	}
}

func writeHeapProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	runtime.GC()
	return pprof.WriteHeapProfile(file)
}

// startCPUProfile records a CPU profile to path, for building default.pgo
// from real sessions. It returns a function that stops recording.
func startCPUProfile(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return func() {
		pprof.StopCPUProfile()
		file.Close()
	}
}

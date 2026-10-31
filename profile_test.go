package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalProfiles(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.out")
	stop := startTrace(tracePath)
	stop()
	heapPath := filepath.Join(dir, "heap.pprof")
	if err := writeHeapProfile(heapPath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{tracePath, heapPath} {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("profile %s: %v", path, err)
		}
	}
}

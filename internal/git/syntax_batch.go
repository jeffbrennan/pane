package git

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

type FullContextResult struct {
	Diff string
	Err  error
}

// FullContexts keeps renames and unparseable sections on the per-file path,
// where Git's rename detection and quoted-path behavior are unchanged.
func (r Repo) FullContexts(changes []Change, key Context) []FullContextResult {
	results := make([]FullContextResult, len(changes))
	var batches [][]int
	var batch []int
	batchBytes := 0
	flush := func() {
		if len(batch) > 0 {
			batches = append(batches, batch)
			batch, batchBytes = nil, 0
		}
	}
	for i, c := range changes {
		if strings.Contains(c.Diff, "\nnew file mode ") {
			results[i].Diff = c.Diff
			continue
		}
		if c.OldPath != "" || !batchableSyntaxPath(c.Path) {
			batches = append(batches, []int{i})
			continue
		}
		if len(batch) >= 32 || batchBytes+len(c.Path)+1 > 64*1024 {
			flush()
		}
		batch = append(batch, i)
		batchBytes += len(c.Path) + 1
	}
	flush()
	parallelEach(len(batches), func(batchIndex int) {
		indexes := batches[batchIndex]
		loadOne := func(i int) {
			fileKey := key
			fileKey.Path = changes[i].Path
			results[i].Diff, results[i].Err = r.FullContext(fileKey, changes[i].Diff, changes[i].OldPath)
		}
		if len(indexes) == 1 {
			loadOne(indexes[0])
			return
		}
		paths := make([]string, len(indexes))
		for i, index := range indexes {
			paths[i] = changes[index].Path
		}
		out, err := r.Command(r.fullContextArgs(key, paths)...)
		var patches map[string]string
		if err == nil {
			patches = splitPatch(string(out))
		}
		var fallback []int
		for _, index := range indexes {
			if r.Context != nil && r.Context.Err() != nil {
				results[index].Err = r.Context.Err()
				continue
			}
			if diff, ok := patches[changes[index].Path]; ok {
				results[index].Diff = strings.Clone(diff)
			} else {
				fallback = append(fallback, index)
			}
		}
		parallelEach(len(fallback), func(i int) { loadOne(fallback[i]) })
	})
	return results
}

func batchableSyntaxPath(path string) bool {
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

// parallelEach runs f for every index with bounded concurrency, preserving
// input order in the results the caller writes.
func parallelEach(n int, f func(int)) {
	workers := min(n, runtime.GOMAXPROCS(0))
	if workers <= 1 {
		for i := range n {
			f(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				f(i)
			}
		}()
	}
	wg.Wait()
}

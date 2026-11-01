package main

import "strings"

type fullContextResult struct {
	diff string
	err  error
}

// fullContexts keeps renames and unparseable sections on the per-file path,
// where Git's rename detection and quoted-path behavior are unchanged.
func (r repository) fullContexts(changes []change, key contextKey) []fullContextResult {
	results := make([]fullContextResult, len(changes))
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
		if strings.Contains(c.diff, "\nnew file mode ") {
			results[i].diff = c.diff
			continue
		}
		if c.oldPath != "" || !batchableSyntaxPath(c.path) {
			batches = append(batches, []int{i})
			continue
		}
		if len(batch) >= 32 || batchBytes+len(c.path)+1 > 64*1024 {
			flush()
		}
		batch = append(batch, i)
		batchBytes += len(c.path) + 1
	}
	flush()
	parallelEach(len(batches), func(batchIndex int) {
		indexes := batches[batchIndex]
		loadOne := func(i int) {
			fileKey := key
			fileKey.path = changes[i].path
			results[i].diff, results[i].err = r.fullContext(fileKey, changes[i].diff, changes[i].oldPath)
		}
		if len(indexes) == 1 {
			loadOne(indexes[0])
			return
		}
		paths := make([]string, len(indexes))
		for i, index := range indexes {
			paths[i] = changes[index].path
		}
		out, err := r.command(r.fullContextArgs(key, paths)...)
		var patches map[string]string
		if err == nil {
			patches = splitPatch(string(out))
		}
		var fallback []int
		for _, index := range indexes {
			if r.context != nil && r.context.Err() != nil {
				results[index].err = r.context.Err()
				continue
			}
			if diff, ok := patches[changes[index].path]; ok {
				results[index].diff = strings.Clone(diff)
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

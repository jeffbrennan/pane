package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type fileFingerprint struct {
	size      int64
	mode      os.FileMode
	modified  time.Time
	missing   bool
	statError bool
}

type statusEntry struct {
	path, oldPath, record                            string
	staged, unstaged, untracked, conflict, submodule bool
	fingerprint                                      fileFingerprint
}

type changeKey struct {
	path   string
	staged bool
}

type cachedChange struct {
	entry    statusEntry
	change   change
	loadedAt time.Time
}

type refreshCache struct {
	branch      branchStatus
	ignoreSpace bool
	changes     map[changeKey]cachedChange
	snapshot    snapshot
}

func (r repository) status() (branchStatus, []statusEntry, error) {
	out, err := r.command("status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--renames", "--ahead-behind")
	if err != nil {
		return branchStatus{}, nil, gitError(out, err)
	}
	var entries []statusEntry
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		var entry statusEntry
		switch {
		case strings.HasPrefix(record, "1 "):
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 || len(fields[1]) != 2 {
				continue
			}
			entry = statusEntry{path: fields[8], record: record, staged: fields[1][0] != '.', unstaged: fields[1][1] != '.', submodule: fields[2] != "N..."}
		case strings.HasPrefix(record, "2 "):
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || len(fields[1]) != 2 || i+1 >= len(records) {
				continue
			}
			i++
			entry = statusEntry{path: fields[9], oldPath: string(records[i]), record: record, staged: fields[1][0] != '.', unstaged: fields[1][1] != '.', submodule: fields[2] != "N..."}
		case strings.HasPrefix(record, "u "):
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 {
				continue
			}
			entry = statusEntry{path: fields[10], record: record, unstaged: true, conflict: true}
		case strings.HasPrefix(record, "? "):
			entry = statusEntry{path: record[2:], record: record, unstaged: true, untracked: true}
		default:
			continue
		}
		if entry.unstaged && !entry.conflict {
			info, err := os.Lstat(filepath.Join(r.root, entry.path))
			if err != nil {
				entry.fingerprint.missing = os.IsNotExist(err)
				entry.fingerprint.statError = !entry.fingerprint.missing
			} else {
				entry.fingerprint = fileFingerprint{size: info.Size(), mode: info.Mode(), modified: info.ModTime()}
			}
		}
		entries = append(entries, entry)
	}
	return parseBranchStatus(out), entries, nil
}

func (r repository) loadCached(previous *refreshCache) (*refreshCache, bool, error) {
	started := time.Now()
	branch, entries, err := r.status()
	if err != nil {
		return nil, false, err
	}
	if previous != nil && (previous.branch.head != branch.head || previous.ignoreSpace != r.ignoreSpace) {
		previous = nil
	}
	next := &refreshCache{branch: branch, ignoreSpace: r.ignoreSpace, changes: make(map[changeKey]cachedChange), snapshot: snapshot{branch: branch}}
	var staged []change
	var unstaged, untracked []string
	for _, entry := range entries {
		for _, isStaged := range []bool{false, true} {
			if (isStaged && !entry.staged) || (!isStaged && !entry.unstaged) {
				continue
			}
			key := changeKey{entry.path, isStaged}
			if previous != nil {
				if cached, ok := previous.changes[key]; ok && cached.entry == entry && !entry.submodule && !entry.fingerprint.statError && (isStaged || entry.conflict || entry.fingerprint.missing || entry.fingerprint.modified.Before(cached.loadedAt.Add(-time.Second))) {
					next.changes[key] = cached
					continue
				}
			}
			next.changes[key] = cachedChange{entry: entry, loadedAt: started}
			switch {
			case entry.conflict:
				next.changes[key] = cachedChange{entry: entry, loadedAt: started, change: change{path: entry.path, diff: "Merge conflict: resolve outside pane.", conflict: true}}
			case isStaged:
				staged = append(staged, change{path: entry.path, oldPath: entry.oldPath})
			case entry.untracked:
				untracked = append(untracked, entry.path)
			default:
				unstaged = append(unstaged, entry.path)
			}
		}
	}
	groups := []struct {
		paths             []change
		staged, untracked bool
		changes           []change
		err               error
	}{{paths: staged, staged: true}, {paths: changesForPaths(unstaged)}, {paths: changesForPaths(untracked), untracked: true}}
	// The groups are independent, so their Git commands and tokenizing overlap.
	var wg sync.WaitGroup
	for i := range groups {
		group := &groups[i]
		if len(group.paths) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var changes []change
			if group.untracked {
				changes, group.err = parallelDiffs(untracked, r.untrackedDiff)
			} else {
				changes, group.err = r.diffsForChanges(group.paths, group.staged)
			}
			if group.err != nil {
				return
			}
			for i := range changes {
				if group.untracked {
					changes[i].untracked = true
					if strings.Contains(changes[i].diff, "Binary files ") {
						changes[i].untrackedHash, changes[i].untrackedHashValid = untrackedDigest(filepath.Join(r.root, changes[i].path))
					}
				}
			}
			r.loadSyntaxDiffs(changes, contextKey{staged: group.staged})
			warmSyntax(changes)
			group.changes = changes
		}()
	}
	wg.Wait()
	for _, group := range groups {
		if group.err != nil {
			return nil, false, group.err
		}
		for _, c := range group.changes {
			key := changeKey{c.path, group.staged}
			cached := next.changes[key]
			cached.change = c
			next.changes[key] = cached
		}
	}
	for _, entry := range entries {
		if entry.unstaged {
			next.snapshot.unstaged = append(next.snapshot.unstaged, next.changes[changeKey{entry.path, false}].change)
		}
		if entry.staged {
			next.snapshot.staged = append(next.snapshot.staged, next.changes[changeKey{entry.path, true}].change)
		}
	}
	return next, previous != nil && previous.snapshot.equal(next.snapshot), nil
}

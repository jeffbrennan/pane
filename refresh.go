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
	// plain marks a change loaded without highlighting; the next load
	// reloads it with highlighting.
	plain bool
}

type refreshCache struct {
	branch      branchStatus
	ignoreSpace bool
	changes     map[changeKey]cachedChange
	snapshot    snapshot
	plain       bool
}

// startupSyntaxLines is how many diff lines of each view are highlighted
// before the first screen appears: more than a tall terminal shows. Files
// past it start plain and are highlighted by an immediate second load, before
// they can be scrolled into view.
const startupSyntaxLines = 120

// deferOffscreenSyntax drops highlighting from changes past the first
// startupSyntaxLines lines of each view, in display order.
func deferOffscreenSyntax(cache *refreshCache, entries []statusEntry) {
	for _, staged := range []bool{false, true} {
		shown := 0
		for _, entry := range entries {
			if (staged && !entry.staged) || (!staged && !entry.unstaged) {
				continue
			}
			key := changeKey{entry.path, staged}
			cached := cache.changes[key]
			if shown >= startupSyntaxLines && cached.change.syntaxDiff != "" {
				cached.change.syntaxDiff, cached.plain = "", true
				cache.changes[key] = cached
				cache.plain = true
			}
			shown += strings.Count(cached.change.diff, "\n")
		}
	}
}

func (r repository) status() (branchStatus, []statusEntry, error) {
	out, err := r.statusOutput()
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

// statusOutput runs status with untracked files listed one by one.
//
// In large repositories most of status is its walk for untracked files, which
// Git's untracked cache skips. Git uses the cache only in the untracked mode
// it was built for, and rebuilds it whenever the mode changes, so status runs
// in the configured mode, which the user's own status shares. That output
// lists each untracked file unless a whole directory is untracked; those
// directories are expanded by walking only them. When untracked files are
// hidden, status lists every file itself, which bypasses the cache without
// discarding it.
func (r repository) statusOutput() ([]byte, error) {
	args := []string{"status", "--porcelain=v2", "-z", "--branch", "--renames", "--ahead-behind"}
	if r.untrackedCache {
		args = append([]string{"-c", "core.untrackedCache=true"}, args...)
	}
	if r.untrackedHidden {
		return r.command(append(args, "--untracked-files=all")...)
	}
	out, err := r.command(args...)
	if err != nil {
		return out, err
	}
	return r.expandUntrackedDirectories(out)
}

// expandUntrackedDirectories replaces each untracked directory in status
// output with the untracked files inside it, as --untracked-files=all lists
// them.
func (r repository) expandUntrackedDirectories(out []byte) ([]byte, error) {
	records := bytes.Split(out, []byte{0})
	var directories []string
	for i := 0; i < len(records); i++ {
		switch record := records[i]; {
		case bytes.HasPrefix(record, []byte("2 ")):
			i++ // the original path of a rename
		case bytes.HasPrefix(record, []byte("? ")) && bytes.HasSuffix(record, []byte("/")):
			directories = append(directories, string(record[2:]))
		}
	}
	if len(directories) == 0 {
		return out, nil
	}
	listed, err := r.command(append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, directories...)...)
	if err != nil {
		return nil, gitError(listed, err)
	}
	files := make(map[string][][]byte)
	for _, file := range bytes.Split(listed, []byte{0}) {
		for _, directory := range directories {
			if len(file) > 0 && bytes.HasPrefix(file, []byte(directory)) {
				files[directory] = append(files[directory], append([]byte("? "), file...))
				break
			}
		}
	}
	expanded := make([][]byte, 0, len(records))
	for i := 0; i < len(records); i++ {
		record := records[i]
		if bytes.HasPrefix(record, []byte("2 ")) && i+1 < len(records) {
			expanded = append(expanded, record, records[i+1])
			i++
			continue
		}
		if bytes.HasPrefix(record, []byte("? ")) && bytes.HasSuffix(record, []byte("/")) && len(files[string(record[2:])]) > 0 {
			expanded = append(expanded, files[string(record[2:])]...)
			continue
		}
		expanded = append(expanded, record)
	}
	return bytes.Join(expanded, []byte{0}), nil
}

func (r repository) loadCached(previous *refreshCache) (*refreshCache, bool, error) {
	return r.loadChanges(previous, false)
}

// loadChanges loads the working tree status and diffs, reusing unchanged
// entries from previous. deferOffscreen is for the first screen at startup;
// see startupSyntaxLines.
func (r repository) loadChanges(previous *refreshCache, deferOffscreen bool) (*refreshCache, bool, error) {
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
				if cached, ok := previous.changes[key]; ok && cached.entry == entry && !cached.plain && !entry.submodule && !entry.fingerprint.statError && (isStaged || entry.conflict || entry.fingerprint.missing || entry.fingerprint.modified.Before(cached.loadedAt.Add(-time.Second))) {
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
			syntaxKey := contextKey{staged: group.staged}
			if group.untracked {
				changes, group.err = parallelDiffs(untracked, r.untrackedDiff)
				if group.err != nil {
					return
				}
				r.loadSyntaxDiffs(changes, syntaxKey)
			} else {
				// Full-context diffs for highlighting need only paths, so
				// they load alongside the display diffs.
				syntaxDiffs := make(chan []string, 1)
				go func() { syntaxDiffs <- r.syntaxDiffs(group.paths, syntaxKey) }()
				changes, group.err = r.diffsForChanges(group.paths, group.staged)
				diffs := <-syntaxDiffs
				if group.err != nil {
					return
				}
				for i := range changes {
					changes[i].syntaxDiff = diffs[i]
				}
				markSyntaxSkipped(changes, syntaxBudgetEnd(group.paths))
			}
			for i := range changes {
				if group.untracked {
					changes[i].untracked = true
					if strings.Contains(changes[i].diff, "Binary files ") {
						changes[i].untrackedHash, changes[i].untrackedHashValid = untrackedDigest(filepath.Join(r.root, changes[i].path))
					}
				}
			}
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
	if deferOffscreen {
		deferOffscreenSyntax(next, entries)
	}
	var highlight []change
	for _, group := range groups {
		for _, c := range group.changes {
			if cached := next.changes[changeKey{c.path, group.staged}]; !cached.plain {
				highlight = append(highlight, cached.change)
			}
		}
	}
	warmSyntax(highlight)
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

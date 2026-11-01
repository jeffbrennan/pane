package git

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileFingerprint struct {
	Size      int64
	Mode      os.FileMode
	Modified  time.Time
	Missing   bool
	StatError bool
}

type StatusEntry struct {
	Path, OldPath, Record                            string
	Staged, Unstaged, Untracked, Conflict, Submodule bool
	Fingerprint                                      FileFingerprint
}

type ChangeKey struct {
	Path   string
	Staged bool
}

type CachedChange struct {
	Entry    StatusEntry
	Change   Change
	LoadedAt time.Time
	// Plain marks a change loaded without highlighting; the next load
	// reloads it with highlighting.
	Plain bool
}

type RefreshCache struct {
	Branch      BranchStatus
	IgnoreSpace bool
	Changes     map[ChangeKey]CachedChange
	Snapshot    Snapshot
	Plain       bool
}

// startupSyntaxLines is how many diff lines of each view are highlighted
// before the first screen appears: more than a tall terminal shows. Files
// past it start plain and are highlighted by an immediate second load, before
// they can be scrolled into view.
const startupSyntaxLines = 120

// deferOffscreenSyntax drops highlighting from changes past the first
// startupSyntaxLines lines of each view, in display order.
func deferOffscreenSyntax(cache *RefreshCache, entries []StatusEntry) {
	for _, staged := range []bool{false, true} {
		shown := 0
		for _, entry := range entries {
			if (staged && !entry.Staged) || (!staged && !entry.Unstaged) {
				continue
			}
			key := ChangeKey{entry.Path, staged}
			cached := cache.Changes[key]
			if shown >= startupSyntaxLines && cached.Change.SyntaxDiff != "" {
				cached.Change.SyntaxDiff, cached.Plain = "", true
				cache.Changes[key] = cached
				cache.Plain = true
			}
			shown += strings.Count(cached.Change.Diff, "\n")
		}
	}
}

func (r Repo) Status() (BranchStatus, []StatusEntry, error) {
	out, err := r.StatusOutput()
	if err != nil {
		return BranchStatus{}, nil, GitError(out, err)
	}
	var entries []StatusEntry
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		var entry StatusEntry
		switch {
		case strings.HasPrefix(record, "1 "):
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 || len(fields[1]) != 2 {
				continue
			}
			entry = StatusEntry{Path: fields[8], Record: record, Staged: fields[1][0] != '.', Unstaged: fields[1][1] != '.', Submodule: fields[2] != "N..."}
		case strings.HasPrefix(record, "2 "):
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || len(fields[1]) != 2 || i+1 >= len(records) {
				continue
			}
			i++
			entry = StatusEntry{Path: fields[9], OldPath: string(records[i]), Record: record, Staged: fields[1][0] != '.', Unstaged: fields[1][1] != '.', Submodule: fields[2] != "N..."}
		case strings.HasPrefix(record, "u "):
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 {
				continue
			}
			entry = StatusEntry{Path: fields[10], Record: record, Unstaged: true, Conflict: true}
		case strings.HasPrefix(record, "? "):
			entry = StatusEntry{Path: record[2:], Record: record, Unstaged: true, Untracked: true}
		default:
			continue
		}
		if entry.Unstaged && !entry.Conflict {
			info, err := os.Lstat(filepath.Join(r.Root, entry.Path))
			if err != nil {
				entry.Fingerprint.Missing = os.IsNotExist(err)
				entry.Fingerprint.StatError = !entry.Fingerprint.Missing
			} else {
				entry.Fingerprint = FileFingerprint{Size: info.Size(), Mode: info.Mode(), Modified: info.ModTime()}
			}
		}
		entries = append(entries, entry)
	}
	return ParseBranchStatus(out), entries, nil
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
func (r Repo) StatusOutput() ([]byte, error) {
	args := []string{"status", "--porcelain=v2", "-z", "--branch", "--renames", "--ahead-behind"}
	if r.UntrackedCache {
		args = append([]string{"-c", "core.UntrackedCache=true"}, args...)
	}
	if r.UntrackedHidden {
		return r.Command(append(args, "--untracked-files=all")...)
	}
	out, err := r.Command(args...)
	if err != nil {
		return out, err
	}
	return r.expandUntrackedDirectories(out)
}

// expandUntrackedDirectories replaces each untracked directory in status
// output with the untracked files inside it, as --untracked-files=all lists
// them.
func (r Repo) expandUntrackedDirectories(out []byte) ([]byte, error) {
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
	listed, err := r.Command(append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, directories...)...)
	if err != nil {
		return nil, GitError(listed, err)
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

func (r Repo) LoadCached(previous *RefreshCache) (*RefreshCache, bool, error) {
	return r.LoadChanges(previous, false)
}

// LoadChanges loads the working tree status and diffs, reusing unchanged
// entries from previous. deferOffscreen is for the first screen at startup;
// see startupSyntaxLines.
func (r Repo) LoadChanges(previous *RefreshCache, deferOffscreen bool) (*RefreshCache, bool, error) {
	started := time.Now()
	branch, entries, err := r.Status()
	if err != nil {
		return nil, false, err
	}
	if previous != nil && (previous.Branch.Head != branch.Head || previous.IgnoreSpace != r.IgnoreSpace) {
		previous = nil
	}
	next := &RefreshCache{Branch: branch, IgnoreSpace: r.IgnoreSpace, Changes: make(map[ChangeKey]CachedChange), Snapshot: Snapshot{Branch: branch}}
	var staged []Change
	var unstaged, untracked []string
	for _, entry := range entries {
		for _, isStaged := range []bool{false, true} {
			if (isStaged && !entry.Staged) || (!isStaged && !entry.Unstaged) {
				continue
			}
			key := ChangeKey{entry.Path, isStaged}
			if previous != nil {
				if cached, ok := previous.Changes[key]; ok && cached.Entry == entry && !cached.Plain && !entry.Submodule && !entry.Fingerprint.StatError && (isStaged || entry.Conflict || entry.Fingerprint.Missing || entry.Fingerprint.Modified.Before(cached.LoadedAt.Add(-time.Second))) {
					next.Changes[key] = cached
					continue
				}
			}
			next.Changes[key] = CachedChange{Entry: entry, LoadedAt: started}
			switch {
			case entry.Conflict:
				next.Changes[key] = CachedChange{Entry: entry, LoadedAt: started, Change: Change{Path: entry.Path, Diff: "Merge conflict: resolve outside pane.", Conflict: true}}
			case isStaged:
				staged = append(staged, Change{Path: entry.Path, OldPath: entry.OldPath})
			case entry.Untracked:
				untracked = append(untracked, entry.Path)
			default:
				unstaged = append(unstaged, entry.Path)
			}
		}
	}
	groups := []struct {
		paths             []Change
		staged, untracked bool
		changes           []Change
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
			var changes []Change
			syntaxKey := Context{Staged: group.staged}
			if group.untracked {
				changes, group.err = parallelDiffs(untracked, r.untrackedDiff)
				if group.err != nil {
					return
				}
				r.LoadSyntaxDiffs(changes, syntaxKey)
			} else {
				// Full-context diffs for highlighting need only paths, so
				// they load alongside the display diffs.
				syntaxDiffs := make(chan []string, 1)
				go func() { syntaxDiffs <- r.SyntaxDiffs(group.paths, syntaxKey) }()
				changes, group.err = r.diffsForChanges(group.paths, group.staged)
				diffs := <-syntaxDiffs
				if group.err != nil {
					return
				}
				for i := range changes {
					changes[i].SyntaxDiff = diffs[i]
				}
				markSyntaxSkipped(changes, syntaxBudgetEnd(group.paths))
			}
			for i := range changes {
				if group.untracked {
					changes[i].Untracked = true
					if strings.Contains(changes[i].Diff, "Binary files ") {
						changes[i].UntrackedHash, changes[i].UntrackedHashValid = untrackedDigest(filepath.Join(r.Root, changes[i].Path))
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
			key := ChangeKey{c.Path, group.staged}
			cached := next.Changes[key]
			c.Generated = GeneratedByPath(c.Path)
			cached.Change = c
			next.Changes[key] = cached
		}
	}
	if deferOffscreen {
		deferOffscreenSyntax(next, entries)
	}
	var highlight []Change
	for _, group := range groups {
		for _, c := range group.changes {
			if cached := next.Changes[ChangeKey{c.Path, group.staged}]; !cached.Plain {
				highlight = append(highlight, cached.Change)
			}
		}
	}
	WarmSyntax(highlight)
	for _, entry := range entries {
		if entry.Unstaged {
			next.Snapshot.Unstaged = append(next.Snapshot.Unstaged, next.Changes[ChangeKey{entry.Path, false}].Change)
		}
		if entry.Staged {
			next.Snapshot.Staged = append(next.Snapshot.Staged, next.Changes[ChangeKey{entry.Path, true}].Change)
		}
	}
	return next, previous != nil && previous.Snapshot.Equal(next.Snapshot), nil
}

package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type CommitInfo struct {
	Hash, Author, Email, Date, Subject, Message string
	Committer, CommitterEmail, CommitterDate    string
	Refs                                        string
	Parents                                     []string
	FilePath                                    string
	Search                                      string
	Added, Deleted, Binary                      int
	StatsLoaded                                 bool
}

const HistoryPage = 200

func (r Repo) ResolveCommit(revision string) (CommitInfo, error) {
	if revision == "" {
		return CommitInfo{}, fmt.Errorf("commit revision cannot be empty")
	}
	out, err := r.Command("rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return CommitInfo{}, fmt.Errorf("invalid commit %q: %w", revision, CommandError(out, err))
	}
	hash := strings.TrimSpace(string(out))
	commit, err := r.CommitDetail(hash)
	if err != nil {
		return CommitInfo{}, err
	}
	if commit.Hash == "" {
		return CommitInfo{}, fmt.Errorf("could not read commit %q", revision)
	}
	return commit, nil
}

// CommitDetail loads the metadata a commit header shows: identity, dates,
// parents, refs and the full message. It runs alongside the patch stream.
func (r Repo) CommitDetail(hash string) (CommitInfo, error) {
	out, err := r.CommandStdout("show", "-s", "--decorate=short", "-z",
		"--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00%P%x00%cn%x00%ce%x00%cI%x00%D%x00", hash, "--")
	if err != nil {
		return CommitInfo{}, CommandError(out, err)
	}
	return parseCommitDetail(out)
}

func parseCommitDetail(out []byte) (CommitInfo, error) {
	fields := bytes.Split(out, []byte{0})
	if len(fields) < 12 {
		return CommitInfo{}, fmt.Errorf("unexpected Git commit detail format")
	}
	commit := CommitInfo{
		Hash: string(fields[1]), Author: string(fields[2]), Email: string(fields[3]),
		Date: string(fields[4]), Subject: string(fields[5]), Message: string(fields[6]),
		Committer: string(fields[8]), CommitterEmail: string(fields[9]), CommitterDate: string(fields[10]),
		Refs: string(fields[11]),
	}
	if parents := strings.Fields(string(fields[7])); len(parents) > 0 {
		commit.Parents = parents
	}
	commit.Search = strings.ToLower(commit.Hash + "\n" + commit.Author + "\n" + commit.Email + "\n" + commit.Message + "\n" + commit.Subject)
	return commit, nil
}

// History loads up to limit commits after skipping the newest skip commits.
func (r Repo) History(skip, limit int) ([]CommitInfo, error) {
	return r.HistoryWithDate(skip, limit, "%aI")
}

func (r Repo) HistoryWithDate(skip, limit int, dateFormat string) ([]CommitInfo, error) {
	return r.historyWithPath(skip, limit, dateFormat, "")
}

func (r Repo) historyWithPath(skip, limit int, dateFormat, path string) ([]CommitInfo, error) {
	out, err := r.Command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, CommandError(out, err)
	}
	format := "--format=%x00%H%x00%an%x00%ae%x00" + dateFormat + "%x00%s%x00%B%x00"
	logSkip, logLimit := skip, limit
	if path != "" {
		logSkip, logLimit = 0, skip+limit
	}
	args := []string{"log", fmt.Sprintf("--skip=%d", logSkip), fmt.Sprintf("--max-count=%d", logLimit), "-z", format}
	if path != "" {
		args = append(args, "--follow", "--name-status", "-M", "--", path)
	}
	out, err = r.CommandStdout(args...)
	if err != nil {
		return nil, CommandError(out, err)
	}
	if path != "" {
		commits, err := parseFileHistory(out, path)
		if err != nil {
			return nil, err
		}
		return commits[min(skip, len(commits)):], nil
	}
	return parseHistory(out, false)
}

func parseFileHistory(out []byte, path string) ([]CommitInfo, error) {
	fields := bytes.Split(out, []byte{0})
	var commits []CommitInfo
	for i := 0; i < len(fields); {
		for i < len(fields) && len(fields[i]) == 0 {
			i++
		}
		if i == len(fields) {
			break
		}
		if i+6 > len(fields) {
			return nil, fmt.Errorf("unexpected Git file history format")
		}
		c := CommitInfo{Hash: string(fields[i]), Author: string(fields[i+1]), Email: string(fields[i+2]), Date: string(fields[i+3]), Subject: string(fields[i+4]), Message: string(fields[i+5]), FilePath: path}
		c.Search = strings.ToLower(c.Hash + "\n" + c.Author + "\n" + c.Email + "\n" + c.Message + "\n" + c.Subject)
		i += 6
		for i < len(fields) && len(fields[i]) == 0 {
			i++
		}
		for i < len(fields) && len(fields[i]) > 0 {
			status := strings.TrimPrefix(string(fields[i]), "\n")
			if status == "" || !strings.ContainsRune("AMDRCTUXB", rune(status[0])) {
				break
			}
			i++
			if i >= len(fields) || len(fields[i]) == 0 {
				return nil, fmt.Errorf("unexpected Git file history path")
			}
			c.FilePath = string(fields[i])
			i++
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				if i >= len(fields) || len(fields[i]) == 0 {
					return nil, fmt.Errorf("unexpected Git file history rename")
				}
				c.FilePath = string(fields[i])
				i++
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func (r Repo) HistoryStats(hashes []string) ([]CommitInfo, error) {
	args := []string{"log", "--no-walk=unsorted", "-z", "--numstat", "-M", "--no-ext-diff", "--no-textconv", "--diff-merges=first-parent", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00"}
	args = append(args, hashes...)
	args = append(args, "--")
	out, err := r.CommandStdout(args...)
	if err != nil {
		return nil, CommandError(out, err)
	}
	commits, err := parseHistory(out, true)
	if err == nil {
		r.Stats.put(commits)
	}
	return commits, err
}

func parseHistory(out []byte, statsLoaded bool) ([]CommitInfo, error) {
	fields := bytes.Split(out, []byte{0})
	var commits []CommitInfo
	for i := 0; i < len(fields); {
		if len(fields[i]) == 0 {
			i++
			continue
		}
		if i+6 > len(fields) {
			return nil, fmt.Errorf("unexpected Git history format")
		}
		c := CommitInfo{Hash: string(fields[i]), Author: string(fields[i+1]), Email: string(fields[i+2]), Date: string(fields[i+3]), Subject: string(fields[i+4]), Message: string(fields[i+5]), StatsLoaded: statsLoaded}
		c.Search = strings.ToLower(c.Hash + "\n" + c.Author + "\n" + c.Email + "\n" + c.Message + "\n" + c.Subject)
		i += 6
		for i < len(fields) && len(fields[i]) == 0 {
			i++
		}
		for i < len(fields) && len(fields[i]) > 0 && bytes.Contains(fields[i], []byte("\t")) {
			parts := bytes.SplitN(bytes.TrimPrefix(fields[i], []byte("\n")), []byte("\t"), 3)
			if len(parts) != 3 {
				return nil, fmt.Errorf("unexpected Git history statistics")
			}
			if string(parts[0]) == "-" && string(parts[1]) == "-" {
				c.Binary++
			} else {
				added, addErr := strconv.Atoi(string(parts[0]))
				deleted, delErr := strconv.Atoi(string(parts[1]))
				if addErr != nil || delErr != nil {
					return nil, fmt.Errorf("unexpected Git history statistics")
				}
				c.Added += added
				c.Deleted += deleted
			}
			i++
			if len(parts[2]) == 0 {
				if i+1 >= len(fields) || len(fields[i]) == 0 || len(fields[i+1]) == 0 {
					return nil, fmt.Errorf("unexpected Git rename statistics")
				}
				i += 2
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func (r Repo) CommitChanges(hash string) ([]Change, error) {
	changes, err := r.CommitChangesWithoutSyntax(hash)
	if err == nil {
		r.LoadSyntaxDiffs(changes, Context{Commit: hash})
	}
	return changes, err
}

// CommitChangesFirstScreen loads a commit's changes with the files on its
// first screen highlighted, so opening it paints in color. The first screen
// starts at focus when a change has that path. Later files start plain for
// loadCommitSyntax, or in a commit over the syntax budget for syntaxDemand;
// see startupSyntaxLines.
func (r Repo) CommitChangesFirstScreen(hash, focus string) ([]Change, error) {
	changes, err := r.CommitChangesWithoutSyntax(hash)
	if err != nil {
		return changes, err
	}
	start := 0
	for i, c := range changes {
		if focus != "" && (c.Path == focus || c.OldPath == focus) {
			start = i
			break
		}
	}
	end, shown := start, 0
	for ; end < len(changes) && shown < StartupSyntaxLines; end++ {
		shown += strings.Count(changes[end].Diff, "\n")
	}
	first := changes[start:end]
	r.LoadSyntaxDiffs(first, Context{Commit: hash})
	WarmSyntax(first)
	// Past the first screen, a commit over the syntax budget highlights files
	// as they near the viewport instead of all at once.
	rest := append(append([]Change(nil), changes[:start]...), changes[end:]...)
	if syntaxBudgetEnd(rest) < len(rest) {
		markSyntaxSkipped(changes[:start], 0)
		markSyntaxSkipped(changes[end:], 0)
	}
	return changes, nil
}

func (r Repo) CommitChangesWithoutSyntax(hash string) ([]Change, error) {
	// The patch does not depend on the name status, so both load at once.
	type result struct {
		out []byte
		err error
	}
	patchResult := make(chan result, 1)
	go func() {
		args := append([]string{"show", "--first-parent", "--format="}, r.diffOptions()...)
		args = append(args, "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
		out, err := r.CommandStdout(args...)
		patchResult <- result{out, err}
	}()
	out, err := r.CommandStdout("show", "--first-parent", "--format=", "--name-status", "-M", "-z", hash, "--")
	loadedPatch := <-patchResult
	if r.Context != nil && r.Context.Err() != nil {
		return nil, r.Context.Err()
	}
	if err != nil {
		return nil, CommandError(out, err)
	}
	patch, err := loadedPatch.out, loadedPatch.err
	if err != nil {
		return nil, CommandError(patch, err)
	}
	patches := splitPatch(string(patch))
	paths, oldPaths, err := parseCommitPaths(out)
	if err != nil {
		return nil, err
	}
	generated := r.GeneratedPaths(paths)
	changes, err := parallelDiffs(paths, func(path string) (string, error) {
		if r.Context != nil && r.Context.Err() != nil {
			return "", r.Context.Err()
		}
		diff, ok := patches[path]
		return r.commitFileDiff(hash, path, oldPaths[path], diff, ok)
	})
	for i := range changes {
		changes[i].OldPath = oldPaths[changes[i].Path]
		changes[i].Generated = generated[changes[i].Path]
	}
	return changes, err
}

func parseCommitPaths(out []byte) ([]string, map[string]string, error) {
	var paths []string
	oldPaths := make(map[string]string)
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields) && len(fields[i]) > 0; {
		status := string(fields[i])
		i++
		if i >= len(fields) || len(fields[i]) == 0 {
			return nil, nil, fmt.Errorf("unexpected Git change format")
		}
		path := string(fields[i])
		i++
		if strings.HasPrefix(status, "R") {
			if i >= len(fields) || len(fields[i]) == 0 {
				return nil, nil, fmt.Errorf("unexpected Git rename format")
			}
			oldPaths[string(fields[i])] = path
			path = string(fields[i])
			i++
		}
		paths = append(paths, path)
	}
	return paths, oldPaths, nil
}

func (r Repo) commitFileDiff(hash, path, oldPath, diff string, ok bool) (string, error) {
	return r.revisionFileDiff(commitPatchPrefix, hash, path, oldPath, diff, ok)
}

func (r Repo) baseFileDiff(hash, path, oldPath, diff string, ok bool) (string, error) {
	return r.revisionFileDiff(basePatchPrefix, hash, path, oldPath, diff, ok)
}

var (
	commitPatchPrefix = []string{"show", "--first-parent", "--format="}
	basePatchPrefix   = []string{"diff"}
)

func (r Repo) revisionFileDiff(prefix []string, hash, path, oldPath, diff string, ok bool) (string, error) {
	args := func(withPrefix bool) []string {
		full := append(append([]string{}, prefix...), r.diffOptions()...)
		if withPrefix {
			full = append(full, "--src-prefix=a/", "--dst-prefix=b/")
		}
		full = append(full, hash, "--", path)
		if oldPath != "" {
			full = append(full, oldPath)
		}
		return full
	}
	if !ok {
		out, err := r.CommandStdout(args(true)...)
		if err != nil {
			return "", CommandError(out, err)
		}
		diff = string(out)
	}
	if strings.TrimSpace(diff) == "" {
		diff = "No diff available for this file."
		if r.IgnoreSpace {
			original := r
			original.IgnoreSpace = false
			out, err := original.CommandStdout(args(false)...)
			if err != nil {
				return "", CommandError(out, err)
			}
			if len(out) > 0 {
				diff = "Whitespace-only changes"
			}
		}
	}
	return diff, nil
}

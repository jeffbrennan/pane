package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type commitInfo struct {
	hash, author, email, date, subject, message string
	committer, committerEmail, committerDate    string
	refs                                        string
	parents                                     []string
	filePath                                    string
	search                                      string
	added, deleted, binary                      int
	statsLoaded                                 bool
}

const historyPage = 200

func (r repository) resolveCommit(revision string) (commitInfo, error) {
	if revision == "" {
		return commitInfo{}, fmt.Errorf("commit revision cannot be empty")
	}
	out, err := r.command("rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return commitInfo{}, fmt.Errorf("invalid commit %q: %w", revision, gitError(out, err))
	}
	hash := strings.TrimSpace(string(out))
	commit, err := r.commitDetail(hash)
	if err != nil {
		return commitInfo{}, err
	}
	if commit.hash == "" {
		return commitInfo{}, fmt.Errorf("could not read commit %q", revision)
	}
	return commit, nil
}

// commitDetail loads the metadata a commit header shows: identity, dates,
// parents, refs and the full message. It runs alongside the patch stream.
func (r repository) commitDetail(hash string) (commitInfo, error) {
	out, err := r.commandStdout("show", "-s", "--decorate=short", "-z",
		"--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00%P%x00%cn%x00%ce%x00%cI%x00%D%x00", hash, "--")
	if err != nil {
		return commitInfo{}, gitError(out, err)
	}
	return parseCommitDetail(out)
}

func parseCommitDetail(out []byte) (commitInfo, error) {
	fields := bytes.Split(out, []byte{0})
	if len(fields) < 12 {
		return commitInfo{}, fmt.Errorf("unexpected Git commit detail format")
	}
	commit := commitInfo{
		hash: string(fields[1]), author: string(fields[2]), email: string(fields[3]),
		date: string(fields[4]), subject: string(fields[5]), message: string(fields[6]),
		committer: string(fields[8]), committerEmail: string(fields[9]), committerDate: string(fields[10]),
		refs: string(fields[11]),
	}
	if parents := strings.Fields(string(fields[7])); len(parents) > 0 {
		commit.parents = parents
	}
	commit.search = strings.ToLower(commit.hash + "\n" + commit.author + "\n" + commit.email + "\n" + commit.message + "\n" + commit.subject)
	return commit, nil
}

// history loads up to limit commits after skipping the newest skip commits.
func (r repository) history(skip, limit int) ([]commitInfo, error) {
	return r.historyWithDate(skip, limit, "%aI")
}

func (r repository) historyWithDate(skip, limit int, dateFormat string) ([]commitInfo, error) {
	return r.historyWithPath(skip, limit, dateFormat, "")
}

func (r repository) historyWithPath(skip, limit int, dateFormat, path string) ([]commitInfo, error) {
	out, err := r.command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, gitError(out, err)
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
	out, err = r.commandStdout(args...)
	if err != nil {
		return nil, gitError(out, err)
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

func parseFileHistory(out []byte, path string) ([]commitInfo, error) {
	fields := bytes.Split(out, []byte{0})
	var commits []commitInfo
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
		c := commitInfo{hash: string(fields[i]), author: string(fields[i+1]), email: string(fields[i+2]), date: string(fields[i+3]), subject: string(fields[i+4]), message: string(fields[i+5]), filePath: path}
		c.search = strings.ToLower(c.hash + "\n" + c.author + "\n" + c.email + "\n" + c.message + "\n" + c.subject)
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
			c.filePath = string(fields[i])
			i++
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				if i >= len(fields) || len(fields[i]) == 0 {
					return nil, fmt.Errorf("unexpected Git file history rename")
				}
				c.filePath = string(fields[i])
				i++
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func (r repository) historyStats(hashes []string) ([]commitInfo, error) {
	args := []string{"log", "--no-walk=unsorted", "-z", "--numstat", "-M", "--no-ext-diff", "--no-textconv", "--diff-merges=first-parent", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00"}
	args = append(args, hashes...)
	args = append(args, "--")
	out, err := r.commandStdout(args...)
	if err != nil {
		return nil, gitError(out, err)
	}
	commits, err := parseHistory(out, true)
	if err == nil {
		r.stats.put(commits)
	}
	return commits, err
}

func parseHistory(out []byte, statsLoaded bool) ([]commitInfo, error) {
	fields := bytes.Split(out, []byte{0})
	var commits []commitInfo
	for i := 0; i < len(fields); {
		if len(fields[i]) == 0 {
			i++
			continue
		}
		if i+6 > len(fields) {
			return nil, fmt.Errorf("unexpected Git history format")
		}
		c := commitInfo{hash: string(fields[i]), author: string(fields[i+1]), email: string(fields[i+2]), date: string(fields[i+3]), subject: string(fields[i+4]), message: string(fields[i+5]), statsLoaded: statsLoaded}
		c.search = strings.ToLower(c.hash + "\n" + c.author + "\n" + c.email + "\n" + c.message + "\n" + c.subject)
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
				c.binary++
			} else {
				added, addErr := strconv.Atoi(string(parts[0]))
				deleted, delErr := strconv.Atoi(string(parts[1]))
				if addErr != nil || delErr != nil {
					return nil, fmt.Errorf("unexpected Git history statistics")
				}
				c.added += added
				c.deleted += deleted
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

func (r repository) commitChanges(hash string) ([]change, error) {
	changes, err := r.commitChangesWithoutSyntax(hash)
	if err == nil {
		r.loadSyntaxDiffs(changes, contextKey{commit: hash})
	}
	return changes, err
}

// commitChangesFirstScreen loads a commit's changes with the files on its
// first screen highlighted, so opening it paints in color. The first screen
// starts at focus when a change has that path. Later files start plain for
// loadCommitSyntax, or in a commit over the syntax budget for syntaxDemand;
// see startupSyntaxLines.
func (r repository) commitChangesFirstScreen(hash, focus string) ([]change, error) {
	changes, err := r.commitChangesWithoutSyntax(hash)
	if err != nil {
		return changes, err
	}
	start := 0
	for i, c := range changes {
		if focus != "" && (c.path == focus || c.oldPath == focus) {
			start = i
			break
		}
	}
	end, shown := start, 0
	for ; end < len(changes) && shown < startupSyntaxLines; end++ {
		shown += strings.Count(changes[end].diff, "\n")
	}
	first := changes[start:end]
	r.loadSyntaxDiffs(first, contextKey{commit: hash})
	warmSyntax(first)
	// Past the first screen, a commit over the syntax budget highlights files
	// as they near the viewport instead of all at once.
	rest := append(append([]change(nil), changes[:start]...), changes[end:]...)
	if syntaxBudgetEnd(rest) < len(rest) {
		markSyntaxSkipped(changes[:start], 0)
		markSyntaxSkipped(changes[end:], 0)
	}
	return changes, nil
}

func (r repository) commitChangesWithoutSyntax(hash string) ([]change, error) {
	// The patch does not depend on the name status, so both load at once.
	type result struct {
		out []byte
		err error
	}
	patchResult := make(chan result, 1)
	go func() {
		args := append([]string{"show", "--first-parent", "--format="}, r.diffOptions()...)
		args = append(args, "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
		out, err := r.commandStdout(args...)
		patchResult <- result{out, err}
	}()
	out, err := r.commandStdout("show", "--first-parent", "--format=", "--name-status", "-M", "-z", hash, "--")
	loadedPatch := <-patchResult
	if r.context != nil && r.context.Err() != nil {
		return nil, r.context.Err()
	}
	if err != nil {
		return nil, gitError(out, err)
	}
	patch, err := loadedPatch.out, loadedPatch.err
	if err != nil {
		return nil, gitError(patch, err)
	}
	patches := splitPatch(string(patch))
	paths, oldPaths, err := parseCommitPaths(out)
	if err != nil {
		return nil, err
	}
	changes, err := parallelDiffs(paths, func(path string) (string, error) {
		if r.context != nil && r.context.Err() != nil {
			return "", r.context.Err()
		}
		diff, ok := patches[path]
		return r.commitFileDiff(hash, path, oldPaths[path], diff, ok)
	})
	for i := range changes {
		changes[i].oldPath = oldPaths[changes[i].path]
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

func (r repository) commitFileDiff(hash, path, oldPath, diff string, ok bool) (string, error) {
	return r.revisionFileDiff(commitPatchPrefix, hash, path, oldPath, diff, ok)
}

func (r repository) baseFileDiff(hash, path, oldPath, diff string, ok bool) (string, error) {
	return r.revisionFileDiff(basePatchPrefix, hash, path, oldPath, diff, ok)
}

var (
	commitPatchPrefix = []string{"show", "--first-parent", "--format="}
	basePatchPrefix   = []string{"diff"}
)

func (r repository) revisionFileDiff(prefix []string, hash, path, oldPath, diff string, ok bool) (string, error) {
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
		out, err := r.commandStdout(args(true)...)
		if err != nil {
			return "", gitError(out, err)
		}
		diff = string(out)
	}
	if strings.TrimSpace(diff) == "" {
		diff = "No diff available for this file."
		if r.ignoreSpace {
			original := r
			original.ignoreSpace = false
			out, err := original.commandStdout(args(false)...)
			if err != nil {
				return "", gitError(out, err)
			}
			if len(out) > 0 {
				diff = "Whitespace-only changes"
			}
		}
	}
	return diff, nil
}

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
	out, err = r.command("log", "-1", "-z", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00", hash, "--")
	if err != nil {
		return commitInfo{}, gitError(out, err)
	}
	commits, err := parseHistory(out, false)
	if err != nil {
		return commitInfo{}, err
	}
	if len(commits) != 1 {
		return commitInfo{}, fmt.Errorf("could not read commit %q", revision)
	}
	return commits[0], nil
}

// history loads up to limit commits after skipping the newest skip commits.
func (r repository) history(skip, limit int) ([]commitInfo, error) {
	return r.historyWithDate(skip, limit, "%aI")
}

func (r repository) historyWithDate(skip, limit int, dateFormat string) ([]commitInfo, error) {
	out, err := r.command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, gitError(out, err)
	}
	format := "--format=%x00%H%x00%an%x00%ae%x00" + dateFormat + "%x00%s%x00%B%x00"
	out, err = r.command("log", fmt.Sprintf("--skip=%d", skip), fmt.Sprintf("--max-count=%d", limit), "-z", format)
	if err != nil {
		return nil, gitError(out, err)
	}
	return parseHistory(out, false)
}

func (r repository) historyStats(hashes []string) ([]commitInfo, error) {
	args := []string{"log", "--no-walk=unsorted", "-z", "--numstat", "-M", "--no-ext-diff", "--no-textconv", "--diff-merges=first-parent", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00"}
	args = append(args, hashes...)
	args = append(args, "--")
	out, err := r.command(args...)
	if err != nil {
		return nil, gitError(out, err)
	}
	return parseHistory(out, true)
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

func (r repository) commitChangesWithoutSyntax(hash string) ([]change, error) {
	out, err := r.command("show", "--first-parent", "--format=", "--name-status", "-M", "-z", hash, "--")
	if err != nil {
		return nil, gitError(out, err)
	}
	patch, err := r.command("show", "--first-parent", "--format=", "--no-ext-diff", "--no-textconv", "--no-color", "-M", "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
	if err != nil {
		return nil, gitError(patch, err)
	}
	patches := splitPatch(string(patch))
	var paths []string
	oldPaths := make(map[string]string)
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields) && len(fields[i]) > 0; {
		status := string(fields[i])
		i++
		if i >= len(fields) || len(fields[i]) == 0 {
			return nil, fmt.Errorf("unexpected Git change format")
		}
		path := string(fields[i])
		i++
		if strings.HasPrefix(status, "R") {
			if i >= len(fields) || len(fields[i]) == 0 {
				return nil, fmt.Errorf("unexpected Git rename format")
			}
			oldPaths[string(fields[i])] = path
			path = string(fields[i])
			i++
		}
		paths = append(paths, path)
	}
	changes, err := parallelDiffs(paths, func(path string) (string, error) {
		diff, ok := patches[path]
		if !ok {
			args := []string{"show", "--first-parent", "--format=", "--no-ext-diff", "--no-textconv", "--no-color", "-M", "--src-prefix=a/", "--dst-prefix=b/", hash, "--", path}
			if oldPaths[path] != "" {
				args = append(args, oldPaths[path])
			}
			out, err := r.command(args...)
			if err != nil {
				return "", gitError(out, err)
			}
			diff = string(out)
		}
		if strings.TrimSpace(diff) == "" {
			diff = "No diff available for this file."
		}
		return diff, nil
	})
	for i := range changes {
		changes[i].oldPath = oldPaths[changes[i].path]
	}
	return changes, err
}

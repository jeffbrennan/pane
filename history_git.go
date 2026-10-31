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
	args := []string{"log", "--no-walk=unsorted", "-z", "--numstat", "--no-renames", "--no-ext-diff", "--no-textconv", "--diff-merges=first-parent", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00"}
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
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func (r repository) commitChanges(hash string) ([]change, error) {
	out, err := r.command("show", "--first-parent", "--format=", "--name-only", "--no-renames", "-z", hash, "--")
	if err != nil {
		return nil, gitError(out, err)
	}
	patch, err := r.command("show", "--first-parent", "--format=", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
	if err != nil {
		return nil, gitError(patch, err)
	}
	patches := splitPatch(string(patch))
	var paths []string
	for _, pathBytes := range bytes.Split(out, []byte{0}) {
		if len(pathBytes) > 0 {
			paths = append(paths, string(pathBytes))
		}
	}
	return parallelDiffs(paths, func(path string) (string, error) {
		diff, ok := patches[path]
		if !ok {
			out, err := r.command("show", "--first-parent", "--format=", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", hash, "--", path)
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
}

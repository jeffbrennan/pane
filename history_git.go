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
	added, deleted, binary                      int
}

func (r repository) history() ([]commitInfo, error) {
	out, err := r.command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, gitError(out, err)
	}
	out, err = r.command("log", "-z", "--numstat", "--no-renames", "--no-ext-diff", "--no-textconv", "--diff-merges=first-parent", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00")
	if err != nil {
		return nil, gitError(out, err)
	}
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
		c := commitInfo{hash: string(fields[i]), author: string(fields[i+1]), email: string(fields[i+2]), date: string(fields[i+3]), subject: string(fields[i+4]), message: string(fields[i+5])}
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
	var changes []change
	for _, pathBytes := range bytes.Split(out, []byte{0}) {
		if len(pathBytes) == 0 {
			continue
		}
		path := string(pathBytes)
		out, err := r.command("show", "--first-parent", "--format=", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", hash, "--", path)
		if err != nil {
			return nil, gitError(out, err)
		}
		diff := string(out)
		if strings.TrimSpace(diff) == "" {
			diff = "No diff available for this file."
		}
		changes = append(changes, change{path: path, diff: diff})
	}
	return changes, nil
}

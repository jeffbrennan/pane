package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type commitInfo struct {
	hash, author, email, date, subject, message string
}

func (r repository) history() ([]commitInfo, error) {
	out, err := r.command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, gitError(out, err)
	}
	out, err = r.command("log", "-z", "--format=%H%x00%an%x00%ae%x00%aI%x00%s%x00%B")
	if err != nil {
		return nil, gitError(out, err)
	}
	fields := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	if len(fields)%6 != 0 {
		return nil, fmt.Errorf("unexpected Git history format")
	}
	commits := make([]commitInfo, 0, len(fields)/6)
	for i := 0; i < len(fields); i += 6 {
		commits = append(commits, commitInfo{string(fields[i]), string(fields[i+1]), string(fields[i+2]), string(fields[i+3]), string(fields[i+4]), string(fields[i+5])})
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

package main

import (
	"fmt"
	"strings"
	"time"
)

const commitHeaderMargin = 2

// onCommitHeader reports whether the active row belongs to the commit detail
// block, which h and l collapse and expand.
func (m model) onCommitHeader() bool {
	return m.pos >= 0 && m.pos < len(m.rows) && (m.rows[m.pos].kind == 'm' || m.rows[m.pos].kind == 'M')
}

// commitHeaderRows prepends the commit detail block to a commit view's rows.
// Base reviews have no single commit to describe, and sequence rows get their
// block from commitHeaderBlock per commit.
func (m model) commitHeaderRows(rows []row) []row {
	if m.contextCommit == "" || m.contextBase {
		return rows
	}
	commit := m.history.meta
	if commit.hash == "" {
		return rows
	}
	block := commitHeaderBlock(commit, m.diffContentWidth(), m.history.headerCollapsed, time.Now())
	if len(block) == 0 {
		return rows
	}
	result := make([]row, 0, len(block)+len(rows))
	result = append(result, block...)
	result = append(result, rows...)
	return result
}

// commitHeaderBlock lays out the author, dates, parents, refs and message body.
// The last row of the block is blank, which separates it from the first file.
func commitHeaderBlock(commit commitInfo, width int, collapsed bool, now time.Time) []row {
	age := relativeCommitTime(commit.date, now)
	if collapsed {
		summary := fmt.Sprintf("  %s · %s · commit details · l expand", clean(commit.author), age)
		return []row{{text: fit(summary, max(1, width)), kind: 'M'}}
	}
	var rows []row
	add := func(text string, kind byte) {
		for _, part := range wrapLine(clean(text), max(1, width)) {
			rows = append(rows, row{text: part, kind: kind})
		}
	}
	author := fmt.Sprintf("  %s <%s> · %s (%s)", commit.author, commit.email, age, absoluteCommitTime(commit.date))
	add(author, 'm')
	if commit.committer != "" && (commit.committer != commit.author || commit.committerEmail != commit.email || commit.committerDate != commit.date) {
		committer := fmt.Sprintf("  Committed by %s <%s> · %s", commit.committer, commit.committerEmail, absoluteCommitTime(commit.committerDate))
		add(committer, 'm')
	}
	if len(commit.parents) > 0 {
		parents := make([]string, len(commit.parents))
		for i, parent := range commit.parents {
			parents[i] = shortHash(parent)
		}
		text := "  Parent " + strings.Join(parents, " ")
		if len(commit.parents) > 1 {
			text += " · merge"
		}
		add(text, 'm')
	}
	if refs := formatRefs(commit.refs); refs != "" {
		add("  Refs "+refs, 'm')
	}
	body, trailers := splitCommitMessage(commit.subject, commit.message)
	if len(body) > 0 || len(trailers) > 0 {
		rows = append(rows, row{kind: 'm'})
	}
	for _, line := range body {
		add("  "+line, 'm')
	}
	for _, line := range trailers {
		add("  "+line, 'M')
	}
	rows = append(rows, row{kind: 'm'})
	return rows
}

func absoluteCommitTime(date string) string {
	when, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return date
	}
	return when.Local().Format("2006-01-02 15:04")
}

func formatRefs(refs string) string {
	return strings.TrimSpace(refs)
}

// splitCommitMessage separates the body from trailing trailers such as
// Co-authored-by, so the trailers can render dim.
func splitCommitMessage(subject, message string) ([]string, []string) {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == strings.TrimSpace(subject) {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	end := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		if isTrailerLine(line) || ((strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && i < len(lines)-1) {
			end = i
			continue
		}
		break
	}
	return lines[:end], lines[end:]
}

func isTrailerLine(line string) bool {
	key, _, ok := strings.Cut(line, ": ")
	if !ok || key == "" || len(key) > 40 || strings.ContainsAny(key, " \t") {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

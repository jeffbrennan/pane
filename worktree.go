package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type worktreeEntry struct {
	path, head, branch string
	detached           bool
	bare               bool
	locked             bool
	prunable           bool
}

func (r repository) worktrees() ([]worktreeEntry, error) {
	out, err := r.commandStdout("worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, gitError(out, err)
	}
	var entries []worktreeEntry
	var current worktreeEntry
	flush := func() {
		if current.path != "" {
			entries = append(entries, current)
		}
		current = worktreeEntry{}
	}
	for _, field := range bytes.Split(out, []byte{0}) {
		line := string(field)
		if line == "" {
			flush()
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			current.path = value
		case "HEAD":
			current.head = value
		case "branch":
			current.branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			current.detached = true
		case "bare":
			current.bare = true
		case "locked":
			current.locked = true
		case "prunable":
			current.prunable = true
		}
	}
	flush()
	return entries, nil
}

// worktreeBranches maps a branch name to the worktree that has it checked out.
func (r repository) worktreeBranches() map[string]string {
	entries, err := r.worktrees()
	if err != nil {
		return nil
	}
	branches := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.branch != "" {
			branches[entry.branch] = entry.path
		}
	}
	return branches
}

// worktreeGitDir resolves a worktree's git directory, following the .git file
// that links a secondary worktree to the common repository.
func worktreeGitDir(path string) string {
	dotGit := filepath.Join(path, ".git")
	info, err := os.Stat(dotGit)
	if err == nil && info.IsDir() {
		return dotGit
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	dir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(path, dir)
	}
	return dir
}

// statusCount reads another worktree's dirty-file count and last activity
// without changing this pane's repository. Activity is the later of the index
// and the newest changed path.
func (r repository) statusCount(path string) (int, time.Time, error) {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain", "-z", "--untracked-files=normal")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1", "GIT_PAGER=cat")
	out, err := cmd.Output()
	if err != nil {
		return 0, time.Time{}, err
	}
	var activity time.Time
	if gitDir := worktreeGitDir(path); gitDir != "" {
		if info, err := os.Stat(filepath.Join(gitDir, "index")); err == nil {
			activity = info.ModTime()
		}
	}
	count := 0
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if len(record) < 4 {
			continue
		}
		status := record[:2]
		changed := record[3:]
		count++
		if status[0] == 'R' || status[0] == 'C' {
			// A rename/copy is followed by the original path in its own field.
			i++
		}
		if info, err := os.Stat(filepath.Join(path, filepath.FromSlash(string(changed)))); err == nil && info.ModTime().After(activity) {
			activity = info.ModTime()
		}
	}
	if activity.IsZero() {
		if info, err := os.Stat(path); err == nil {
			activity = info.ModTime()
		}
	}
	return count, activity, nil
}

func commonWorktreeDir(entries []worktreeEntry) string {
	if len(entries) < 2 {
		return ""
	}
	dirs := make([]string, len(entries))
	for i, entry := range entries {
		dirs[i] = filepath.Dir(entry.path)
	}
	common := dirs[0]
	for _, dir := range dirs[1:] {
		for common != "/" && common != "." && dir != common && !strings.HasPrefix(dir, common+string(filepath.Separator)) {
			common = filepath.Dir(common)
		}
	}
	return common
}

func worktreeDisplayPath(entries []worktreeEntry, path string) string {
	if common := commonWorktreeDir(entries); common != "" && strings.HasPrefix(path, common+string(filepath.Separator)) {
		return strings.TrimPrefix(path, common+string(filepath.Separator))
	}
	return path
}

func worktreePathFor(entries []worktreeEntry, root, branch string) string {
	slug := strings.ReplaceAll(branch, "/", "-")
	if common := commonWorktreeDir(entries); common != "" {
		return filepath.Join(common, slug)
	}
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+slug)
}

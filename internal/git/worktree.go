package git

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type WorktreeEntry struct {
	Path, Head, Branch string
	Detached           bool
	Bare               bool
	Locked             bool
	Prunable           bool
}

func (r Repo) Worktrees() ([]WorktreeEntry, error) {
	out, err := r.CommandStdout("worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, GitError(out, err)
	}
	var entries []WorktreeEntry
	var current WorktreeEntry
	flush := func() {
		if current.Path != "" {
			entries = append(entries, current)
		}
		current = WorktreeEntry{}
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
			current.Path = value
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			current.Detached = true
		case "bare":
			current.Bare = true
		case "locked":
			current.Locked = true
		case "prunable":
			current.Prunable = true
		}
	}
	flush()
	return entries, nil
}

// worktreeBranches maps a branch name to the worktree that has it checked out.
func (r Repo) worktreeBranches() map[string]string {
	entries, err := r.Worktrees()
	if err != nil {
		return nil
	}
	branches := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.Branch != "" {
			branches[entry.Branch] = entry.Path
		}
	}
	return branches
}

// worktreeGitDir resolves a worktree's git directory, following the .git file
// that links a secondary worktree to the common repository.
func WorktreeGitDir(path string) string {
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

// StatusCount reads another worktree's dirty-file count and last activity
// without changing this pane's repository. Activity is the later of the index
// and the newest changed path.
func (r Repo) StatusCount(path string) (int, time.Time, error) {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain", "-z", "--untracked-files=normal")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1", "GIT_PAGER=cat")
	out, err := cmd.Output()
	if err != nil {
		return 0, time.Time{}, err
	}
	var activity time.Time
	if gitDir := WorktreeGitDir(path); gitDir != "" {
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

func CommonWorktreeDir(entries []WorktreeEntry) string {
	if len(entries) < 2 {
		return ""
	}
	dirs := make([]string, len(entries))
	for i, entry := range entries {
		dirs[i] = filepath.Dir(entry.Path)
	}
	common := dirs[0]
	for _, dir := range dirs[1:] {
		for common != "/" && common != "." && dir != common && !strings.HasPrefix(dir, common+string(filepath.Separator)) {
			common = filepath.Dir(common)
		}
	}
	return common
}

func WorktreeDisplayPath(entries []WorktreeEntry, path string) string {
	if common := CommonWorktreeDir(entries); common != "" && strings.HasPrefix(path, common+string(filepath.Separator)) {
		return strings.TrimPrefix(path, common+string(filepath.Separator))
	}
	return path
}

func WorktreePathFor(entries []WorktreeEntry, root, branch string) string {
	slug := strings.ReplaceAll(branch, "/", "-")
	if common := CommonWorktreeDir(entries); common != "" {
		return filepath.Join(common, slug)
	}
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+slug)
}

package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type change struct {
	path               string
	diff               string
	syntaxDiff         string
	conflict           bool
	untracked          bool
	untrackedHash      [32]byte
	untrackedHashValid bool
}

type snapshot struct {
	unstaged []change
	staged   []change
	branch   branchStatus
}

func (s snapshot) equal(other snapshot) bool {
	return s.branch == other.branch && slices.Equal(s.unstaged, other.unstaged) && slices.Equal(s.staged, other.staged)
}

type repository struct{ root string }

func openRepository() (repository, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return repository{}, fmt.Errorf("not inside a Git working tree")
	}
	return repository{root: strings.TrimSuffix(string(out), "\n")}, nil
}

func (r repository) command(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1", "GIT_PAGER=cat", "GIT_EDITOR=true")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return cmd.CombinedOutput()
}

func (r repository) load() (snapshot, error) {
	out, err := r.command("status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--no-renames", "--ahead-behind")
	if err != nil {
		return snapshot{}, gitError(out, err)
	}
	result := snapshot{branch: parseBranchStatus(out)}
	var stagedPaths, unstagedPaths, untrackedPaths []string
	var unstagedIndexes, untrackedIndexes []int
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		record := string(records[i])
		switch {
		case strings.HasPrefix(record, "1 "):
			fields := strings.SplitN(record, " ", 9)
			if len(fields) < 9 {
				continue
			}
			status, path := fields[1], fields[8]
			if status[0] != '.' {
				stagedPaths = append(stagedPaths, path)
			}
			if status[1] != '.' {
				unstagedPaths = append(unstagedPaths, path)
				unstagedIndexes = append(unstagedIndexes, len(result.unstaged))
				result.unstaged = append(result.unstaged, change{path: path})
			}
		case strings.HasPrefix(record, "2 "):
			i++
		case strings.HasPrefix(record, "u "):
			if fields := strings.SplitN(record, " ", 11); len(fields) == 11 {
				result.unstaged = append(result.unstaged, change{path: fields[10], diff: "Merge conflict: resolve outside pane.", conflict: true})
			}
		case strings.HasPrefix(record, "? "):
			untrackedPaths = append(untrackedPaths, record[2:])
			untrackedIndexes = append(untrackedIndexes, len(result.unstaged))
			result.unstaged = append(result.unstaged, change{path: record[2:]})
		}
	}
	if result.staged, err = r.diffs(stagedPaths, true); err != nil {
		return snapshot{}, err
	}
	unstaged, err := r.diffs(unstagedPaths, false)
	if err != nil {
		return snapshot{}, err
	}
	untracked, err := parallelDiffs(untrackedPaths, r.untrackedDiff)
	if err != nil {
		return snapshot{}, err
	}
	for i, c := range unstaged {
		result.unstaged[unstagedIndexes[i]] = c
	}
	for i, c := range untracked {
		c.untracked = true
		if strings.Contains(c.diff, "Binary files ") {
			c.untrackedHash, c.untrackedHashValid = untrackedDigest(filepath.Join(r.root, c.path))
		}
		result.unstaged[untrackedIndexes[i]] = c
	}
	r.loadSyntaxDiffs(result.unstaged, contextKey{})
	r.loadSyntaxDiffs(result.staged, contextKey{staged: true})
	return result, nil
}

func untrackedDigest(path string) ([32]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return [32]byte{}, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return sha256.Sum256([]byte(target)), err == nil
	}
	if !info.Mode().IsRegular() {
		return [32]byte{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, false
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [32]byte{}, false
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result, true
}

// diffs loads every path's diff with a single git process, falling back to
// per-file commands for paths whose patch header Git had to quote.
func (r repository) diffs(paths []string, staged bool) ([]change, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/"}
	if staged {
		args = append(args, "--cached")
	}
	out, err := r.command(args...)
	if err != nil {
		return nil, gitError(out, err)
	}
	patches := splitPatch(string(out))
	return parallelDiffs(paths, func(path string) (string, error) {
		if diff, ok := patches[path]; ok {
			return diff, nil
		}
		return r.diff(path, staged)
	})
}

// splitPatch divides a multi-file patch at its "diff" headers. Paths that Git
// quoted, and combined conflict diffs, are omitted so callers can load them individually.
func splitPatch(patch string) map[string]string {
	patches := map[string]string{}
	var path string
	var section strings.Builder
	flush := func() {
		if path != "" {
			patches[path] += section.String()
		}
		section.Reset()
	}
	for len(patch) > 0 {
		end := strings.IndexByte(patch, '\n') + 1
		if end == 0 {
			end = len(patch)
		}
		line := patch[:end]
		patch = patch[end:]
		if strings.HasPrefix(line, "diff ") {
			flush()
			path = patchPath(strings.TrimSuffix(line, "\n"))
		}
		section.WriteString(line)
	}
	flush()
	return patches
}

func patchPath(header string) string {
	rest, ok := strings.CutPrefix(header, "diff --git a/")
	if !ok || (len(rest)-3)%2 != 0 {
		return ""
	}
	n := (len(rest) - 3) / 2
	if n <= 0 || rest[n:n+3] != " b/" || rest[:n] != rest[n+3:] {
		return ""
	}
	return rest[:n]
}

// parallelDiffs runs load for each path with bounded concurrency, keeping the
// input order. Cheap lookups return immediately; only fallbacks spawn Git.
func parallelDiffs(paths []string, load func(string) (string, error)) ([]change, error) {
	changes := make([]change, len(paths))
	errs := make([]error, len(paths))
	limit := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		limit <- struct{}{}
		go func(i int, path string) {
			defer func() { <-limit; wg.Done() }()
			diff, err := load(path)
			changes[i], errs[i] = change{path: path, diff: diff}, err
		}(i, path)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return changes, nil
}

func (r repository) diff(path string, staged bool) (string, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := r.command(args...)
	if err != nil {
		return "", gitError(out, err)
	}
	if len(out) == 0 {
		return "No diff available (possibly a conflict or submodule change).", nil
	}
	return string(out), nil
}

func (r repository) untrackedDiff(path string) (string, error) {
	out, err := r.command("diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", os.DevNull, path)
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", gitError(out, err)
		}
	}
	return string(out), nil
}

func (r repository) stage(path string, staged bool) error {
	var out []byte
	var err error
	if !staged {
		out, err = r.command("add", "-A", "--", path)
	} else if _, headErr := r.command("rev-parse", "--verify", "HEAD"); headErr != nil {
		out, err = r.command("rm", "--cached", "-r", "--ignore-unmatch", "--", path)
	} else {
		out, err = r.command("restore", "--staged", "--", path)
	}
	if err != nil {
		return gitError(out, err)
	}
	return nil
}

func (r repository) action(args ...string) (string, error) {
	out, err := r.command(args...)
	if err != nil {
		return "", gitError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r repository) commit(message string) (string, error) {
	out, err := r.command("diff", "--cached", "--quiet", "--exit-code")
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 {
			return "", gitError(out, err)
		}
	} else {
		conflicts, conflictErr := r.command("diff", "--name-only", "--diff-filter=U", "-z")
		if conflictErr != nil {
			return "", gitError(conflicts, conflictErr)
		}
		if len(conflicts) > 0 {
			return "", fmt.Errorf("resolve merge conflicts outside pane before committing")
		}
		out, err = r.command("add", "--all")
		if err != nil {
			return "", gitError(out, err)
		}
	}
	return r.action("commit", "-m", message)
}

func gitError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}

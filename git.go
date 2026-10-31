package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type change struct {
	path       string
	oldPath    string
	diff       string
	syntaxDiff string
	// syntaxSkipped marks a file the syntax budget left plain; it is
	// highlighted once it scrolls into view. See syntaxDemand.
	syntaxSkipped      bool
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

type repository struct {
	root        string
	commandHook func([]string)
	context     context.Context
	ignoreSpace bool
	stats       *statsStore
	// untrackedCache turns on Git's untracked cache for status, which
	// otherwise walks every directory for untracked files. It is set only
	// when the repository leaves core.untrackedCache unset.
	untrackedCache bool
	// untrackedHidden is set when status.showUntrackedFiles hides untracked
	// files; see statusOutput.
	untrackedHidden bool
}

func (r repository) diffOptions() []string {
	args := []string{"--no-ext-diff", "--no-textconv", "--no-color", "--submodule=log", "-M"}
	if r.ignoreSpace {
		args = append(args, "-w")
	}
	return args
}

func openRepository() (repository, error) {
	// Status settings are read alongside the root, so they cost no extra
	// wait.
	settings := make(chan repository, 1)
	go func() {
		out, _ := exec.Command("git", "config", "--get-regexp", `^(core\.untrackedcache|status\.showuntrackedfiles)$`).Output()
		settings <- statusSettings(string(out))
	}()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return repository{}, fmt.Errorf("not inside a Git working tree")
	}
	root := strings.TrimSuffix(string(out), "\n")
	r := <-settings
	r.root, r.stats = root, newStatsStore(root)
	return r, nil
}

// statusSettings reads core.untrackedCache and status.showUntrackedFiles
// from git config --get-regexp output.
func statusSettings(config string) repository {
	r := repository{untrackedCache: true}
	for _, line := range strings.Split(config, "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "core.untrackedcache":
			r.untrackedCache = false
		case "status.showuntrackedfiles":
			switch strings.ToLower(value) {
			case "no", "false", "off", "0":
				r.untrackedHidden = true
			default:
				r.untrackedHidden = false
			}
		}
	}
	return r
}

func (r repository) command(args ...string) ([]byte, error) {
	return r.commandInput(nil, args...)
}

func (r repository) commandStdout(args ...string) ([]byte, error) {
	cmd := r.gitCommand(args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, gitError(stderr.Bytes(), err)
	}
	return out, nil
}

func (r repository) commandInput(input io.Reader, args ...string) ([]byte, error) {
	cmd := r.gitCommand(args...)
	cmd.Stdin = input
	return cmd.CombinedOutput()
}

func (r repository) gitCommand(args ...string) *exec.Cmd {
	if r.commandHook != nil {
		r.commandHook(args)
	}
	ctx := r.context
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LITERAL_PATHSPECS=1", "GIT_PAGER=cat", "GIT_EDITOR=true")
	if len(args) > 0 && args[0] == "stash" {
		// Git stash uses its own magic pathspecs when saving untracked files.
		cmd.Env = append(cmd.Env, "GIT_LITERAL_PATHSPECS=0")
	}
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return cmd
}

func (r repository) applyPatch(patch string, args ...string) error {
	args = append([]string{"apply"}, args...)
	args = append(args, "--whitespace=nowarn", "-")
	out, err := r.commandInput(strings.NewReader(patch), args...)
	if err != nil {
		return gitError(out, err)
	}
	return nil
}

func (r repository) load() (snapshot, error) {
	cache, _, err := r.loadCached(nil)
	if err != nil {
		return snapshot{}, err
	}
	return cache.snapshot, nil
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
	return r.diffsForChanges(changesForPaths(paths), staged)
}

func changesForPaths(paths []string) []change {
	changes := make([]change, len(paths))
	for i, path := range paths {
		changes[i].path = path
	}
	return changes
}

func (r repository) diffsForChanges(input []change, staged bool) ([]change, error) {
	if len(input) == 0 {
		return nil, nil
	}
	args := append([]string{"diff"}, r.diffOptions()...)
	args = append(args, "--src-prefix=a/", "--dst-prefix=b/")
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	var paths []string
	byPath := make(map[string]change, len(input))
	for _, c := range input {
		paths = append(paths, c.path)
		byPath[c.path] = c
		args = append(args, c.path)
		if c.oldPath != "" {
			args = append(args, c.oldPath)
		}
	}
	out, err := r.command(args...)
	if err != nil {
		return nil, gitError(out, err)
	}
	patches := splitPatch(string(out))
	changes, err := parallelDiffs(paths, func(path string) (string, error) {
		if diff, ok := patches[path]; ok {
			return diff, nil
		}
		c := byPath[path]
		return r.diffPaths(c.path, c.oldPath, staged)
	})
	for i := range changes {
		changes[i].oldPath = input[i].oldPath
	}
	return changes, err
}

// splitPatch divides a multi-file patch at its "diff" headers. Paths that Git
// quoted, and combined conflict diffs, are omitted so callers can load them individually.
func splitPatch(patch string) map[string]string {
	patches := map[string]string{}
	var path string
	start := 0
	// Sections are slices of patch, and only their headers are scanned, so
	// splitting a large patch copies nothing.
	flush := func(end int) {
		section := patch[start:end]
		for rest := section; rest != ""; {
			line, next, _ := strings.Cut(rest, "\n")
			rest = next
			if strings.HasPrefix(line, "@@ ") {
				break
			}
			if value, ok := strings.CutPrefix(line, "rename to "); ok {
				if strings.HasPrefix(value, `"`) {
					decoded, err := strconv.Unquote(value)
					if err != nil {
						path = ""
						break
					}
					value = decoded
				}
				path = value
				break
			}
		}
		if path != "" {
			patches[path] += section
		}
		start = end
	}
	for offset := 0; offset < len(patch); {
		end := strings.IndexByte(patch[offset:], '\n') + 1
		if end == 0 {
			end = len(patch) - offset
		}
		line := strings.TrimSuffix(patch[offset:offset+end], "\n")
		if strings.HasPrefix(line, "diff ") {
			flush(offset)
			path = patchPath(line)
		} else if submodulePath, _, ok := submoduleLine(line); ok {
			if path != submodulePath {
				flush(offset)
				path = submodulePath
			}
		}
		offset += end
	}
	flush(len(patch))
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
	return r.diffPaths(path, "", staged)
}

func (r repository) diffPaths(path, oldPath string, staged bool) (string, error) {
	args := append([]string{"diff"}, r.diffOptions()...)
	args = append(args, "--src-prefix=a/", "--dst-prefix=b/")
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	if oldPath != "" {
		args = append(args, oldPath)
	}
	out, err := r.command(args...)
	if err != nil {
		return "", gitError(out, err)
	}
	if len(out) == 0 {
		if r.ignoreSpace {
			original := r
			original.ignoreSpace = false
			unfiltered, err := original.diffPaths(path, oldPath, staged)
			if err != nil {
				return "", err
			}
			if strings.HasPrefix(unfiltered, "diff --git ") {
				return "Whitespace-only changes", nil
			}
			return unfiltered, nil
		}
		return "No diff available (possibly a conflict or submodule change).", nil
	}
	return string(out), nil
}

func (r repository) untrackedDiff(path string) (string, error) {
	args := append([]string{"diff", "--no-index"}, r.diffOptions()...)
	args = append(args, "--", os.DevNull, path)
	out, err := r.command(args...)
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", gitError(out, err)
		}
	}
	return string(out), nil
}

func (r repository) stage(path string, staged bool) error {
	return r.stageChange(change{path: path}, staged)
}

func (r repository) stageChange(c change, staged bool) error {
	paths := []string{c.path}
	if c.oldPath != "" {
		if staged {
			out, err := r.command("ls-files", "-z", "--", c.oldPath)
			if err != nil {
				return gitError(out, err)
			}
			if len(out) > 0 {
				return fmt.Errorf("rename source has separate staged changes; unstage it outside pane")
			}
		}
		paths = append(paths, c.oldPath)
	}
	var out []byte
	var err error
	if !staged {
		out, err = r.command(append([]string{"add", "-A", "--"}, paths...)...)
	} else if _, headErr := r.command("rev-parse", "--verify", "HEAD"); headErr != nil {
		out, err = r.command(append([]string{"rm", "--cached", "-r", "--ignore-unmatch", "--"}, paths...)...)
	} else {
		out, err = r.command(append([]string{"restore", "--staged", "--"}, paths...)...)
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
	out, err = r.commandInput(strings.NewReader(message), "commit", "--file=-")
	if err != nil {
		return "", gitError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}

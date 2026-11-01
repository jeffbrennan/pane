package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Change struct {
	Path       string
	OldPath    string
	Diff       string
	SyntaxDiff string
	// SyntaxSkipped marks a file the syntax budget left plain; it is
	// highlighted once it scrolls into view. See syntaxDemand.
	SyntaxSkipped      bool
	Generated          bool
	Conflict           bool
	Untracked          bool
	UntrackedHash      [32]byte
	UntrackedHashValid bool
}

type Snapshot struct {
	Unstaged []Change
	Staged   []Change
	Branch   BranchStatus
}

func (s Snapshot) Equal(other Snapshot) bool {
	return s.Branch == other.Branch && slices.Equal(s.Unstaged, other.Unstaged) && slices.Equal(s.Staged, other.Staged)
}

type Repo struct {
	Root        string
	CommandHook func([]string)
	Context     context.Context
	IgnoreSpace bool
	Stats       *StatsStore
	// UntrackedCache turns on Git's untracked cache for status, which
	// otherwise walks every directory for untracked files. It is set only
	// when the repository leaves core.UntrackedCache unset.
	UntrackedCache bool
	// UntrackedHidden is set when status.showUntrackedFiles hides untracked
	// files; see statusOutput.
	UntrackedHidden bool
}

func (r Repo) diffOptions() []string {
	args := []string{"--no-ext-diff", "--no-textconv", "--no-color", "--submodule=log", "-M"}
	if r.IgnoreSpace {
		args = append(args, "-w")
	}
	return args
}

func OpenRepository() (Repo, error) {
	// Status settings are read alongside the root, so they cost no extra
	// wait.
	settings := make(chan Repo, 1)
	go func() {
		out, _ := exec.Command("git", "config", "--get-regexp", `^(core\.untrackedcache|status\.showuntrackedfiles)$`).Output()
		settings <- statusSettings(string(out))
	}()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return Repo{}, fmt.Errorf("not inside a Git working tree")
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return Repo{}, fmt.Errorf("not inside a Git working tree")
	}
	root := lines[0]
	commonDir, err := filepath.Abs(lines[1])
	if err != nil {
		commonDir = lines[1]
	}
	r := <-settings
	// Commit counts are the same in every worktree, so share the cache by the
	// repository's common directory instead of the worktree root.
	r.Root, r.Stats = root, newStatsStore(commonDir)
	return r, nil
}

// statusSettings reads core.UntrackedCache and status.showUntrackedFiles
// from git config --get-regexp output.
func statusSettings(config string) Repo {
	r := Repo{UntrackedCache: true}
	for _, line := range strings.Split(config, "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "core.untrackedcache":
			r.UntrackedCache = false
		case "status.showuntrackedfiles":
			switch strings.ToLower(value) {
			case "no", "false", "off", "0":
				r.UntrackedHidden = true
			default:
				r.UntrackedHidden = false
			}
		}
	}
	return r
}

func (r Repo) Command(args ...string) ([]byte, error) {
	return r.commandInput(nil, args...)
}

func (r Repo) CommandStdout(args ...string) ([]byte, error) {
	cmd := r.gitCommand(args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, CommandError(stderr.Bytes(), err)
	}
	return out, nil
}

func (r Repo) commandInput(input io.Reader, args ...string) ([]byte, error) {
	cmd := r.gitCommand(args...)
	cmd.Stdin = input
	return cmd.CombinedOutput()
}

func (r Repo) gitCommand(args ...string) *exec.Cmd {
	if r.CommandHook != nil {
		r.CommandHook(args)
	}
	if path := os.Getenv("PANE_PERF_GIT_LOG"); path != "" {
		// Perf harnesses count Git work without sampling process tables.
		if file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			fmt.Fprintln(file, strings.Join(args, " "))
			file.Close()
		}
	}
	ctx := r.Context
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Root
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

func (r Repo) applyPatch(patch string, args ...string) error {
	args = append([]string{"apply"}, args...)
	args = append(args, "--whitespace=nowarn", "-")
	out, err := r.commandInput(strings.NewReader(patch), args...)
	if err != nil {
		return CommandError(out, err)
	}
	return nil
}

func (r Repo) Load() (Snapshot, error) {
	cache, _, err := r.LoadCached(nil)
	if err != nil {
		return Snapshot{}, err
	}
	return cache.Snapshot, nil
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
func (r Repo) diffs(paths []string, staged bool) ([]Change, error) {
	return r.diffsForChanges(changesForPaths(paths), staged)
}

func changesForPaths(paths []string) []Change {
	changes := make([]Change, len(paths))
	for i, path := range paths {
		changes[i].Path = path
	}
	return changes
}

func (r Repo) diffsForChanges(input []Change, staged bool) ([]Change, error) {
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
	byPath := make(map[string]Change, len(input))
	for _, c := range input {
		paths = append(paths, c.Path)
		byPath[c.Path] = c
		args = append(args, c.Path)
		if c.OldPath != "" {
			args = append(args, c.OldPath)
		}
	}
	out, err := r.Command(args...)
	if err != nil {
		return nil, CommandError(out, err)
	}
	patches := splitPatch(string(out))
	changes, err := parallelDiffs(paths, func(path string) (string, error) {
		if diff, ok := patches[path]; ok {
			return diff, nil
		}
		c := byPath[path]
		return r.diffPaths(c.Path, c.OldPath, staged)
	})
	for i := range changes {
		changes[i].OldPath = input[i].OldPath
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
		path = patchSectionPath(path, section)
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
		} else if submodulePath, _, ok := SubmoduleLine(line); ok {
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

func patchSectionPath(path, section string) string {
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
					return ""
				}
				value = decoded
			}
			return value
		}
	}
	return path
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
func parallelDiffs(paths []string, load func(string) (string, error)) ([]Change, error) {
	changes := make([]Change, len(paths))
	errs := make([]error, len(paths))
	limit := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		limit <- struct{}{}
		go func(i int, path string) {
			defer func() { <-limit; wg.Done() }()
			diff, err := load(path)
			changes[i], errs[i] = Change{Path: path, Diff: diff}, err
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

func (r Repo) Diff(path string, staged bool) (string, error) {
	return r.diffPaths(path, "", staged)
}

func (r Repo) diffPaths(path, oldPath string, staged bool) (string, error) {
	args := append([]string{"diff"}, r.diffOptions()...)
	args = append(args, "--src-prefix=a/", "--dst-prefix=b/")
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	if oldPath != "" {
		args = append(args, oldPath)
	}
	out, err := r.Command(args...)
	if err != nil {
		return "", CommandError(out, err)
	}
	if len(out) == 0 {
		if r.IgnoreSpace {
			original := r
			original.IgnoreSpace = false
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

func (r Repo) untrackedDiff(path string) (string, error) {
	args := append([]string{"diff", "--no-index"}, r.diffOptions()...)
	args = append(args, "--", os.DevNull, path)
	out, err := r.Command(args...)
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return "", CommandError(out, err)
		}
	}
	return string(out), nil
}

func (r Repo) Stage(path string, staged bool) error {
	return r.StageChange(Change{Path: path}, staged)
}

func (r Repo) StageChange(c Change, staged bool) error {
	paths := []string{c.Path}
	if c.OldPath != "" {
		if staged {
			out, err := r.Command("ls-files", "-z", "--", c.OldPath)
			if err != nil {
				return CommandError(out, err)
			}
			if len(out) > 0 {
				return fmt.Errorf("rename source has separate staged changes; unstage it outside pane")
			}
		}
		paths = append(paths, c.OldPath)
	}
	var out []byte
	var err error
	if !staged {
		out, err = r.Command(append([]string{"add", "-A", "--"}, paths...)...)
	} else if _, headErr := r.Command("rev-parse", "--verify", "HEAD"); headErr != nil {
		out, err = r.Command(append([]string{"rm", "--cached", "-r", "--ignore-unmatch", "--"}, paths...)...)
	} else {
		out, err = r.Command(append([]string{"restore", "--staged", "--"}, paths...)...)
	}
	if err != nil {
		return CommandError(out, err)
	}
	return nil
}

func (r Repo) Action(args ...string) (string, error) {
	out, err := r.Command(args...)
	if err != nil {
		return "", CommandError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r Repo) Commit(message string) (string, error) {
	out, err := r.Command("diff", "--cached", "--quiet", "--exit-code")
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 {
			return "", CommandError(out, err)
		}
	} else {
		conflicts, conflictErr := r.Command("diff", "--name-only", "--diff-filter=U", "-z")
		if conflictErr != nil {
			return "", CommandError(conflicts, conflictErr)
		}
		if len(conflicts) > 0 {
			return "", fmt.Errorf("resolve merge conflicts outside pane before committing")
		}
		out, err = r.Command("add", "--all")
		if err != nil {
			return "", CommandError(out, err)
		}
	}
	out, err = r.commandInput(strings.NewReader(message), "commit", "--file=-")
	if err != nil {
		return "", CommandError(out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func CommandError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}

var submoduleHeader = regexp.MustCompile(`^Submodule (.+) ([0-9a-f]+)\.\.\.?([0-9a-f]+)(?: \(([^)]*)\))?:?$`)

// SubmoduleLine parses a submodule summary line into its path and display
// text.
func SubmoduleLine(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "Submodule ") {
		return "", "", false
	}
	if match := submoduleHeader.FindStringSubmatch(line); match != nil {
		text := "Submodule " + match[2] + " → " + match[3]
		if match[4] != "" {
			text += " (" + match[4] + ")"
		}
		return match[1], text, true
	}
	for _, suffix := range []string{" contains modified content", " contains untracked content"} {
		if path, ok := strings.CutSuffix(strings.TrimPrefix(line, "Submodule "), suffix); ok {
			return path, "Submodule" + suffix, true
		}
	}
	return "", "", false
}

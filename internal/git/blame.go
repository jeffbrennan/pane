package git

import (
	"fmt"
	"strconv"
	"strings"
)

// Blame resolves the commit and final line number for a line of path at
// hash, falling back to the commit itself for a root commit.
func (r Repo) Blame(hash, path string, line int) (string, int, error) {
	args := []string{"blame", "-L", fmt.Sprintf("%d,%d", line, line), "--porcelain", hash + "^", "--", path}
	out, err := r.Command(args...)
	if err != nil {
		// A root commit has no parent to blame.
		args[4] = hash
		out, err = r.Command(args...)
	}
	if err != nil {
		return "", 0, CommandError(out, err)
	}
	fields := strings.Fields(strings.SplitN(string(out), "\n", 2)[0])
	if len(fields) < 3 {
		return "", 0, fmt.Errorf("unexpected git blame output")
	}
	blamed := strings.TrimPrefix(fields[0], "^")
	final, err := strconv.Atoi(fields[2])
	if err != nil {
		final = line
	}
	return blamed, final, nil
}

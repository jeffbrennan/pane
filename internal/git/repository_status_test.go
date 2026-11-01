package git

import (
	"strings"
	"testing"
)

func TestBranchStatusTracksAheadAndBehind(t *testing.T) {
	r := testRepo(t)
	s, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Branch.Available || s.Branch.Name == "" || s.Branch.Upstream != "" || s.Branch.CountsKnown {
		t.Fatalf("unborn branch: %#v", s.Branch)
	}
	writeFile(t, r, "file", "base\n")
	if _, err := r.Commit("base"); err != nil {
		t.Fatal(err)
	}
	branchBytes, err := r.Command("symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(string(branchBytes))
	baseBytes, err := r.Command("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(baseBytes))
	if _, err := r.Action("remote", "add", "origin", r.Root); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("update-ref", "refs/remotes/origin/"+branch, base); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("branch", "--set-upstream-to=origin/"+branch, branch); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "file", "local\n")
	if _, err := r.Commit("local"); err != nil {
		t.Fatal(err)
	}
	treeBytes, err := r.Command("rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	remoteBytes, err := r.Command("commit-tree", strings.TrimSpace(string(treeBytes)), "-p", base, "-m", "remote")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("update-ref", "refs/remotes/origin/"+branch, strings.TrimSpace(string(remoteBytes))); err != nil {
		t.Fatal(err)
	}
	s, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch.Name != branch || s.Branch.Upstream != "origin/"+branch || !s.Branch.CountsKnown || s.Branch.Ahead != 1 || s.Branch.Behind != 1 {
		t.Fatalf("divergent branch: %#v", s.Branch)
	}
}

func TestParseBranchStatusWithoutCounts(t *testing.T) {
	status := parseBranchStatus([]byte("# branch.head main\x00# branch.upstream origin/main\x00"))
	if !status.Available || status.Upstream != "origin/main" || status.CountsKnown {
		t.Fatalf("status: %#v", status)
	}
}

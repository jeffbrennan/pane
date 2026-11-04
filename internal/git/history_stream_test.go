package git

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestHistoryStreamDeepPaging(t *testing.T) {
	r := testRepo(t)
	var input strings.Builder
	for i := 0; i < 2500; i++ {
		fmt.Fprintf(&input, "commit refs/heads/history-test\nmark :%d\ncommitter Test <test@example.com> %d +0000\ndata %d\ncommit %d\n", i+1, i+1, len(fmt.Sprintf("commit %d", i)), i)
	}
	if out, err := r.commandInput(strings.NewReader(input.String()), "fast-import", "--quiet"); err != nil {
		t.Fatalf("fast-import: %s: %v", out, err)
	}
	if out, err := r.Command("checkout", "history-test"); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	var logs int
	r.CommandHook = func(args []string) {
		if args[0] == "log" {
			logs++
			for _, arg := range args {
				if strings.HasPrefix(arg, "--skip=") {
					t.Errorf("stream used %s", arg)
				}
			}
		}
	}
	s := NewHistoryStream(r, "")
	defer s.Close()
	var commits []CommitInfo
	for {
		page, err := s.NextPage(HistoryPage)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, page...)
		if len(page) < HistoryPage {
			break
		}
	}
	if len(commits) != 2500 || logs != 1 {
		t.Fatalf("commits=%d log processes=%d", len(commits), logs)
	}
	for i, commit := range commits {
		if want := fmt.Sprintf("commit %d", 2499-i); commit.Subject != want {
			t.Fatalf("commit %d: got %q, want %q", i, commit.Subject, want)
		}
	}
	if !s.reaped() {
		t.Fatal("completed process was not reaped")
	}
}

func TestHistoryStreamFileRenameAcrossPages(t *testing.T) {
	r := fileHistoryFixture(t)
	s := NewHistoryStream(r, "renamed")
	defer s.Close()
	for _, want := range []struct{ subject, path string }{{"latest", "renamed"}, {"rename", "renamed"}, {"second", "old name"}, {"first", "old name"}} {
		page, err := s.NextPage(1)
		if err != nil || len(page) != 1 || page[0].Subject != want.subject || page[0].FilePath != want.path {
			t.Fatalf("page: %#v, err=%v, want %#v", page, err, want)
		}
	}
	page, err := s.NextPage(1)
	if err != nil || len(page) != 0 {
		t.Fatalf("end: %#v, %v", page, err)
	}
}

func TestHistoryStreamMergeOrder(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base", "base")
	if _, err := r.Commit("base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("checkout", "-b", "side"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "side", "side")
	if _, err := r.Commit("side"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("checkout", "-"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "main", "main")
	if _, err := r.Commit("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("merge", "--no-ff", "-m", "merge", "side"); err != nil {
		t.Fatal(err)
	}
	want, err := r.History(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	s := NewHistoryStream(r, "")
	defer s.Close()
	for i := range want {
		page, err := s.NextPage(1)
		if err != nil || len(page) != 1 || page[0].Hash != want[i].Hash {
			t.Fatalf("page %d: %#v, %v", i, page, err)
		}
	}
}

func TestHistoryStreamCloseDuringRead(t *testing.T) {
	r := fileHistoryFixture(t)
	s := NewHistoryStream(r, "")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = s.NextPage(1000)
	}()
	s.Close()
	wg.Wait()
	if s.running() && !s.reaped() {
		t.Fatal("closed process was not reaped")
	}
}

func TestHistoryStreamEmptyRepository(t *testing.T) {
	s := NewHistoryStream(testRepo(t), "")
	defer s.Close()
	page, err := s.NextPage(1)
	if err != nil || len(page) != 0 {
		t.Fatalf("empty repository: %#v, %v", page, err)
	}
}

func TestHistoryStreamClosesBlockedProcess(t *testing.T) {
	r := testRepo(t)
	body := strings.Repeat("x", 1024*1024)
	input := fmt.Sprintf("commit refs/heads/stream-test\ncommitter Test <test@example.com> 1 +0000\ndata %d\n%s\ncommit refs/heads/stream-test\ncommitter Test <test@example.com> 2 +0000\ndata 6\nnewest\n", len(body), body)
	if out, err := r.commandInput(strings.NewReader(input), "fast-import", "--quiet"); err != nil {
		t.Fatalf("fast-import: %s: %v", out, err)
	}
	if out, err := r.Command("checkout", "stream-test"); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	s := NewHistoryStream(r, "")
	page, err := s.NextPage(1)
	if err != nil || len(page) != 1 || page[0].Subject != "newest" {
		t.Fatalf("first page: %#v, %v", page, err)
	}
	s.Close()
	if s.running() && !s.reaped() {
		t.Fatal("closing a blocked process did not reap it")
	}
}

func TestHistoryStreamFirstParentCollapsesMerges(t *testing.T) {
	r := testRepo(t)
	commitFile(t, r, "a", "a\n", "base")
	for _, args := range [][]string{{"checkout", "-q", "-b", "feature"}} {
		if out, err := r.Command(args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	commitFile(t, r, "b", "b\n", "feature work")
	for _, args := range [][]string{{"checkout", "-q", "-"}, {"merge", "-q", "--no-ff", "-m", "merge feature", "feature"}} {
		if out, err := r.Command(args...); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	subjects := func(r Repo) []string {
		s := NewHistoryStream(r, "")
		defer s.Close()
		page, err := s.NextPage(HistoryPage)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range page {
			out = append(out, c.Subject)
		}
		return out
	}
	if got := subjects(r); len(got) != 3 {
		t.Fatalf("full history = %v", got)
	}
	r.FirstParent = true
	if got := strings.Join(subjects(r), ","); got != "merge feature,base" {
		t.Fatalf("first-parent history = %s", got)
	}
}

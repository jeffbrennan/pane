package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestHistoryStreamDeepPaging(t *testing.T) {
	r := testRepo(t)
	var input strings.Builder
	for i := 0; i < 2500; i++ {
		fmt.Fprintf(&input, "commit refs/heads/history-test\nmark :%d\ncommitter Test <test@example.com> %d +0000\ndata %d\ncommit %d\n", i+1, i+1, len(fmt.Sprintf("commit %d", i)), i)
	}
	if out, err := r.CommandInput(strings.NewReader(input.String()), "fast-import", "--quiet"); err != nil {
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
	s := git.NewHistoryStream(r, "")
	defer s.Close()
	var commits []git.CommitInfo
	for {
		page, err := s.NextPage(git.HistoryPage)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, page...)
		if len(page) < git.HistoryPage {
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
	if !s.Reaped() {
		t.Fatal("completed process was not reaped")
	}
}

func TestHistoryStreamFileRenameAcrossPages(t *testing.T) {
	r := fileHistoryFixture(t)
	s := git.NewHistoryStream(r, "renamed")
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
	s := git.NewHistoryStream(r, "")
	defer s.Close()
	for i := range want {
		page, err := s.NextPage(1)
		if err != nil || len(page) != 1 || page[0].Hash != want[i].Hash {
			t.Fatalf("page %d: %#v, %v", i, page, err)
		}
	}
}

func TestHistoryStreamReloadAndClose(t *testing.T) {
	r := fileHistoryFixture(t)
	m := model{repo: r, screen: "history"}
	m, cmd := m.reloadHistory()
	old := m.history.stream
	if msg := cmd().(historyLoaded); msg.err != nil {
		t.Fatal(msg.err)
	}
	m, _ = m.reloadHistory()
	if !old.Reaped() || m.history.stream == old {
		t.Fatal("reload did not close and replace the old process")
	}
	m.history.stream.Close()
	if _, err := m.history.stream.NextPage(1); err == nil {
		t.Fatal("closed stream started after cancellation")
	}
}

func TestHistoryStreamCloseDuringRead(t *testing.T) {
	r := fileHistoryFixture(t)
	s := git.NewHistoryStream(r, "")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = s.NextPage(1000)
	}()
	s.Close()
	wg.Wait()
	if s.Running() && !s.Reaped() {
		t.Fatal("closed process was not reaped")
	}
}

func TestHistoryStreamClosesBlockedProcess(t *testing.T) {
	r := testRepo(t)
	body := strings.Repeat("x", 1024*1024)
	input := fmt.Sprintf("commit refs/heads/stream-test\ncommitter Test <test@example.com> 1 +0000\ndata %d\n%s\ncommit refs/heads/stream-test\ncommitter Test <test@example.com> 2 +0000\ndata 6\nnewest\n", len(body), body)
	if out, err := r.CommandInput(strings.NewReader(input), "fast-import", "--quiet"); err != nil {
		t.Fatalf("fast-import: %s: %v", out, err)
	}
	if out, err := r.Command("checkout", "stream-test"); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	m := model{repo: r, screen: "history"}
	m, _ = m.reloadHistory()
	old := m.history.stream
	page, err := old.NextPage(1)
	if err != nil || len(page) != 1 || page[0].Subject != "newest" {
		t.Fatalf("first page: %#v, %v", page, err)
	}
	m, _ = m.reloadHistory()
	if !old.Reaped() {
		t.Fatal("reload did not reap the blocked process")
	}
	m.history.stream.Close()
}

func TestHistoryStreamEmptyRepository(t *testing.T) {
	s := git.NewHistoryStream(testRepo(t), "")
	defer s.Close()
	page, err := s.NextPage(1)
	if err != nil || len(page) != 0 {
		t.Fatalf("empty repository: %#v, %v", page, err)
	}
}

func TestHistoryStreamInterruptedPageReloadsOnReturn(t *testing.T) {
	r := fileHistoryFixture(t)
	s := git.NewHistoryStream(r, "")
	m := model{repo: r, screen: "history", history: historyState{stream: s, commits: []git.CommitInfo{{Hash: "HEAD"}}, fetching: true}}
	updated, _ := m.handleHistoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !m.history.stale || m.screen != "commit" {
		t.Fatal("in-flight page was not invalidated")
	}
	updated, cmd := m.returnToHistory()
	m = updated.(model)
	if cmd == nil || m.history.stream == s || !m.history.loading {
		t.Fatal("history was not reloaded after an interrupted page")
	}
	m.history.stream.Close()
}

func TestFileHistoryReturnInvalidatesPendingPages(t *testing.T) {
	previous := model{screen: "commit", history: historyState{request: 3}}
	m := model{screen: "history", history: historyState{path: "file", request: 4}, fileHistoryReturn: &previous}
	updated, _ := m.leaveFileHistory()
	restored := updated.(model)
	updated, _ = restored.returnToHistory()
	restored = updated.(model)
	updated, _ = restored.Update(historyLoaded{request: 4, commits: []git.CommitInfo{{Subject: "stale"}}, limit: git.HistoryPage})
	if len(updated.(model).history.commits) != 0 {
		t.Fatal("file-history page replaced the restored history")
	}
}

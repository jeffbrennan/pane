package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	if out, err := r.command("checkout", "history-test"); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	var logs int
	r.commandHook = func(args []string) {
		if args[0] == "log" {
			logs++
			for _, arg := range args {
				if strings.HasPrefix(arg, "--skip=") {
					t.Errorf("stream used %s", arg)
				}
			}
		}
	}
	s := newHistoryStream(r, "")
	defer s.close()
	var commits []commitInfo
	for {
		page, err := s.nextPage(historyPage)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, page...)
		if len(page) < historyPage {
			break
		}
	}
	if len(commits) != 2500 || logs != 1 {
		t.Fatalf("commits=%d log processes=%d", len(commits), logs)
	}
	for i, commit := range commits {
		if want := fmt.Sprintf("commit %d", 2499-i); commit.subject != want {
			t.Fatalf("commit %d: got %q, want %q", i, commit.subject, want)
		}
	}
	if s.cmd.ProcessState == nil {
		t.Fatal("completed process was not reaped")
	}
}

func TestHistoryStreamFileRenameAcrossPages(t *testing.T) {
	r := fileHistoryFixture(t)
	s := newHistoryStream(r, "renamed")
	defer s.close()
	for _, want := range []struct{ subject, path string }{{"latest", "renamed"}, {"rename", "renamed"}, {"second", "old name"}, {"first", "old name"}} {
		page, err := s.nextPage(1)
		if err != nil || len(page) != 1 || page[0].subject != want.subject || page[0].filePath != want.path {
			t.Fatalf("page: %#v, err=%v, want %#v", page, err, want)
		}
	}
	page, err := s.nextPage(1)
	if err != nil || len(page) != 0 {
		t.Fatalf("end: %#v, %v", page, err)
	}
}

func TestHistoryStreamMergeOrder(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base", "base")
	if _, err := r.commit("base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("checkout", "-b", "side"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "side", "side")
	if _, err := r.commit("side"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("checkout", "-"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "main", "main")
	if _, err := r.commit("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("merge", "--no-ff", "-m", "merge", "side"); err != nil {
		t.Fatal(err)
	}
	want, err := r.history(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	s := newHistoryStream(r, "")
	defer s.close()
	for i := range want {
		page, err := s.nextPage(1)
		if err != nil || len(page) != 1 || page[0].hash != want[i].hash {
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
	if old.cmd.ProcessState == nil || m.history.stream == old {
		t.Fatal("reload did not close and replace the old process")
	}
	m.history.stream.close()
	if _, err := m.history.stream.nextPage(1); err == nil {
		t.Fatal("closed stream started after cancellation")
	}
}

func TestHistoryStreamCloseDuringRead(t *testing.T) {
	r := fileHistoryFixture(t)
	s := newHistoryStream(r, "")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = s.nextPage(1000)
	}()
	s.close()
	wg.Wait()
	if s.cmd != nil && s.cmd.ProcessState == nil {
		t.Fatal("closed process was not reaped")
	}
}

func TestHistoryStreamClosesBlockedProcess(t *testing.T) {
	r := testRepo(t)
	body := strings.Repeat("x", 1024*1024)
	input := fmt.Sprintf("commit refs/heads/stream-test\ncommitter Test <test@example.com> 1 +0000\ndata %d\n%s\ncommit refs/heads/stream-test\ncommitter Test <test@example.com> 2 +0000\ndata 6\nnewest\n", len(body), body)
	if out, err := r.commandInput(strings.NewReader(input), "fast-import", "--quiet"); err != nil {
		t.Fatalf("fast-import: %s: %v", out, err)
	}
	if out, err := r.command("checkout", "stream-test"); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	m := model{repo: r, screen: "history"}
	m, _ = m.reloadHistory()
	old := m.history.stream
	page, err := old.nextPage(1)
	if err != nil || len(page) != 1 || page[0].subject != "newest" {
		t.Fatalf("first page: %#v, %v", page, err)
	}
	m, _ = m.reloadHistory()
	if old.cmd.ProcessState == nil {
		t.Fatal("reload did not reap the blocked process")
	}
	m.history.stream.close()
}

func TestHistoryStreamEmptyRepository(t *testing.T) {
	s := newHistoryStream(testRepo(t), "")
	defer s.close()
	page, err := s.nextPage(1)
	if err != nil || len(page) != 0 {
		t.Fatalf("empty repository: %#v, %v", page, err)
	}
}

func TestHistoryStreamInterruptedPageReloadsOnReturn(t *testing.T) {
	r := fileHistoryFixture(t)
	s := newHistoryStream(r, "")
	m := model{repo: r, screen: "history", history: historyState{stream: s, commits: []commitInfo{{hash: "HEAD"}}, fetching: true}}
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
	m.history.stream.close()
}

func TestFileHistoryReturnInvalidatesPendingPages(t *testing.T) {
	previous := model{screen: "commit", history: historyState{request: 3}}
	m := model{screen: "history", history: historyState{path: "file", request: 4}, fileHistoryReturn: &previous}
	updated, _ := m.leaveFileHistory()
	restored := updated.(model)
	updated, _ = restored.returnToHistory()
	restored = updated.(model)
	updated, _ = restored.Update(historyLoaded{request: 4, commits: []commitInfo{{subject: "stale"}}, limit: historyPage})
	if len(updated.(model).history.commits) != 0 {
		t.Fatal("file-history page replaced the restored history")
	}
}

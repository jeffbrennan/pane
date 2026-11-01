package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestHistoryStreamReloadAndClose(t *testing.T) {
	r := fileHistoryFixture(t)
	m := model{repo: r, screen: "history"}
	m, cmd := m.reloadHistory()
	old := m.history.stream
	if msg := cmd().(historyLoaded); msg.err != nil {
		t.Fatal(msg.err)
	}
	m, _ = m.reloadHistory()
	if m.history.stream == old {
		t.Fatal("reload did not replace the old process")
	}
	old.Close()
	if _, err := old.NextPage(1); err == nil {
		t.Fatal("reload did not close the old process")
	}
	m.history.stream.Close()
	if _, err := m.history.stream.NextPage(1); err == nil {
		t.Fatal("closed stream started after cancellation")
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

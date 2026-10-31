package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHistoryAndCommitChanges(t *testing.T) {
	r := testRepo(t)
	if commits, err := r.history(0, historyPage); err != nil || len(commits) != 0 {
		t.Fatalf("empty history: %#v %v", commits, err)
	}
	writeFile(t, r, "first file", "initial\n")
	if _, err := r.commit("first commit"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "first file", "updated\n")
	writeFile(t, r, "second", "new\n")
	if _, err := r.commit("second commit\n\nDetailed body"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history(0, historyPage)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].subject != "second commit" || commits[1].author != "Test" || commits[0].email != "test@example.com" {
		t.Fatalf("history: %#v", commits)
	}
	if !strings.Contains(commits[0].message, "Detailed body") {
		t.Fatalf("missing body: %#v", commits[0])
	}
	commits, err = r.historyStats([]string{commits[0].hash, commits[1].hash})
	if err != nil {
		t.Fatal(err)
	}
	if commits[0].added != 2 || commits[0].deleted != 1 || commits[1].added != 1 {
		t.Fatalf("wrong line counts: %#v", commits)
	}
	changes, err := r.commitChanges(commits[0].hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || !strings.Contains(changes[0].diff, "+updated") {
		t.Fatalf("changes: %#v", changes)
	}
	changes, err = r.commitChanges(commits[1].hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.Contains(changes[0].diff, "+initial") {
		t.Fatalf("root changes: %#v", changes)
	}
}

func TestHistoryMergeFirstParent(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base", "base\n")
	if _, err := r.commit("base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "feature", "feature\n")
	if _, err := r.commit("feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("checkout", "-"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "main", "main\n")
	if _, err := r.commit("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("merge", "--no-ff", "-m", "merge", "feature"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history(0, historyPage)
	if err != nil {
		t.Fatal(err)
	}
	commits, err = r.historyStats([]string{commits[0].hash})
	if err != nil {
		t.Fatal(err)
	}
	if commits[0].added != 1 || commits[0].deleted != 0 {
		t.Fatalf("merge counts: %#v", commits[0])
	}
	changes, err := r.commitChanges(commits[0].hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].path != "feature" || !strings.Contains(changes[0].diff, "+feature") {
		t.Fatalf("merge changes: %#v", changes)
	}
}

func TestHistorySearchAndReadOnlyNavigation(t *testing.T) {
	m := model{width: 60, height: 10, wrap: true, screen: "history", history: historyState{commits: []commitInfo{
		{hash: "abc123456789", author: "Alice", email: "a@example.com", subject: "One"},
		{hash: "def123456789", author: "Bob", email: "b@example.com", subject: "Fix issue", message: "Fix issue\n\nbody detail"},
	}}}
	for _, query := range []string{"ALICE", "example.com", "fix", "def123", "body detail"} {
		m.history.filter = query
		if got := m.filteredHistory(); len(got) == 0 {
			t.Fatalf("no result for %q", query)
		}
	}
	m.history.filter = ""
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix")})
	m = updated.(model)
	if len(m.filteredHistory()) != 1 || !strings.Contains(m.historyView(), "Fix issue") {
		t.Fatal("live search failed")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.screen != "commit" || cmd == nil || m.history.current.author != "Bob" || m.history.searching || m.history.filter != "fix" {
		t.Fatal("commit not opened")
	}
	for _, key := range []string{" ", "c", "p", "P"} {
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil || updated.(model).screen != "commit" {
			t.Fatalf("mutating key %q accepted", key)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).screen != "history" {
		t.Fatal("back to history failed")
	}
}

func TestSearchEnterWithNoMatchesKeepsSearchOpen(t *testing.T) {
	m := model{screen: "history", history: historyState{searching: true, draft: "missing", commits: []commitInfo{{subject: "present"}}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if got.screen != "history" || !got.history.searching || cmd != nil {
		t.Fatal("empty search should remain editable")
	}
}

func TestSearchEnterOpensFocusedResult(t *testing.T) {
	m := model{screen: "history", history: historyState{searching: true, draft: "fix", commits: []commitInfo{
		{hash: "first", subject: "fix one"}, {hash: "second", subject: "fix two"},
	}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if cmd == nil || got.screen != "commit" || got.history.current.hash != "second" {
		t.Fatalf("wrong focused result: %#v", got.history)
	}
}

func TestHistorySelectionPresentationAndScrolling(t *testing.T) {
	m := model{width: 60, height: 9, screen: "history", history: historyState{commits: []commitInfo{
		{hash: "aaaaaaaa", author: "Alice", subject: "first", statsLoaded: true},
		{hash: "bbbbbbbb", author: "Bob", subject: "second", statsLoaded: true},
		{hash: "cccccccc", author: "Carol", subject: "third", statsLoaded: true},
	}}}
	view := m.historyView()
	if !strings.Contains(view, "\x1b[1m▸ first") || !strings.Contains(view, "\x1b[2m  second") || !strings.Contains(view, "\x1b[32m+0") || !strings.Contains(view, "\x1b[31m -0") {
		t.Fatalf("missing selection styles or spacing: %q", view)
	}
	m.history.selected = 2
	view = m.historyView()
	if !strings.Contains(view, "\x1b[1m▸ third") {
		t.Fatalf("selected commit not visible: %q", view)
	}
}

func TestHistoryEmptyAndBinaryCommits(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "binary", "\x00\x01\x02")
	if _, err := r.commit("binary"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.action("commit", "--allow-empty", "-m", "empty"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.history(0, historyPage)
	if err != nil {
		t.Fatal(err)
	}
	commits, err = r.historyStats([]string{commits[0].hash, commits[1].hash})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].subject != "empty" || commits[1].binary != 1 || commits[1].added != 0 {
		t.Fatalf("history: %#v", commits)
	}
}

func TestSuccessfulCommitOpensHistory(t *testing.T) {
	m := model{width: 60, height: 10, busy: true, history: historyState{selected: 5, filter: "old"}}
	updated, cmd := m.Update(completed{message: "[main abc] message", commitMessage: "message"})
	got := updated.(model)
	if got.screen != "history" || got.history.selected != 0 || got.history.filter != "" || !got.history.loading || cmd == nil {
		t.Fatalf("history did not open after commit: %#v", got)
	}
}

func TestParagraphHistoryAndCommitJumps(t *testing.T) {
	m := model{screen: "history", history: historyState{commits: make([]commitInfo, 7)}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("}")})
	m = updated.(model)
	if m.history.selected != 5 {
		t.Fatalf("history jump: %d", m.history.selected)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("}")})
	m = updated.(model)
	if m.history.selected != 6 {
		t.Fatalf("history end: %d", m.history.selected)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{")})
	m = updated.(model)
	if m.history.selected != 1 {
		t.Fatalf("history jump back: %d", m.history.selected)
	}
	m.screen = "commit"
	m.history.rows = []row{{text: "first"}, {text: ""}, {text: "second"}}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("}")})
	m = updated.(model)
	if m.history.pos != 1 {
		t.Fatalf("commit jump: %d", m.history.pos)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{")})
	if updated.(model).history.pos != 0 {
		t.Fatal("commit jump back failed")
	}
}

func TestCommitChangesBatchesQuotedAndSpacedPaths(t *testing.T) {
	r := testRepo(t)
	for _, path := range []string{"plain", "a b", `q"t`} {
		writeFile(t, r, path, "content "+path+"\n")
	}
	if _, err := r.commit("files"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.commitChanges("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes: %#v", changes)
	}
	for _, c := range changes {
		if strings.Count(c.diff, "diff --git") != 1 || !strings.Contains(c.diff, "+content "+c.path) {
			t.Errorf("%q: %q", c.path, c.diff)
		}
	}
}

func TestHistoryPaging(t *testing.T) {
	r := testRepo(t)
	for _, name := range []string{"one", "two", "three"} {
		writeFile(t, r, name, name+"\n")
		if _, err := r.commit(name); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.history(1, 1)
	if err != nil || len(page) != 1 || page[0].subject != "two" || page[0].search == "" {
		t.Fatalf("page: %#v %v", page, err)
	}

	m := model{height: 20, screen: "history", history: historyState{loading: true, request: 1}}
	full := make([]commitInfo, historyPage)
	updated, cmd := m.Update(historyLoaded{commits: full, request: 1, limit: historyPage})
	m = updated.(model)
	if cmd != nil || !m.history.more || m.history.fetching {
		t.Fatal("first page should not fetch more until the selection nears the end")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(model)
	if !m.history.fetching {
		t.Fatal("reaching the end did not fetch more")
	}
	updated, _ = m.Update(historyLoaded{commits: full[:3], request: 1, skip: 1, limit: historyPage})
	if len(updated.(model).history.commits) != historyPage {
		t.Fatal("misaligned page was appended")
	}
	updated, cmd = m.Update(historyLoaded{commits: full[:3], request: 1, skip: historyPage, limit: historyPage})
	m = updated.(model)
	if cmd != nil || len(m.history.commits) != historyPage+3 || m.history.more || m.history.fetching {
		t.Fatalf("final page: %d more=%v", len(m.history.commits), m.history.more)
	}
	reloaded, cmd := m.reloadHistory()
	if cmd == nil || reloaded.history.request != 2 || reloaded.history.fetching {
		t.Fatal("reload failed")
	}
}

func TestHistoryStatsLoadForViewportAndFollowScrolling(t *testing.T) {
	r := testRepo(t)
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight"} {
		writeFile(t, r, name, name+"\n")
		if _, err := r.commit(name); err != nil {
			t.Fatal(err)
		}
	}
	commits, err := r.history(0, historyPage)
	if err != nil {
		t.Fatal(err)
	}
	m := model{repo: r, width: 60, height: 5, screen: "history", history: historyState{commits: commits}}
	m, cmd := m.loadHistoryStats()
	if cmd == nil || !m.history.statsFetching || !strings.Contains(m.historyView(), "…") {
		t.Fatal("visible statistics were not requested")
	}
	msg := cmd().(historyStatsLoaded)
	if len(msg.hashes) != 1+historyStatsLookahead {
		t.Fatalf("loaded %d commits, expected the visible commit and lookahead", len(msg.hashes))
	}
	updated, cmd := m.Update(msg)
	m = updated.(model)
	if cmd != nil || !m.history.commits[0].statsLoaded || !m.history.commits[5].statsLoaded || m.history.commits[6].statsLoaded {
		t.Fatal("statistics did not load for the viewport and lookahead")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if cmd == nil || !m.history.statsFetching {
		t.Fatal("scrolling did not request newly visible statistics")
	}
	updated, _ = m.Update(cmd())
	if !updated.(model).history.commits[6].statsLoaded {
		t.Fatal("new lookahead statistics were not applied")
	}
}

func TestStaleHistoryStatsAreIgnored(t *testing.T) {
	m := model{screen: "history", history: historyState{request: 2, statsFetching: true, commits: []commitInfo{{hash: "a"}}}}
	updated, _ := m.Update(historyStatsLoaded{request: 1, commits: []commitInfo{{hash: "a", statsLoaded: true}}})
	got := updated.(model)
	if got.history.commits[0].statsLoaded || !got.history.statsFetching {
		t.Fatal("stale statistics changed the current history")
	}
}

func TestSearchFetchesOlderHistory(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", history: historyState{commits: make([]commitInfo, historyPage), more: true}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("empty search should not fetch")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(model)
	if cmd == nil || !m.history.fetching || !strings.Contains(m.historyView(), "searching older commits") {
		t.Fatal("search did not fetch older commits")
	}
}

func TestHistoryHeaderHeight(t *testing.T) {
	for _, available := range []bool{false, true} {
		for height := 0; height < 10; height++ {
			m := model{width: 40, height: height, snapshot: snapshot{branch: branchStatus{available: available}}}
			if got, want := m.historyHeaderHeight(), len(m.historyHeader()); got != want {
				t.Errorf("height %d available %v: %d != %d", height, available, got, want)
			}
		}
	}
}

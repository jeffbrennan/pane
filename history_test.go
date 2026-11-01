package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func TestHistoryCommitTime(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		date string
		want string
	}{
		{now.Add(-5 * time.Minute).Format(time.RFC3339), "5m ago"},
		{now.Add(-23 * time.Hour).Format(time.RFC3339), "23h ago"},
		{now.Add(-24 * time.Hour).Format(time.RFC3339), "2026-10-30"},
		{now.Add(time.Hour).Format(time.RFC3339), "2026-10-31"},
		{"unknown", "unknown"},
	} {
		if got := git.HistoryCommitTime(tt.date, now); got != tt.want {
			t.Errorf("date %q: got %q, want %q", tt.date, got, tt.want)
		}
	}
}

func TestHistoryViewShowsRecentRelativeTime(t *testing.T) {
	m := model{width: 80, height: 12, screen: "history", history: historyState{commits: []git.CommitInfo{{Subject: "recent", Author: "Alice", Date: time.Now().Add(-2 * time.Hour).Format(time.RFC3339)}}}}
	if view := m.View(); !strings.Contains(view, "2h ago") || !strings.Contains(view, "Alice") {
		t.Fatalf("missing recent relative time: %q", view)
	}
}

func TestHistoryAndCommitChanges(t *testing.T) {
	r := testRepo(t)
	if commits, err := r.History(0, git.HistoryPage); err != nil || len(commits) != 0 {
		t.Fatalf("empty history: %#v %v", commits, err)
	}
	writeFile(t, r, "first file", "initial\n")
	if _, err := r.Commit("first commit"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "first file", "updated\n")
	writeFile(t, r, "second", "new\n")
	if _, err := r.Commit("second commit\n\nDetailed body"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, git.HistoryPage)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Subject != "second commit" || commits[1].Author != "Test" || commits[0].Email != "test@example.com" {
		t.Fatalf("history: %#v", commits)
	}
	if !strings.Contains(commits[0].Message, "Detailed body") {
		t.Fatalf("missing body: %#v", commits[0])
	}
	commits, err = r.HistoryStats([]string{commits[0].Hash, commits[1].Hash})
	if err != nil {
		t.Fatal(err)
	}
	if commits[0].Added != 2 || commits[0].Deleted != 1 || commits[1].Added != 1 {
		t.Fatalf("wrong line counts: %#v", commits)
	}
	changes, err := r.CommitChanges(commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || !strings.Contains(changes[0].Diff, "+updated") {
		t.Fatalf("changes: %#v", changes)
	}
	changes, err = r.CommitChanges(commits[1].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.Contains(changes[0].Diff, "+initial") {
		t.Fatalf("root changes: %#v", changes)
	}
}

func TestHistoryMergeFirstParent(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "base", "base\n")
	if _, err := r.Commit("base"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("checkout", "-b", "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "feature", "feature\n")
	if _, err := r.Commit("feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("checkout", "-"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "main", "main\n")
	if _, err := r.Commit("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("merge", "--no-ff", "-m", "merge", "feature"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, git.HistoryPage)
	if err != nil {
		t.Fatal(err)
	}
	commits, err = r.HistoryStats([]string{commits[0].Hash})
	if err != nil {
		t.Fatal(err)
	}
	if commits[0].Added != 1 || commits[0].Deleted != 0 {
		t.Fatalf("merge counts: %#v", commits[0])
	}
	changes, err := r.CommitChanges(commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "feature" || !strings.Contains(changes[0].Diff, "+feature") {
		t.Fatalf("merge changes: %#v", changes)
	}
}

func TestHistorySearchAndReadOnlyNavigation(t *testing.T) {
	m := model{width: 60, height: 10, wrap: true, screen: "history", history: historyState{commits: []git.CommitInfo{
		{Hash: "abc123456789", Author: "Alice", Email: "a@example.com", Subject: "One"},
		{Hash: "def123456789", Author: "Bob", Email: "b@example.com", Subject: "Fix issue", Message: "Fix issue\n\nbody detail"},
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
	if m.screen != "commit" || cmd == nil || m.history.current.Author != "Bob" || m.history.searching || m.history.filter != "fix" {
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

func TestCommitLoadingIsDelayed(t *testing.T) {
	m := model{width: 60, height: 10, screen: "commit", history: historyState{loading: true, request: 1}}
	if view := m.View(); strings.Contains(view, "Loading commit") || strings.Contains(view, "No changes") {
		t.Fatalf("unexpected initial loading state: %q", view)
	}
	updated, _ := m.Update(commitLoadingDelay{request: 0})
	if strings.Contains(updated.(model).View(), "Loading commit") {
		t.Fatal("stale timer showed a loading state")
	}
	updated, _ = m.Update(commitLoadingDelay{request: 1})
	m = updated.(model)
	if view := m.View(); !strings.Contains(view, "Loading commit") || strings.Contains(view, "No changes") {
		t.Fatalf("missing delayed loading state: %q", view)
	}
	updated, _ = m.Update(commitLoaded{request: 1})
	m = updated.(model)
	updated, _ = m.Update(commitLoadingDelay{request: 1})
	if view := updated.(model).View(); strings.Contains(view, "Loading commit") || !strings.Contains(view, "No changes") {
		t.Fatalf("loading timer affected completed empty commit: %q", view)
	}
}

func TestCommitLoadsDiffBeforeSyntax(t *testing.T) {
	changes := []git.Change{longFile("first.go")}
	m := model{width: 60, height: 12, wrap: true, screen: "commit", history: historyState{loading: true, request: 4}}
	updated, cmd := m.Update(commitLoaded{changes: changes, request: 4})
	m = updated.(model)
	if cmd == nil || m.history.loading || len(m.history.rows) < 2 || strings.Contains(m.View(), "Loading commit") {
		t.Fatalf("diff should appear before syntax loads: %q", m.View())
	}
	m.history.pos = 2
	updated, _ = m.Update(commitSyntaxLoaded{changes: changes, request: 4})
	m = updated.(model)
	if m.history.pos != 2 {
		t.Fatalf("syntax loading moved selection to %d", m.history.pos)
	}
	updated, _ = m.Update(commitSyntaxLoaded{changes: nil, request: 3})
	if len(updated.(model).history.changes) != 1 {
		t.Fatal("stale syntax result replaced current changes")
	}
}

func TestCommitHeadingShowsHashAndSubject(t *testing.T) {
	m := model{width: 60, height: 10, screen: "commit", history: historyState{current: git.CommitInfo{Hash: "123456789abcdef", Subject: "commit subject"}}}
	header := strings.Split(plain(m.View()), "\n")[0]
	if !strings.HasPrefix(header, " 12345678  commit subject ") || !strings.HasSuffix(header, "read-only") || strings.Contains(header, "pane") {
		t.Fatalf("unexpected commit heading: %q", header)
	}
}

func TestCommitPickerBackNavigation(t *testing.T) {
	for _, back := range []string{"esc", "q"} {
		for _, close := range []string{"esc", "q"} {
			t.Run(back+"_"+close, func(t *testing.T) {
				m := model{width: 60, height: 12, wrap: true, screen: "commit", history: historyState{changes: []git.Change{longFile("first.go"), longFile("second.go")}}}
				m.rebuildCommit()
				m = keys(t, m, "f", "/", "s", "e", "c", "o", "n", "d", "enter", "j")
				if m.screen != "commit" || m.modal != "" || !m.pickerReturn {
					t.Fatalf("file did not open from picker: %#v", m.history)
				}
				m = keys(t, m, back)
				if m.screen != "commit" || m.modal != "files" || m.picker.query != "second" || m.picker.searching {
					t.Fatalf("did not restore picker and search: screen=%q modal=%q picker=%#v", m.screen, m.modal, m.picker)
				}
				items := m.commitModel().pickerItems()
				if len(items) == 0 || items[m.picker.selected].path != "second.go" {
					t.Fatalf("picker lost its selection: %#v", items)
				}
				m = keys(t, m, close)
				if m.screen != "history" || m.modal != "" {
					t.Fatalf("did not return to history: screen=%q modal=%q", m.screen, m.modal)
				}
			})
		}
	}
}

func TestCommitPickerCanReopenFileAfterGoingBack(t *testing.T) {
	m := model{width: 60, height: 12, wrap: true, screen: "commit", history: historyState{changes: []git.Change{longFile("first.go"), longFile("second.go")}}}
	m.rebuildCommit()
	m = keys(t, m, "f", "/", "s", "e", "c", "o", "n", "d", "enter", "esc", "enter")
	if m.screen != "commit" || m.modal != "" || !m.pickerReturn {
		t.Fatal("reopening a file should restore picker back navigation")
	}
	m = keys(t, m, "q", "q")
	if m.screen != "history" || m.modal != "" {
		t.Fatal("back navigation failed after reopening file")
	}
}

func TestClosingInitiallyOpenedCommitPickerReturnsToDiff(t *testing.T) {
	m := model{width: 60, height: 12, wrap: true, screen: "commit", history: historyState{changes: []git.Change{longFile("file.go")}}}
	m.rebuildCommit()
	m = keys(t, m, "f", "q")
	if m.screen != "commit" || m.modal != "" {
		t.Fatal("closing the initial picker should return to the diff")
	}
	m = keys(t, m, "esc")
	if m.screen != "history" {
		t.Fatal("back from commit diff should return to history")
	}
}

func TestSearchEnterWithNoMatchesKeepsSearchOpen(t *testing.T) {
	m := model{screen: "history", history: historyState{searching: true, draft: "missing", commits: []git.CommitInfo{{Subject: "present"}}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if got.screen != "history" || !got.history.searching || cmd != nil {
		t.Fatal("empty search should remain editable")
	}
}

func TestSearchEnterOpensFocusedResult(t *testing.T) {
	m := model{screen: "history", history: historyState{searching: true, draft: "fix", commits: []git.CommitInfo{
		{Hash: "first", Subject: "fix one"}, {Hash: "second", Subject: "fix two"},
	}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if cmd == nil || got.screen != "commit" || got.history.current.Hash != "second" {
		t.Fatalf("wrong focused result: %#v", got.history)
	}
}

func TestSpaceOpensSelectedCommit(t *testing.T) {
	m := model{screen: "history", history: historyState{selected: 1, commits: []git.CommitInfo{{Hash: "first"}, {Hash: "second"}}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	got := updated.(model)
	if cmd == nil || got.screen != "commit" || got.history.current.Hash != "second" {
		t.Fatal("space did not open the selected commit")
	}

	m.history.searching = true
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	got = updated.(model)
	if got.screen != "history" || got.history.draft != " " {
		t.Fatal("space should remain searchable while editing the history filter")
	}
}

func TestHistorySelectionPresentationAndScrolling(t *testing.T) {
	m := model{width: 60, height: 9, screen: "history", history: historyState{commits: []git.CommitInfo{
		{Hash: "aaaaaaaa", Author: "Alice", Subject: "first", StatsLoaded: true},
		{Hash: "bbbbbbbb", Author: "Bob", Subject: "second", StatsLoaded: true},
		{Hash: "cccccccc", Author: "Carol", Subject: "third", StatsLoaded: true},
	}}}
	view := m.historyView()
	if !strings.Contains(view, "▸ \x1b[0m\x1b[1mfirst") || !strings.Contains(view, "\x1b[2m  second") || !strings.Contains(view, "\x1b[32m+0") || !strings.Contains(view, "\x1b[31m -0") {
		t.Fatalf("missing selection styles or spacing: %q", view)
	}
	m.history.selected = 2
	view = m.historyView()
	if !strings.Contains(view, "▸ \x1b[0m\x1b[1mthird") {
		t.Fatalf("selected commit not visible: %q", view)
	}
}

func TestHistoryReverseScrollKeepsViewportStill(t *testing.T) {
	m := model{width: 60, height: 12, screen: "history", history: historyState{commits: make([]git.CommitInfo, 12)}}
	for i := 0; i < 8; i++ {
		m = keys(t, m, "j")
	}
	offset := m.history.offset
	if offset == 0 {
		t.Fatal("history did not scroll down")
	}
	m = keys(t, m, "k")
	if m.history.offset != offset || m.history.selected != 7 {
		t.Fatalf("history scrolled instead of moving selection: offset=%d selected=%d", m.history.offset, m.history.selected)
	}
	for m.history.selected > offset {
		m = keys(t, m, "k")
	}
	if m.history.offset != offset {
		t.Fatal("history scrolled before the selection reached the top")
	}
	m = keys(t, m, "k")
	if m.history.offset != offset-1 {
		t.Fatal("history did not scroll after the selection reached the top")
	}
}

func TestHistoryEmptyAndBinaryCommits(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "binary", "\x00\x01\x02")
	if _, err := r.Commit("binary"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Action("commit", "--allow-empty", "-m", "empty"); err != nil {
		t.Fatal(err)
	}
	commits, err := r.History(0, git.HistoryPage)
	if err != nil {
		t.Fatal(err)
	}
	commits, err = r.HistoryStats([]string{commits[0].Hash, commits[1].Hash})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Subject != "empty" || commits[1].Binary != 1 || commits[1].Added != 0 {
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
	m := model{screen: "history", history: historyState{commits: make([]git.CommitInfo, 7)}}
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
	if _, err := r.Commit("files"); err != nil {
		t.Fatal(err)
	}
	changes, err := r.CommitChanges("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes: %#v", changes)
	}
	for _, c := range changes {
		if strings.Count(c.Diff, "diff --git") != 1 || !strings.Contains(c.Diff, "+content "+c.Path) {
			t.Errorf("%q: %q", c.Path, c.Diff)
		}
	}
}

func TestHistoryPaging(t *testing.T) {
	r := testRepo(t)
	for _, name := range []string{"one", "two", "three"} {
		writeFile(t, r, name, name+"\n")
		if _, err := r.Commit(name); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.History(1, 1)
	if err != nil || len(page) != 1 || page[0].Subject != "two" || page[0].Search == "" {
		t.Fatalf("page: %#v %v", page, err)
	}

	m := model{height: 20, screen: "history", history: historyState{loading: true, request: 1}}
	full := make([]git.CommitInfo, git.HistoryPage)
	updated, cmd := m.Update(historyLoaded{commits: full, request: 1, limit: git.HistoryPage})
	m = updated.(model)
	if cmd != nil || !m.history.more || m.history.fetching {
		t.Fatal("first page should not fetch more until the selection nears the end")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(model)
	if !m.history.fetching {
		t.Fatal("reaching the end did not fetch more")
	}
	updated, _ = m.Update(historyLoaded{commits: full[:3], request: 1, skip: 1, limit: git.HistoryPage})
	if len(updated.(model).history.commits) != git.HistoryPage {
		t.Fatal("misaligned page was appended")
	}
	updated, cmd = m.Update(historyLoaded{commits: full[:3], request: 1, skip: git.HistoryPage, limit: git.HistoryPage})
	m = updated.(model)
	if cmd != nil || len(m.history.commits) != git.HistoryPage+3 || m.history.more || m.history.fetching {
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
		if _, err := r.Commit(name); err != nil {
			t.Fatal(err)
		}
	}
	commits, err := r.History(0, git.HistoryPage)
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
	if cmd != nil || !m.history.commits[0].StatsLoaded || !m.history.commits[5].StatsLoaded || m.history.commits[6].StatsLoaded {
		t.Fatal("statistics did not load for the viewport and lookahead")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if cmd == nil || !m.history.statsFetching {
		t.Fatal("scrolling did not request newly visible statistics")
	}
	updated, _ = m.Update(cmd())
	if !updated.(model).history.commits[6].StatsLoaded {
		t.Fatal("new lookahead statistics were not applied")
	}
}

func TestStaleHistoryStatsAreIgnored(t *testing.T) {
	m := model{screen: "history", history: historyState{request: 2, statsFetching: true, commits: []git.CommitInfo{{Hash: "a"}}}}
	updated, _ := m.Update(historyStatsLoaded{request: 1, commits: []git.CommitInfo{{Hash: "a", StatsLoaded: true}}})
	got := updated.(model)
	if got.history.commits[0].StatsLoaded || !got.history.statsFetching {
		t.Fatal("stale statistics changed the current history")
	}
}

func TestHistoryStatsCachedWhenReopening(t *testing.T) {
	r := testRepo(t)
	for _, name := range []string{"one", "two"} {
		writeFile(t, r, name, name+"\n")
		if _, err := r.Commit(name); err != nil {
			t.Fatal(err)
		}
	}
	m := model{repo: r, width: 60, height: 10}
	opened, cmd := m.openHistory()
	m, cmd = applyBatch(t, opened.(model), cmd)
	m, _ = applyBatch(t, m, cmd)
	if len(m.history.statsCache) != 2 {
		t.Fatal("loaded statistics were not cached")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	opened, cmd = m.openHistory()
	m, cmd = applyBatch(t, opened.(model), cmd)
	if cmd != nil || !m.history.commits[0].StatsLoaded || !m.history.commits[1].StatsLoaded || m.history.commits[0].Added != 1 {
		t.Fatal("reopened history did not reuse cached statistics")
	}
}

// applyBatch runs a command and expands tea.BatchMsg, since tests drive Update
// directly while the runtime normally does this.
func applyBatch(t *testing.T, m model, cmd tea.Cmd) (model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var next tea.Cmd
		for _, item := range batch {
			var itemCmd tea.Cmd
			m, itemCmd = applyBatch(t, m, item)
			if itemCmd != nil {
				next = itemCmd
			}
		}
		return m, next
	}
	updated, next := m.Update(msg)
	return updated.(model), next
}

func TestHistoryStatsCacheTracksNewestCommits(t *testing.T) {
	commits := make([]git.CommitInfo, historyStatsCacheSize+1)
	for i := range commits {
		commits[i] = git.CommitInfo{Hash: fmt.Sprint(i), StatsLoaded: true, Added: i}
	}
	m := model{screen: "history", history: historyState{commits: commits}}
	m.cacheHistoryStats()
	if len(m.history.statsCache) != historyStatsCacheSize {
		t.Fatal("cache did not respect its size limit")
	}
	m.history.request++
	updated, _ := m.Update(historyLoaded{request: m.history.request, commits: []git.CommitInfo{{Hash: "new"}, {Hash: "1"}}, limit: git.HistoryPage})
	got := updated.(model)
	if got.history.commits[0].StatsLoaded || !got.history.commits[1].StatsLoaded || got.history.commits[1].Added != 1 || len(got.history.statsCache) != 1 {
		t.Fatal("cache did not follow commit hashes after history changed")
	}
}

func TestSearchFetchesOlderHistory(t *testing.T) {
	m := model{width: 60, height: 20, screen: "history", history: historyState{commits: make([]git.CommitInfo, git.HistoryPage), more: true}}
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

func TestHistorySearchUsesLargerPages(t *testing.T) {
	r := testRepo(t)
	writeFile(t, r, "file", "content\n")
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		filter string
		limit  int
	}{
		{name: "navigation", limit: git.HistoryPage},
		{name: "search", filter: "initial", limit: 5 * git.HistoryPage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := model{repo: r, screen: "history", history: historyState{commits: make([]git.CommitInfo, git.HistoryPage), selected: git.HistoryPage - 1, more: true, filter: tt.filter}}
			_, cmd := m.loadMoreHistory()
			if cmd == nil {
				t.Fatal("expected a page request")
			}
			msg := cmd().(historyLoaded)
			if msg.err != nil || msg.limit != tt.limit || msg.skip != git.HistoryPage {
				t.Fatalf("page request: skip=%d limit=%d err=%v", msg.skip, msg.limit, msg.err)
			}
		})
	}
}

func TestHistoryHeaderHeight(t *testing.T) {
	for _, available := range []bool{false, true} {
		for height := 0; height < 10; height++ {
			m := model{width: 40, height: height, snapshot: git.Snapshot{Branch: git.BranchStatus{Available: available}}}
			if got, want := m.historyHeaderHeight(), len(m.historyHeader()); got != want {
				t.Errorf("height %d available %v: %d != %d", height, available, got, want)
			}
		}
	}
}

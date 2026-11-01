package git

import "testing"

func TestFileHistoryFollowsRenamesAndPages(t *testing.T) {
	r := fileHistoryFixture(t)
	commits, err := r.historyWithPath(0, 10, "%aI", "renamed")
	if err != nil || len(commits) != 4 {
		t.Fatalf("file history: %#v %v", commits, err)
	}
	for i, want := range []struct{ subject, path string }{{"latest", "renamed"}, {"rename", "renamed"}, {"second", "old name"}, {"first", "old name"}} {
		if commits[i].Subject != want.subject || commits[i].FilePath != want.path {
			t.Errorf("commit %d: %#v", i, commits[i])
		}
	}
	page, err := r.historyWithPath(2, 2, "%aI", "renamed")
	if err != nil || len(page) != 2 || page[0].FilePath != "old name" || page[0].Subject != "second" {
		t.Fatalf("renamed history page: %#v %v", page, err)
	}
}

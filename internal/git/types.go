package git

type BranchEntry struct {
	Ref, Name, Upstream, Track, Date, Subject, Worktree string
	Current, Remote                                     bool
}

type StashEntry struct{ Ref, Hash, Date, Message string }

type DiscardRequest struct {
	Change Change
	Patch  string
	Whole  bool
}

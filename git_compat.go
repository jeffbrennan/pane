package main

import (
	"time"

	"github.com/jeffbrennan/pane/internal/git"
)

type (
	Repo              = git.Repo
	Change            = git.Change
	Snapshot          = git.Snapshot
	BranchStatus      = git.BranchStatus
	CommitInfo        = git.CommitInfo
	BranchEntry       = git.BranchEntry
	WorktreeEntry     = git.WorktreeEntry
	StashEntry        = git.StashEntry
	StatusEntry       = git.StatusEntry
	FileFingerprint   = git.FileFingerprint
	ChangeKey         = git.ChangeKey
	CachedChange      = git.CachedChange
	RefreshCache      = git.RefreshCache
	CommitChunk       = git.CommitChunk
	HistoryStream     = git.HistoryStream
	HotSpotStream     = git.HotSpotStream
	HotCacheCommit    = git.HotCacheCommit
	HotCacheChange    = git.HotCacheChange
	FullContextResult = git.FullContextResult
	StatsStore        = git.StatsStore
	DiscardRequest    = git.DiscardRequest
)

const (
	historyPage        = git.HistoryPage
	startupSyntaxLines = git.StartupSyntaxLines
	syntaxBudgetFiles  = git.SyntaxBudgetFiles
	syntaxBudgetLines  = git.SyntaxBudgetLines
)

func gitError(out []byte, err error) error { return git.GitError(out, err) }

func relativeCommitTime(date string, now time.Time) string { return git.RelativeCommitTime(date, now) }

func historyCommitTime(date string, now time.Time) string { return git.HistoryCommitTime(date, now) }

func fileHistoryPath(repo Repo, path string) (string, error) { return git.FileHistoryPath(repo, path) }

func warmSyntax(changes []Change) { git.WarmSyntax(changes) }

func (k contextKey) gitContext() git.Context {
	return git.Context{Path: k.path, Commit: k.commit, Staged: k.staged, Base: k.base}
}

func NewHistoryStream(repo Repo, path string, since ...string) *HistoryStream {
	return git.NewHistoryStream(repo, path, since...)
}

func NewHotSpotStream(repo Repo, since, author, revisionRange string) *HotSpotStream {
	return git.NewHotSpotStream(repo, since, author, revisionRange)
}

func splitPatch(patch string) map[string]string { return git.SplitPatch(patch) }

package git

// HotCacheChange is one file's line counts in a cached commit.
type HotCacheChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// HotCacheCommit is one commit in the hotspot cache.
type HotCacheCommit struct {
	Hash    string           `json:"hash"`
	Time    int64            `json:"time"`
	Author  string           `json:"author"`
	Changes []HotCacheChange `json:"changes"`
}

// NewHotCacheCommit builds a cached commit; callers outside git use it in tests.
func NewHotCacheCommit(hash string, when int64, author string, changes []HotCacheChange) HotCacheCommit {
	return HotCacheCommit{Hash: hash, Time: when, Author: author, Changes: changes}
}

package git

type HotCacheCommit struct {
	Hash    string           `json:"hash"`
	Time    int64            `json:"time"`
	Author  string           `json:"author"`
	Changes []hotCacheChange `json:"changes"`
}

type hotCacheChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

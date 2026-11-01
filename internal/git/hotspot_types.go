package git

type HotCacheCommit struct {
	Hash    string           `json:"hash"`
	Time    int64            `json:"time"`
	Author  string           `json:"author"`
	Changes []HotCacheChange `json:"changes"`
}

type HotCacheChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

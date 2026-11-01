package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

const hotHalfLife = 90 * 24 * time.Hour

type hotClass uint8

const (
	hotSource hotClass = iota
	hotTests
	hotDocs
	hotGenerated
	hotVendored
)

func classifyHotPath(path string, marks git.PathMarks) hotClass {
	if marks.Generated[path] || git.GeneratedByPath(path) {
		return hotGenerated
	}
	lower := strings.ToLower(path)
	for _, prefix := range []string{"vendor/", "third_party/", "node_modules/", "deps/"} {
		if strings.HasPrefix(lower, prefix) {
			return hotVendored
		}
	}
	if marks.Vendored[path] {
		return hotVendored
	}
	if marks.Documentation[path] || strings.HasPrefix(lower, "docs/") || strings.HasSuffix(lower, ".md") {
		return hotDocs
	}
	for _, prefix := range []string{"test/", "tests/", "__tests__/", "testdata/", "fixtures/"} {
		if strings.HasPrefix(lower, prefix) {
			return hotTests
		}
	}
	base := lower[strings.LastIndex(lower, "/")+1:]
	if strings.Contains(base, "_test.") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return hotTests
	}
	return hotSource
}

type hotFile struct {
	path           string
	class          hotClass
	added, deleted int
	commits        int
	score          float64
	last           time.Time
	authors        map[string]int
}

type hotAuthor struct {
	name        string
	commits     int
	churn       int
	dirs        map[string]int
	first, last time.Time
}

type hotItemsCache struct {
	items            []hotItem
	authors          []*hotAuthor
	filled           bool
	builtAt          time.Time
	dataVersion      int
	collapsedVersion int
	sortPath         bool
	authorFilter     string
}

type hotspotState struct {
	files            []hotFile
	authors          map[string]*hotAuthor
	items            *hotItemsCache
	hidden           hotHidden
	commits          []git.HotCacheCommit
	stream           *git.HotSpotStream
	collapsed        map[string]bool
	opened           map[string]bool
	showAll          map[string]bool
	dirs             map[string]*hotDirStat
	head             string
	cachePath        string
	cutoff           int64
	list             listState
	request          int
	streamLoaded     int
	dataVersion      int
	collapsedVersion int
	loading          bool
	sortPath         bool
	mode             string
	hiddenShown      bool
	authorFilter     string
	since            string
	author           string
}

type hotspotsLoaded struct {
	files   []hotFile
	authors map[string]*hotAuthor
	items   []hotItem
	dirs    map[string]*hotDirStat
	hidden  hotHidden
	request int
	err     error
}

type hotspotSequenceLoaded struct {
	commits []git.CommitInfo
	request int
	err     error
}

type hotspotsStarted struct {
	plan    hotspotPlan
	request int
	err     error
}

type hotspotPageLoaded struct {
	commits []git.HotCacheCommit
	stream  *git.HotSpotStream
	request int
	done    bool
	err     error
}

func hotSinceDefault() string { return "1.year" }

// hotspotPlan describes how to build the window: a cache hit, or cached
// commits plus a stream for the rest.
type hotspotPlan struct {
	head, cachePath, since, author string
	cutoff                         int64
	commits                        []git.HotCacheCommit
	stream                         *git.HotSpotStream
	files                          []hotFile
	authors                        map[string]*hotAuthor
	items                          []hotItem
	dirs                           map[string]*hotDirStat
	hidden                         hotHidden
	cacheHit                       bool
}

func planHotspots(r git.Repo, since, author string) (hotspotPlan, error) {
	head := ""
	if out, err := r.Command("rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		head = strings.TrimSpace(string(out))
	}
	now := time.Now()
	plan := hotspotPlan{head: head, cachePath: hotCachePath(r.Root, since, author), since: since, author: author}
	// Check the small summary first: a matching HEAD must not decode the
	// per-commit cache.
	if head != "" {
		if files, authors, items, dirs, hidden, cachedAt, ok := readHotSummary(hotSummaryPath(r.Root, since, author), head, since, author); ok && len(items) > 0 && now.Unix()-cachedAt <= int64(hotSummaryTTL.Seconds()) {
			if kept := trackedHotFiles(r, files); len(kept) != len(files) {
				files = kept
				items = buildHotItems(files, nil, false, hotTreeState{}, "")
			}
			plan.files, plan.authors, plan.items, plan.dirs, plan.hidden = files, authors, items, dirs, hidden
			plan.cacheHit = true
			return plan, nil
		}
	}
	cache, cached := readHotCache(plan.cachePath, since, author)
	if cached && head != "" && cache.Head == head {
		plan.cutoff = advanceHotCutoff(since, now)
		plan.commits = pruneHotCommits(cache.Commits, plan.cutoff)
		marks := r.PathMarks(hotspotPaths(plan.commits))
		var hidden hotHidden
		plan.files, plan.authors, plan.dirs, hidden = aggregateHotspots(plan.commits, marks, plan.cutoff, now)
		plan.files = trackedHotFiles(r, plan.files)
		plan.hidden = hidden
		plan.items = buildHotItems(plan.files, plan.dirs, false, hotTreeState{}, "")
		writeHotSummary(hotSummaryPath(r.Root, since, author), head, since, author, plan.cutoff, now.Unix(), plan.files, plan.authors, plan.items, plan.dirs, plan.hidden)
		plan.cacheHit = true
		return plan, nil
	}
	revisionRange := ""
	if cached && cache.Head != "" && head != "" && isAncestor(r, cache.Head, head) {
		plan.cutoff = advanceHotCutoff(since, now)
		plan.commits = pruneHotCommits(cache.Commits, plan.cutoff)
		revisionRange = cache.Head + ".." + head
	} else if cutoff, ok := sinceCutoff(since, now); ok {
		plan.cutoff = cutoff
	}
	plan.stream = git.NewHotSpotStream(r, since, author, revisionRange)
	plan.files, plan.authors, plan.dirs, plan.hidden = aggregateHotspots(plan.commits, builtinPathMarks(plan.commits), plan.cutoff, now)
	return plan, nil
}

// Hotspots is the blocking form used by tests and non-interactive callers.
func hotspots(r git.Repo, since, author string) ([]hotFile, map[string]*hotAuthor, error) {
	plan, err := planHotspots(r, since, author)
	if err != nil {
		return nil, nil, err
	}
	if plan.cacheHit {
		return visibleHotFiles(plan.files), plan.authors, nil
	}
	commits := append([]git.HotCacheCommit(nil), plan.commits...)
	for {
		page, done, err := plan.stream.NextPage(hotspotPageSize)
		if err != nil {
			plan.stream.Close()
			return nil, nil, err
		}
		commits = append(commits, page...)
		if done {
			break
		}
	}
	files, authors, _, _, _, err := finishHotspots(r, plan, commits)
	return visibleHotFiles(files), authors, err
}

func visibleHotFiles(files []hotFile) []hotFile {
	visible := make([]hotFile, 0, len(files))
	for _, file := range files {
		if file.class != hotGenerated && file.class != hotVendored {
			visible = append(visible, file)
		}
	}
	return visible
}

var hotspotPageSize = 100

// The first page is one commit so the tree paints as soon as Git starts
// emitting; later pages batch more.
var hotspotFirstPageSize = 1

// hotSummaryTTL bounds how stale cached scores can be; older summaries are
// rebuilt from the per-commit cache.
var hotSummaryTTL = time.Hour

func finishHotspots(r git.Repo, plan hotspotPlan, commits []git.HotCacheCommit) ([]hotFile, map[string]*hotAuthor, []hotItem, map[string]*hotDirStat, hotHidden, error) {
	commits = pruneHotCommits(commits, plan.cutoff)
	now := time.Now()
	cutoff := plan.cutoff
	if _, ok := sinceCutoff(plan.since, now); !ok {
		if oldest := oldestHotCommit(commits); oldest > 0 {
			cutoff = oldest
		}
	}
	marks := r.PathMarks(hotspotPaths(commits))
	files, authors, dirs, hidden := aggregateHotspots(commits, marks, cutoff, now)
	files = trackedHotFiles(r, files)
	items := buildHotItems(files, dirs, false, hotTreeState{}, "")
	writeHotCache(plan.cachePath, plan.head, plan.since, plan.author, cutoff, now.Unix(), commits)
	writeHotSummary(hotSummaryPath(r.Root, plan.since, plan.author), plan.head, plan.since, plan.author, cutoff, now.Unix(), files, authors, items, dirs, hidden)
	return files, authors, items, dirs, hidden, nil
}

func hotspotPaths(commits []git.HotCacheCommit) []string {
	paths := make([]string, 0, len(commits))
	seen := make(map[string]bool)
	for _, commit := range commits {
		for _, change := range commit.Changes {
			if !seen[change.Path] {
				seen[change.Path] = true
				paths = append(paths, change.Path)
			}
		}
	}
	return paths
}

// trackedHotFiles drops paths that were deleted inside the window, since the
// question is where to look now.
func trackedHotFiles(r git.Repo, files []hotFile) []hotFile {
	tracked, err := r.TrackedPaths()
	if err != nil {
		return files
	}
	kept := files[:0]
	for _, file := range files {
		if tracked[file.path] {
			kept = append(kept, file)
		}
	}
	return kept
}

func builtinPathMarks(commits []git.HotCacheCommit) git.PathMarks {
	marks := newHotPathMarks()
	for _, commit := range commits {
		for _, change := range commit.Changes {
			if git.GeneratedByPath(change.Path) {
				marks.Generated[change.Path] = true
			}
		}
	}
	return marks
}

func newHotPathMarks() git.PathMarks {
	return git.PathMarks{
		Generated:     make(map[string]bool),
		Vendored:      make(map[string]bool),
		Documentation: make(map[string]bool),
	}
}

func isAncestor(r git.Repo, ancestor, descendant string) bool {
	_, err := r.Command("merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

type hotHidden struct{ generated, vendored int }

// hotDirStat tracks the distinct commits, people and owner weight under one
// directory prefix.
type hotDirStat struct {
	commits map[string]bool
	authors map[string]bool
	owners  map[string]float64
	last    time.Time
	class   hotClass
}

func (d *hotDirStat) record(hash, author string, weight float64, when time.Time, class hotClass) {
	if d.commits == nil {
		d.commits = make(map[string]bool)
		d.authors = make(map[string]bool)
		d.owners = make(map[string]float64)
		d.class = hotVendored
	}
	d.commits[hash] = true
	d.authors[author] = true
	d.owners[author] += weight
	if when.After(d.last) {
		d.last = when
	}
	if class < d.class {
		d.class = class
	}
}

func hotDirPrefixes(path string) []string {
	var prefixes []string
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			prefixes = append(prefixes, path[:i])
		}
	}
	return prefixes
}

func aggregateHotspots(commits []git.HotCacheCommit, marks git.PathMarks, cutoff int64, now time.Time) ([]hotFile, map[string]*hotAuthor, map[string]*hotDirStat, hotHidden) {
	files := make(map[string]*hotFile)
	authors := make(map[string]*hotAuthor)
	dirs := make(map[string]*hotDirStat)
	hidden := hotHidden{}
	hiddenGenerated := make(map[string]bool)
	hiddenVendored := make(map[string]bool)
	for _, commit := range commits {
		if commit.Time < cutoff {
			continue
		}
		when := time.Unix(commit.Time, 0)
		age := max(0, now.Sub(when).Seconds())
		decay := math.Pow(0.5, age/hotHalfLife.Seconds())
		// Damp huge diffs and sweeping commits so attention, not volume, ranks.
		sweep := math.Sqrt(float64(max(1, len(commit.Changes))))
		author := authors[commit.Author]
		if author == nil {
			author = &hotAuthor{name: commit.Author, dirs: make(map[string]int)}
			authors[commit.Author] = author
		}
		author.commits++
		if author.first.IsZero() || when.Before(author.first) {
			author.first = when
		}
		if when.After(author.last) {
			author.last = when
		}
		for _, change := range commit.Changes {
			class := classifyHotPath(change.Path, marks)
			switch class {
			case hotGenerated:
				hiddenGenerated[change.Path] = true
			case hotVendored:
				hiddenVendored[change.Path] = true
			}
			file := files[change.Path]
			if file == nil {
				file = &hotFile{path: change.Path, class: class, authors: make(map[string]int)}
				files[change.Path] = file
			}
			weight := decay * math.Log2(1+float64(change.Added+change.Deleted)) / sweep
			file.added += change.Added
			file.deleted += change.Deleted
			file.score += weight
			if class == hotGenerated || class == hotVendored {
				// Hidden noise must not lift a directory's rank.
				file.authors[commit.Author] += change.Added + change.Deleted
				file.commits++
				continue
			}
			for _, prefix := range hotDirPrefixes(change.Path) {
				stat := dirs[prefix]
				if stat == nil {
					stat = &hotDirStat{}
					dirs[prefix] = stat
				}
				stat.record(commit.Hash, commit.Author, weight, when, class)
			}
			if when.After(file.last) {
				file.last = when
			}
			file.authors[commit.Author] += change.Added + change.Deleted
			file.commits++
			author.churn += change.Added + change.Deleted
			if dir := topDir(change.Path); dir != "" {
				author.dirs[dir]++
			}
		}
	}
	result := make([]hotFile, 0, len(files))
	for _, file := range files {
		result = append(result, *file)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].score > result[j].score })
	hidden.generated = len(hiddenGenerated)
	hidden.vendored = len(hiddenVendored)
	return result, authors, dirs, hidden
}

func pruneHotCommits(commits []git.HotCacheCommit, cutoff int64) []git.HotCacheCommit {
	pruned := commits[:0]
	for _, commit := range commits {
		if commit.Time >= cutoff {
			pruned = append(pruned, commit)
		}
	}
	return pruned
}

// advanceHotCutoff recomputes the window boundary from the same relative
// expression Git applies. Unknown expressions keep every cached commit rather
// than risk dropping one that is still inside the window.
func advanceHotCutoff(since string, now time.Time) int64 {
	if cutoff, ok := sinceCutoff(since, now); ok {
		return cutoff
	}
	return 0
}

func oldestHotCommit(commits []git.HotCacheCommit) int64 {
	oldest := int64(0)
	for _, commit := range commits {
		if oldest == 0 || commit.Time < oldest {
			oldest = commit.Time
		}
	}
	return oldest
}

var sincePattern = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*\.?\s*([a-zA-Z]+)`)

var sinceUnits = map[string]time.Duration{
	"y": time.Hour * 24 * 365, "yr": time.Hour * 24 * 365, "yrs": time.Hour * 24 * 365,
	"year": time.Hour * 24 * 365, "years": time.Hour * 24 * 365,
	"mo": time.Hour * 24 * 30, "mon": time.Hour * 24 * 30, "month": time.Hour * 24 * 30, "months": time.Hour * 24 * 30,
	"w": time.Hour * 24 * 7, "wk": time.Hour * 24 * 7, "week": time.Hour * 24 * 7, "weeks": time.Hour * 24 * 7,
	"d": time.Hour * 24, "day": time.Hour * 24, "days": time.Hour * 24,
	"h": time.Hour, "hr": time.Hour, "hour": time.Hour, "hours": time.Hour,
	"min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"s": time.Second, "sec": time.Second, "secs": time.Second, "second": time.Second, "seconds": time.Second,
}

// sinceCutoff converts a relative Git window such as "1.year" or "6mo" into
// the timestamp Git filters against. Absolute dates are left to Git and report
// false, so callers do not prune cached commits.
func sinceCutoff(since string, now time.Time) (int64, bool) {
	since = strings.TrimSpace(strings.ToLower(since))
	if since == "" {
		return 0, true
	}
	if strings.ContainsAny(since, "-/:") {
		return 0, false
	}
	match := sincePattern.FindStringSubmatch(since)
	if match == nil {
		return 0, false
	}
	unit, ok := sinceUnits[match[2]]
	if !ok {
		return 0, false
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	seconds := int64(amount * unit.Seconds())
	return max(0, now.Unix()-seconds), true
}

func topDir(path string) string {
	if i := strings.Index(path, "/"); i > 0 {
		return path[:i]
	}
	return "."
}

const hotCacheVersion = 3

type hotCache struct {
	Version  int                  `json:"version"`
	Head     string               `json:"head"`
	Since    string               `json:"since"`
	Author   string               `json:"author"`
	Cutoff   int64                `json:"cutoff"`
	CachedAt int64                `json:"cached_at"`
	Commits  []git.HotCacheCommit `json:"commits"`
}

type hotCacheFile struct {
	Path    string         `json:"path"`
	Added   int            `json:"added"`
	Deleted int            `json:"deleted"`
	Commits int            `json:"commits"`
	Score   float64        `json:"score"`
	Last    int64          `json:"last"`
	Authors map[string]int `json:"authors"`
}

type hotCacheAuthor struct {
	Name    string         `json:"name"`
	Commits int            `json:"commits"`
	Churn   int            `json:"churn"`
	Dirs    map[string]int `json:"dirs"`
	First   int64          `json:"first"`
	Last    int64          `json:"last"`
}

type hotSummary struct {
	Version         int              `json:"version"`
	Head            string           `json:"head"`
	Since           string           `json:"since"`
	Author          string           `json:"author"`
	Cutoff          int64            `json:"cutoff"`
	CachedAt        int64            `json:"cached_at"`
	HiddenGenerated int              `json:"hidden_generated"`
	HiddenVendored  int              `json:"hidden_vendored"`
	Dirs            []hotCacheDir    `json:"dirs"`
	Files           []hotCacheFile   `json:"files"`
	Authors         []hotCacheAuthor `json:"authors"`
	Items           []hotCacheItem   `json:"items"`
}

type hotCacheItem struct {
	Path      string          `json:"path"`
	Name      string          `json:"name"`
	Branch    string          `json:"branch"`
	Directory bool            `json:"directory"`
	Collapsed bool            `json:"collapsed"`
	Score     float64         `json:"score"`
	Commits   int             `json:"commits"`
	People    int             `json:"people"`
	Churn     int             `json:"churn"`
	Last      int64           `json:"last"`
	Owner     string          `json:"owner"`
	Owners    []hotCacheOwner `json:"owners"`
	Class     uint8           `json:"class"`
}

type hotCacheOwner struct {
	Name  string `json:"name"`
	Share int    `json:"share"`
}

type hotCacheDir struct {
	Path    string             `json:"path"`
	Commits int                `json:"commits"`
	People  int                `json:"people"`
	Owners  map[string]float64 `json:"owners"`
	Last    int64              `json:"last"`
	Class   uint8              `json:"class"`
}

func hotSummaryPath(root, since, author string) string {
	path := hotCachePath(root, since, author)
	if path == "" {
		return ""
	}
	return path + ".summary"
}

func hotCachePath(root, since, author string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(root + "\x00" + since + "\x00" + author))
	return filepath.Join(dir, "pane", "hot-"+hex.EncodeToString(sum[:8]))
}

func readHotCache(path, since, author string) (hotCache, bool) {
	if path == "" {
		return hotCache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return hotCache{}, false
	}
	var cache hotCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != hotCacheVersion || cache.Since != since || cache.Author != author {
		return hotCache{}, false
	}
	return cache, true
}

func readHotSummary(path, head, since, author string) ([]hotFile, map[string]*hotAuthor, []hotItem, map[string]*hotDirStat, hotHidden, int64, bool) {
	if path == "" || head == "" {
		return nil, nil, nil, nil, hotHidden{}, 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, nil, hotHidden{}, 0, false
	}
	defer file.Close()
	var summary hotSummary
	if err := gob.NewDecoder(file).Decode(&summary); err != nil || summary.Version != hotCacheVersion || summary.Head != head || summary.Since != since || summary.Author != author {
		return nil, nil, nil, nil, hotHidden{}, 0, false
	}
	files := make([]hotFile, len(summary.Files))
	for i, file := range summary.Files {
		files[i] = hotFile{path: file.Path, added: file.Added, deleted: file.Deleted, commits: file.Commits, score: file.Score, last: time.Unix(file.Last, 0), authors: file.Authors}
	}
	authors := make(map[string]*hotAuthor, len(summary.Authors))
	for _, author := range summary.Authors {
		authors[author.Name] = &hotAuthor{name: author.Name, commits: author.Commits, churn: author.Churn, dirs: author.Dirs, first: unixOrZero(author.First), last: unixOrZero(author.Last)}
	}
	items := make([]hotItem, len(summary.Items))
	for i, item := range summary.Items {
		items[i] = hotItem{
			path: item.Path, name: item.Name, branch: item.Branch, directory: item.Directory,
			collapsed: item.Collapsed, score: item.Score, commits: item.Commits, people: item.People,
			churn: item.Churn, last: time.Unix(item.Last, 0), owner: item.Owner, class: hotClass(item.Class),
		}
		for _, owner := range item.Owners {
			items[i].owners = append(items[i].owners, hotOwnerShare{name: owner.Name, share: owner.Share})
		}
	}
	dirs := make(map[string]*hotDirStat, len(summary.Dirs))
	for _, dir := range summary.Dirs {
		stat := &hotDirStat{owners: dir.Owners, last: time.Unix(dir.Last, 0), class: hotClass(dir.Class)}
		stat.commits = make(map[string]bool, dir.Commits)
		for i := 0; i < dir.Commits; i++ {
			stat.commits[fmt.Sprintf("%d", i)] = true
		}
		stat.authors = make(map[string]bool, dir.People)
		for i := 0; i < dir.People; i++ {
			stat.authors[fmt.Sprintf("%d", i)] = true
		}
		dirs[dir.Path] = stat
	}
	return files, authors, items, dirs, hotHidden{generated: summary.HiddenGenerated, vendored: summary.HiddenVendored}, summary.CachedAt, true
}

func unixOrZero(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

func writeHotSummary(path, head, since, author string, cutoff, cachedAt int64, files []hotFile, authors map[string]*hotAuthor, items []hotItem, dirs map[string]*hotDirStat, hidden hotHidden) {
	if path == "" {
		return
	}
	summary := hotSummary{Version: hotCacheVersion, Head: head, Since: since, Author: author, Cutoff: cutoff, CachedAt: cachedAt, HiddenGenerated: hidden.generated, HiddenVendored: hidden.vendored}
	for path, stat := range dirs {
		summary.Dirs = append(summary.Dirs, hotCacheDir{
			Path: path, Commits: len(stat.commits), People: len(stat.authors),
			Owners: stat.owners, Last: stat.last.Unix(), Class: uint8(stat.class),
		})
	}
	for _, file := range files {
		summary.Files = append(summary.Files, hotCacheFile{Path: file.path, Added: file.added, Deleted: file.deleted, Commits: file.commits, Score: file.score, Last: file.last.Unix(), Authors: file.authors})
	}
	for _, item := range authors {
		summary.Authors = append(summary.Authors, hotCacheAuthor{Name: item.name, Commits: item.commits, Churn: item.churn, Dirs: item.dirs, First: item.first.Unix(), Last: item.last.Unix()})
	}
	for _, item := range items {
		cached := hotCacheItem{
			Path: item.path, Name: item.name, Branch: item.branch, Directory: item.directory,
			Collapsed: item.collapsed, Score: item.score, Commits: item.commits, People: item.people,
			Churn: item.churn, Last: item.last.Unix(), Owner: item.owner, Class: uint8(item.class),
		}
		for _, owner := range item.owners {
			cached.Owners = append(cached.Owners, hotCacheOwner{Name: owner.name, Share: owner.share})
		}
		summary.Items = append(summary.Items, cached)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	file, err := os.Create(path)
	if err != nil {
		return
	}
	defer file.Close()
	_ = gob.NewEncoder(file).Encode(summary)
}

func writeHotCache(path, head, since, author string, cutoff, cachedAt int64, commits []git.HotCacheCommit) {
	if path == "" {
		return
	}
	cache := hotCache{Version: hotCacheVersion, Head: head, Since: since, Author: author, Cutoff: cutoff, CachedAt: cachedAt, Commits: commits}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

func (m model) loadHotspots() tea.Cmd {
	repo, request := m.repo, m.hotspots.request
	since, author := m.hotspots.since, m.hotspots.author
	return func() tea.Msg {
		plan, err := planHotspots(repo, since, author)
		if err != nil {
			return hotspotsLoaded{request: request, err: err}
		}
		if plan.cacheHit {
			return hotspotsLoaded{files: plan.files, authors: plan.authors, items: plan.items, dirs: plan.dirs, hidden: plan.hidden, request: request}
		}
		return hotspotsStarted{plan: plan, request: request}
	}
}

func (m model) waitHotspotPage(stream *git.HotSpotStream, request, limit int) tea.Cmd {
	return func() tea.Msg {
		commits, done, err := stream.NextPage(limit)
		return hotspotPageLoaded{commits: commits, stream: stream, request: request, done: done, err: err}
	}
}

func (m model) finishHotspots(request int) tea.Cmd {
	plan := hotspotPlan{
		head: m.hotspots.head, cachePath: m.hotspots.cachePath,
		since: m.hotspots.since, author: m.hotspots.author, cutoff: m.hotspots.cutoff,
	}
	commits := append([]git.HotCacheCommit(nil), m.hotspots.commits...)
	repo := m.repo
	return func() tea.Msg {
		files, authors, items, dirs, hidden, err := finishHotspots(repo, plan, commits)
		return hotspotsLoaded{files: files, authors: authors, items: items, dirs: dirs, hidden: hidden, request: request, err: err}
	}
}

func (m *model) closeHotspotStream() {
	if m.hotspots.stream != nil {
		m.hotspots.stream.Close()
		m.hotspots.stream = nil
	}
}

func (m model) openHotspots(since, author string) (tea.Model, tea.Cmd) {
	m.closeHotspotStream()
	m.screen = "hotspots"
	m.hotspots = hotspotState{loading: true, since: since, author: author, request: 1, authors: make(map[string]*hotAuthor), items: &hotItemsCache{}}
	return m, m.loadHotspots()
}

type hotNode struct {
	name, path string
	file       *hotFile
	children   []*hotNode
	byName     map[string]*hotNode
	score      float64
	commits    int
	people     int
	churn      int
	last       time.Time
	class      hotClass
	owner      string
	owners     []hotOwnerShare
}

func (m model) hotItems() []hotItem {
	cache := m.hotspots.items
	if cache != nil && cache.filled && cache.dataVersion == m.hotspots.dataVersion && cache.collapsedVersion == m.hotspots.collapsedVersion && cache.sortPath == m.hotspots.sortPath && cache.authorFilter == m.hotspots.authorFilter {
		return cache.items
	}
	// While pages stream in, keep the previous tree until the rebuild budget
	// expires instead of rebuilding tens of thousands of nodes per page.
	if cache != nil && cache.filled && cache.dataVersion != m.hotspots.dataVersion && cache.collapsedVersion == m.hotspots.collapsedVersion && cache.sortPath == m.hotspots.sortPath && cache.authorFilter == m.hotspots.authorFilter && time.Since(cache.builtAt) < 250*time.Millisecond {
		return cache.items
	}
	items := buildHotItems(m.hotspots.files, m.hotspots.dirs, m.hotspots.sortPath,
		hotTreeState{collapsed: m.hotspots.collapsed, opened: m.hotspots.opened, showAll: m.hotspots.showAll}, m.hotspots.authorFilter)
	if cache != nil {
		cache.items = items
		cache.filled = true
		cache.builtAt = time.Now()
		cache.dataVersion = m.hotspots.dataVersion
		cache.collapsedVersion = m.hotspots.collapsedVersion
		cache.sortPath = m.hotspots.sortPath
		cache.authorFilter = m.hotspots.authorFilter
	}
	return items
}

// hotTreeState is how the reader has folded the tree. Directories open one
// level by default; explicit choices override that.
type hotTreeState struct {
	collapsed  map[string]bool
	opened     map[string]bool
	showAll    map[string]bool
	showHidden bool
}

func (t hotTreeState) open(path string, depth int) bool {
	if t.collapsed[path] {
		return false
	}
	if t.opened[path] {
		return true
	}
	return depth == 0
}

type hotOwnerShare struct {
	name  string
	share int
}

type hotItem struct {
	path, name, branch string
	directory          bool
	collapsed          bool
	more               bool
	remaining          int
	file               *hotFile
	class              hotClass
	score              float64
	commits            int
	people             int
	churn              int
	last               time.Time
	owner              string
	owners             []hotOwnerShare
}

func classRank(class hotClass) int {
	switch class {
	case hotSource:
		return 0
	case hotTests:
		return 1
	case hotDocs:
		return 2
	default:
		return 3
	}
}

func hotOwners(owners map[string]float64) []hotOwnerShare {
	if len(owners) == 0 {
		return nil
	}
	var total float64
	names := make([]string, 0, len(owners))
	for name, share := range owners {
		total += share
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if owners[names[i]] != owners[names[j]] {
			return owners[names[i]] > owners[names[j]]
		}
		return names[i] < names[j]
	})
	if total <= 0 {
		return nil
	}
	var result []hotOwnerShare
	for _, name := range names[:min(2, len(names))] {
		result = append(result, hotOwnerShare{name: name, share: int(math.Round(owners[name] / total * 100))})
	}
	return result
}

func buildHotItems(files []hotFile, dirs map[string]*hotDirStat, sortPath bool, state hotTreeState, authorFilter string) []hotItem {
	if !state.showHidden {
		var visible []hotFile
		for _, file := range files {
			if file.class == hotGenerated || file.class == hotVendored {
				continue
			}
			visible = append(visible, file)
		}
		files = visible
	}
	if authorFilter != "" {
		var filtered []hotFile
		for _, file := range files {
			if file.authors[authorFilter] > 0 {
				filtered = append(filtered, file)
			}
		}
		files = filtered
	}
	root := &hotNode{}
	for _, file := range files {
		path := file.path
		if strings.ContainsRune(path, '\\') {
			path = filepathToSlash(path)
		}
		parts := strings.Split(path, "/")
		node := root
		for i, part := range parts {
			child := node.byName[part]
			if child == nil {
				child = &hotNode{name: part, path: strings.Join(parts[:i+1], "/")}
				node.children = append(node.children, child)
				if node.byName == nil {
					node.byName = make(map[string]*hotNode)
				}
				node.byName[part] = child
			}
			node = child
		}
		file := file
		node.file = &file
	}
	// Merge single-child directory chains so src/bun.js reads as one row.
	var merge func(*hotNode)
	merge = func(node *hotNode) {
		for _, child := range node.children {
			merge(child)
		}
		for i := 0; i < len(node.children); {
			child := node.children[i]
			if child.file == nil && len(child.children) == 1 && child.children[0].file == nil {
				grand := child.children[0]
				child.name += "/" + grand.name
				child.path = grand.path
				child.children = grand.children
				child.byName = grand.byName
				continue
			}
			i++
		}
	}
	merge(root)

	var aggregate func(*hotNode, int) (float64, int, int, time.Time, string, hotClass, int, []hotOwnerShare)
	aggregate = func(node *hotNode, depth int) (float64, int, int, time.Time, string, hotClass, int, []hotOwnerShare) {
		if node.file != nil {
			owners := make(map[string]float64, len(node.file.authors))
			for name, churn := range node.file.authors {
				owners[name] = float64(churn)
			}
			node.owners = hotOwners(owners)
			node.owner = ""
			if len(node.owners) > 0 {
				node.owner = node.owners[0].name
			}
			node.score, node.commits, node.churn, node.last = node.file.score, node.file.commits, node.file.added+node.file.deleted, node.file.last
			node.people, node.class = len(node.file.authors), node.file.class
			return node.score, node.commits, node.churn, node.last, node.owner, node.class, node.people, node.owners
		}
		node.class = hotVendored
		owners := make(map[string]float64)
		for _, child := range node.children {
			score, commits, churn, last, _, class, _, childOwners := aggregate(child, depth+1)
			node.score += score
			if last.After(node.last) {
				node.last = last
			}
			_ = commits
			_ = churn
			_ = childOwners
			if classRank(class) < classRank(node.class) {
				node.class = class
			}
		}
		if stat := dirs[node.path]; stat != nil {
			node.commits = len(stat.commits)
			node.people = len(stat.authors)
			if stat.last.After(node.last) {
				node.last = stat.last
			}
			if classRank(stat.class) < classRank(node.class) {
				node.class = stat.class
			}
			owners = stat.owners
			node.churn = dirChurn(node)
		} else {
			for _, child := range node.children {
				node.commits += child.commits
				node.people += child.people
				node.churn += child.churn
				for _, share := range child.owners {
					owners[share.name] += float64(share.share)
				}
			}
		}
		node.owners = hotOwners(owners)
		if len(node.owners) > 0 {
			node.owner = node.owners[0].name
		}
		return node.score, node.commits, node.churn, node.last, node.owner, node.class, node.people, node.owners
	}
	aggregate(root, 0)

	var sortNodes func(*hotNode)
	sortNodes = func(node *hotNode) {
		sort.SliceStable(node.children, func(i, j int) bool {
			if sortPath {
				return node.children[i].name < node.children[j].name
			}
			left, right := node.children[i], node.children[j]
			if left.class != right.class {
				return classRank(left.class) < classRank(right.class)
			}
			if left.score != right.score {
				return left.score > right.score
			}
			return left.name < right.name
		})
		for _, child := range node.children {
			sortNodes(child)
		}
	}
	sortNodes(root)

	const perDirectory = 5
	var items []hotItem
	var appendChildren func(*hotNode, string, int)
	appendChildren = func(parent *hotNode, prefix string, depth int) {
		children := parent.children
		truncated := 0
		if !state.showAll[parent.path] && len(children) > perDirectory {
			truncated = len(children) - perDirectory
			children = children[:perDirectory]
		}
		for i, node := range children {
			last := i == len(children)-1 && truncated == 0
			branch, childPrefix := "├─ ", prefix+"│  "
			if last {
				branch, childPrefix = "└─ ", prefix+"   "
			}
			marker := "▸ "
			if node.file == nil && state.open(node.path, depth) {
				marker = "▾ "
			}
			item := hotItem{
				path: node.path, name: node.name, branch: prefix + branch, directory: node.file == nil,
				collapsed: node.file == nil && !state.open(node.path, depth), file: node.file,
				class: node.class, score: node.score, commits: node.commits, people: node.people,
				churn: node.churn, last: node.last, owner: node.owner, owners: node.owners,
			}
			if item.directory {
				item.name = marker + item.name + "/"
			}
			items = append(items, item)
			if item.directory && !item.collapsed {
				appendChildren(node, childPrefix, depth+1)
			}
		}
		if truncated > 0 {
			items = append(items, hotItem{
				path: parent.path, name: fmt.Sprintf("…%d more", truncated), branch: prefix + "└─ ",
				more: true, remaining: truncated, directory: true, collapsed: true,
			})
		}
	}
	appendChildren(root, "", 0)
	return items
}

func dirChurn(node *hotNode) int {
	total := 0
	for _, child := range node.children {
		total += child.churn
	}
	return total
}

func filepathToSlash(path string) string { return strings.ReplaceAll(path, "\\", "/") }

func (m model) hotspotSequence(path string) tea.Cmd {
	repo, request := m.repo, m.hotspots.request
	since, author := m.hotspots.since, m.hotspots.author
	return func() tea.Msg {
		args := []string{"log", "-z", "--reverse", "--format=%H"}
		if since != "" {
			args = append(args, "--since="+since)
		}
		if author != "" {
			args = append(args, "--author="+author)
		}
		args = append(args, "--", path)
		out, err := repo.CommandStdout(args...)
		if err != nil {
			return hotspotSequenceLoaded{request: request, err: git.CommandError(out, err)}
		}
		var commits []git.CommitInfo
		for _, field := range bytes.Split(out, []byte{0}) {
			hash := strings.TrimSpace(string(field))
			if hash == "" {
				continue
			}
			if len(commits) == maxSequenceCommits {
				break
			}
			commits = append(commits, git.CommitInfo{Hash: hash})
		}
		return hotspotSequenceLoaded{commits: commits, request: request}
	}
}

func hotBar(score, maxScore float64, width int) string {
	if maxScore <= 0 {
		return strings.Repeat("░", width)
	}
	filled := int(math.Round(score / maxScore * float64(width)))
	filled = max(0, min(filled, width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

const (
	hotBarWidth = 8
)

// hotRows is the number of list rows that fit between the title, header,
// detail line and status bar.
func (m model) hotRows() int { return max(1, m.height-4) }

func hotPeople(n int) string {
	if n == 1 {
		return "1 person"
	}
	return fmt.Sprintf("%d people", n)
}

func hotCommits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return formatCount(n) + " commits"
}

func hotAge(when time.Time, now time.Time) string {
	if when.IsZero() {
		return ""
	}
	age := git.RelativeCommitTime(when.Format(time.RFC3339), now)
	return strings.TrimSuffix(age, " ago")
}

func formatCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}

func hotOwnersText(owners []hotOwnerShare) string {
	var parts []string
	for _, owner := range owners {
		parts = append(parts, fmt.Sprintf("%s %d%%", owner.name, owner.share))
	}
	return strings.Join(parts, " · ")
}

// hotColumnMax maps each tree prefix to the largest score among its siblings,
// so every level has a full bar.
func hotColumnMax(items []hotItem) map[string]float64 {
	maximum := make(map[string]float64)
	for _, item := range items {
		prefix := item.branch
		if i := strings.LastIndex(prefix, "─ "); i >= 0 {
			prefix = prefix[:i]
		}
		maximum[prefix] = max(maximum[prefix], item.score)
	}
	return maximum
}

// hotColumns formats the fixed columns that follow a tree or file name. The
// owners text comes last and is the first thing cut on a narrow screen.
func hotColumns(activity, commits, people, last, owners string) string {
	text := fmt.Sprintf("%-8s  %7s  %6s  %5s", activity, commits, people, last)
	if owners != "" {
		text += "  " + owners
	}
	return text
}

// hotFixedWidth is the width of hotColumns without the owners text.
var hotFixedWidth = len(hotColumns("", "", "", "", ""))

func (m model) hotItemColumns(item hotItem, maximum map[string]float64, now time.Time) string {
	prefix := item.branch
	if i := strings.LastIndex(prefix, "─ "); i >= 0 {
		prefix = prefix[:i]
	}
	if item.more {
		return ""
	}
	people := ""
	if item.people > 0 {
		people = formatCount(item.people)
	}
	owners := ""
	if m.showHotOwners() {
		owners = hotOwnersText(item.owners)
	}
	return hotColumns(hotBar(item.score, maximum[prefix], hotBarWidth), formatCount(item.commits), people, hotAge(item.last, now), owners)
}

func (m model) showHotOwners() bool { return len(m.hotspots.authors) > 1 }

// hotAuthorColumns formats the columns that follow an author's name.
func hotAuthorColumns(commits, active, areas string) string {
	text := fmt.Sprintf("%7s  %-13s", commits, active)
	if areas != "" {
		text += "  " + areas
	}
	return text
}

var hotAuthorFixedWidth = len(hotAuthorColumns("", "", ""))

func hotActive(author *hotAuthor, now time.Time) string {
	first, last := hotAge(author.first, now), hotAge(author.last, now)
	switch {
	case first == "" && last == "":
		return ""
	case first == last || first == "":
		return last
	}
	return first + " – " + last
}

// hotHeaderRow labels the columns at the same offsets listTableRow uses.
func (m model) hotHeaderRow(count, nameWidth int) string {
	name, columns := "area", hotColumns("activity", "commits", "people", "last", "")
	if m.showHotOwners() {
		columns += "  owners"
	}
	if m.hotspots.mode == "authors" {
		name, columns = "author", hotAuthorColumns("commits", "active", "areas")
	}
	indent := strings.Repeat(" ", listPrefixWidth(count))
	return styleDim + ansi.Truncate(indent+padCell(name, nameWidth)+"  "+columns, m.width, "") + styleReset
}

func (m model) hotWindowLabel() string {
	switch m.hotspots.since {
	case "3.months", "3mo":
		return "last 3 months"
	case "1.year", "1y":
		return "last 12 months"
	case "":
		return "all time"
	}
	return m.hotspots.since
}

// hotFileItems is the flat ranked file view, the direct "which files matter"
// answer the tree buries.
func (m model) hotFileItems() []hotItem {
	files := m.hotspots.files
	query := strings.ToLower(strings.TrimSpace(m.hotspots.list.filter))
	author := m.hotspots.authorFilter
	var items []hotItem
	for _, file := range files {
		if !m.hotspots.hiddenShown && (file.class == hotGenerated || file.class == hotVendored) {
			continue
		}
		if author != "" && file.authors[author] == 0 {
			continue
		}
		if query != "" && !matchesPicker(file.path, query) {
			continue
		}
		owners := make(map[string]float64, len(file.authors))
		for name, churn := range file.authors {
			owners[name] = float64(churn)
		}
		items = append(items, hotItem{
			path: file.path, name: file.path, file: &file, class: file.class, score: file.score,
			commits: file.commits, people: len(file.authors), churn: file.added + file.deleted,
			last: file.last, owners: hotOwners(owners),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
	return items
}

func isBotAuthor(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "[bot]") || strings.HasSuffix(lower, "bot") || strings.Contains(lower, "github-actions")
}

func (m model) hotFilteredItems() []hotItem {
	items := m.hotItems()
	query := strings.ToLower(strings.TrimSpace(m.hotspots.list.filter))
	if query == "" {
		return items
	}
	var matches []hotItem
	for _, item := range items {
		if matchesPicker(item.path, query) || matchesPicker(item.name, query) {
			matches = append(matches, item)
		}
	}
	return matches
}

func (m model) hotFilteredAuthors() []*hotAuthor {
	authors := m.hotAuthorList()
	query := strings.ToLower(strings.TrimSpace(m.hotspots.list.filter))
	if query == "" {
		return authors
	}
	var matches []*hotAuthor
	for _, author := range authors {
		if matchesPicker(author.name, query) {
			matches = append(matches, author)
		}
	}
	return matches
}

func (m model) hotView() string {
	commits := m.hotspots.streamLoaded
	if commits == 0 {
		commits = len(m.hotspots.commits)
	}
	if commits == 0 {
		// A cached summary keeps no commits; each one has a single author.
		for _, author := range m.hotspots.authors {
			commits += author.commits
		}
	}
	title := fmt.Sprintf(" hotspots · %s · %s commits · %s", m.hotWindowLabel(), formatCount(commits), hotPeople(len(m.hotspots.authors)))
	if m.hotspots.authorFilter != "" {
		title = fmt.Sprintf(" hotspots · %s · %s commits · by %s", m.hotWindowLabel(), formatCount(commits), m.hotspots.authorFilter)
	}
	view := "tree"
	switch m.hotspots.mode {
	case "files":
		view = "files"
	case "authors":
		view = "authors"
	}
	title += " · " + view
	lines := []string{m.titleBar(" "+m.accent()+syntax.Clean(title)+styleReset, stateBadges(m.filterBadge(m.hotspots.list.filter)))}
	now := time.Now()
	rows := m.hotRows()
	var items []hotItem
	selected := m.hotspots.list.selected
	var body []string
	var header string
	switch m.hotspots.mode {
	case "authors":
		authors := m.hotFilteredAuthors()
		selected = min(selected, max(0, len(authors)-1))
		offset, visible := listViewport(m.hotspots.list, len(authors), rows)
		end := min(len(authors), offset+visible)
		names := make([]string, 0, end-offset)
		for _, author := range authors[offset:end] {
			names = append(names, author.name)
		}
		nameWidth := m.listNameWidth(len(authors), append(names, "author"), hotAuthorFixedWidth)
		header = m.hotHeaderRow(len(authors), nameWidth)
		for i := offset; i < end; i++ {
			author := authors[i]
			columns := hotAuthorColumns(formatCount(author.commits), hotActive(author, now), topAuthorDirs(author, 3))
			body = append(body, m.listTableRow(i, selected, len(authors), author.name, nameWidth, columns))
		}
	default:
		if m.hotspots.mode == "files" {
			items = m.hotFileItems()
		} else {
			items = m.hotFilteredItems()
		}
		selected = min(selected, max(0, len(items)-1))
		offset, visible := listViewport(m.hotspots.list, len(items), rows)
		end := min(len(items), offset+visible)
		maximum := hotColumnMax(items)
		names := make([]string, 0, end-offset)
		for _, item := range items[offset:end] {
			names = append(names, item.branch+item.name)
		}
		nameWidth := m.listNameWidth(len(items), append(names, "area"), hotFixedWidth)
		header = m.hotHeaderRow(len(items), nameWidth)
		for i := offset; i < end; i++ {
			body = append(body, m.listTableRow(i, selected, len(items), items[i].branch+items[i].name, nameWidth, m.hotItemColumns(items[i], maximum, now)))
		}
	}
	lines = append(lines, header)
	lines = append(lines, body...)
	if len(items) == 0 && m.hotspots.mode != "authors" {
		text := " No changes in the window"
		if m.hotspots.loading {
			text = " Loading hotspots…"
		}
		lines = append(lines, fit(text, m.width))
	}
	// Detail line for the selection answers "how big is this" without widening
	// the table.
	detail := m.hotDetail(selected, items, now)
	if detail == "" {
		detail = " "
	}
	lines = append(lines, styleDim+fit(detail, m.width)+styleReset)
	status := ""
	if filter := m.hotspots.list.listStatus(m.width); filter != "" {
		status = filter
	}
	hints := m.hotFooter()
	if m.flash != "" {
		status = syntax.Clean(m.flash)
	}
	lines = append(lines, m.statusBar(status, hints))
	if m.modal == "help" {
		clearBody(lines)
		m.showHelpDialog(lines)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
}

func (m model) hotDetail(selected int, items []hotItem, now time.Time) string {
	if len(items) == 0 || selected < 0 || selected >= len(items) {
		return ""
	}
	item := items[selected]
	if item.more {
		return fmt.Sprintf("%s · %d more entries · l expand", item.path, item.remaining)
	}
	parts := []string{item.name}
	if item.directory {
		parts[0] = item.path + "/"
	}
	parts = append(parts, hotCommits(item.commits))
	if item.people > 0 {
		parts = append(parts, hotPeople(item.people))
	}
	if item.churn > 0 {
		parts = append(parts, fmt.Sprintf("%s lines changed", formatCount(item.churn)))
	}
	if !item.last.IsZero() {
		parts = append(parts, "busiest "+hotAge(item.last, now))
	}
	return strings.Join(parts, " · ")
}

func (m model) hotFooter() string {
	view := "tree"
	switch m.hotspots.mode {
	case "files":
		view = "files"
	case "authors":
		view = "authors"
	}
	hidden := m.hotspots.hidden.generated + m.hotspots.hidden.vendored
	a := "hidden"
	if hidden > 0 {
		a = fmt.Sprintf("hidden %d", hidden)
	}
	actions := []shortcut{{"tab", view}, {"w", "window"}, {"a", a}, {"↵", "open"}}
	if m.hotspots.mode == "authors" {
		actions = []shortcut{{"tab", view}, {"w", "window"}, {"↵", "filter"}}
	}
	return footer(true, actions...)
}

func topAuthorDirs(author *hotAuthor, limit int) string {
	type share struct {
		dir   string
		count int
	}
	var shares []share
	total := 0
	for dir, count := range author.dirs {
		shares = append(shares, share{dir, count})
		total += count
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].count != shares[j].count {
			return shares[i].count > shares[j].count
		}
		return shares[i].dir < shares[j].dir
	})
	var parts []string
	for _, s := range shares[:min(limit, len(shares))] {
		parts = append(parts, fmt.Sprintf("%s %d%%", s.dir, s.count*100/max(1, total)))
	}
	return strings.Join(parts, " · ")
}

func topAuthorDir(author *hotAuthor) string {
	top, count := "", 0
	for dir, n := range author.dirs {
		if n > count {
			top, count = dir, n
		}
	}
	if top == "" {
		return ""
	}
	return top
}

func (m model) hotAuthorList() []*hotAuthor {
	if cache := m.hotspots.items; cache != nil && cache.filled && cache.dataVersion == m.hotspots.dataVersion && cache.authors != nil {
		return cache.authors
	}
	authors := make([]*hotAuthor, 0, len(m.hotspots.authors))
	for _, author := range m.hotspots.authors {
		authors = append(authors, author)
	}
	sort.Slice(authors, func(i, j int) bool {
		leftBot, rightBot := isBotAuthor(authors[i].name), isBotAuthor(authors[j].name)
		if leftBot != rightBot {
			return !leftBot
		}
		if authors[i].commits != authors[j].commits {
			return authors[i].commits > authors[j].commits
		}
		return authors[i].name < authors[j].name
	})
	if cache := m.hotspots.items; cache != nil {
		cache.authors = authors
		cache.dataVersion = m.hotspots.dataVersion
	}
	return authors
}

func (m model) hotVisible() []hotItem {
	if m.hotspots.mode == "files" {
		return m.hotFileItems()
	}
	return m.hotFilteredItems()
}

func (m model) handleHotspotKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	count := len(m.hotVisible())
	if m.hotspots.mode == "authors" {
		count = len(m.hotFilteredAuthors())
	}
	if m.handleListKey(&m.hotspots.list, key.String(), listRunes(key), count, m.hotRows(), nil) {
		return m, nil
	}
	resetItems := func() {
		if m.hotspots.items != nil {
			m.hotspots.items.filled = false
		}
	}
	switch key.String() {
	case "esc", "q":
		if m.hotspots.authorFilter != "" {
			m.hotspots.authorFilter = ""
			m.hotspots.list.reset()
			resetItems()
			break
		}
		m.closeHotspotStream()
		return m.openHistory()
	case "ctrl+c":
		m.closeHotspotStream()
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.help.reset()
	case "tab":
		switch m.hotspots.mode {
		case "":
			m.hotspots.mode = "files"
		case "files":
			m.hotspots.mode = "authors"
		default:
			m.hotspots.mode = ""
		}
		m.hotspots.list.reset()
		resetItems()
	case "a":
		m.hotspots.hiddenShown = !m.hotspots.hiddenShown
		m.hotspots.collapsedVersion++
		resetItems()
		if m.hotspots.hiddenShown {
			m.flash = "Showing generated and vendored files"
		} else {
			m.flash = "Hiding generated and vendored files"
		}
	case "w":
		m.hotspots.since = nextHotWindow(m.hotspots.since)
		m.closeHotspotStream()
		next, cmd := m.openHotspots(m.hotspots.since, m.hotspots.author)
		return next, cmd
	case "h", "l":
		items := m.hotVisible()
		if m.hotspots.list.selected < 0 || m.hotspots.list.selected >= len(items) {
			break
		}
		item := items[m.hotspots.list.selected]
		if item.more {
			if m.hotspots.showAll == nil {
				m.hotspots.showAll = make(map[string]bool)
			}
			if key.String() == "l" {
				m.hotspots.showAll[item.path] = true
			}
			m.hotspots.collapsedVersion++
			resetItems()
			break
		}
		if !item.directory {
			break
		}
		if m.hotspots.collapsed == nil {
			m.hotspots.collapsed = make(map[string]bool)
		}
		if m.hotspots.opened == nil {
			m.hotspots.opened = make(map[string]bool)
		}
		if key.String() == "h" {
			m.hotspots.collapsed[item.path] = true
			delete(m.hotspots.opened, item.path)
		} else {
			delete(m.hotspots.collapsed, item.path)
			m.hotspots.opened[item.path] = true
		}
		m.hotspots.collapsedVersion++
		resetItems()
	case "V":
		if m.hotspots.mode != "authors" {
			items := m.hotVisible()
			if len(items) > 0 {
				m.closeHotspotStream()
				return m, m.hotspotSequence(items[min(m.hotspots.list.selected, len(items)-1)].path)
			}
		}
	case "o":
		items := m.hotVisible()
		if len(items) > 0 {
			item := items[min(m.hotspots.list.selected, len(items)-1)]
			if !item.directory {
				return m.openFilePath(item.path, 0)
			}
		}
	case "L":
		items := m.hotVisible()
		if len(items) > 0 {
			item := items[min(m.hotspots.list.selected, len(items)-1)]
			if item.directory {
				return m.openPathHistory(item.path)
			}
		}
	case "enter":
		if m.hotspots.mode == "authors" {
			authors := m.hotFilteredAuthors()
			if len(authors) > 0 {
				m.hotspots.authorFilter = authors[min(m.hotspots.list.selected, len(authors)-1)].name
				m.hotspots.mode = ""
				m.hotspots.list.reset()
				resetItems()
			}
			break
		}
		items := m.hotVisible()
		if len(items) == 0 {
			break
		}
		item := items[min(m.hotspots.list.selected, len(items)-1)]
		if item.more {
			if m.hotspots.showAll == nil {
				m.hotspots.showAll = make(map[string]bool)
			}
			m.hotspots.showAll[item.path] = true
			m.hotspots.collapsedVersion++
			resetItems()
			break
		}
		if item.directory {
			if m.hotspots.collapsed == nil {
				m.hotspots.collapsed = make(map[string]bool)
			}
			if m.hotspots.opened == nil {
				m.hotspots.opened = make(map[string]bool)
			}
			if item.collapsed {
				delete(m.hotspots.collapsed, item.path)
				m.hotspots.opened[item.path] = true
			} else {
				m.hotspots.collapsed[item.path] = true
				delete(m.hotspots.opened, item.path)
			}
			m.hotspots.collapsedVersion++
			resetItems()
			break
		}
		m.closeHotspotStream()
		return m.openPathHistory(item.path)
	}
	return m, nil
}

// nextHotWindow cycles the three exploration windows.
func nextHotWindow(since string) string {
	switch since {
	case "3.months", "3mo":
		return "1.year"
	case "1.year", "1y":
		return ""
	default:
		return "3.months"
	}
}

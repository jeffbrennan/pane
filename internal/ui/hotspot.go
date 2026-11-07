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
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

const hotHalfLife = 90 * 24 * time.Hour

// hotBuckets is how many equal slices of the window an activity strip shows,
// oldest on the left.
const hotBuckets = 12

// hotTimeline is activity per slice of the window. Unlike the score it is not
// decayed: the strip shows when work happened, and its position already says
// how recent it was.
type hotTimeline [hotBuckets]float64

func (t *hotTimeline) add(other hotTimeline) {
	for i := range t {
		t[i] += other[i]
	}
}

func (t hotTimeline) peak() float64 {
	peak := 0.0
	for _, v := range t {
		peak = max(peak, v)
	}
	return peak
}

// hotBucket places a commit time in the window that runs from start to now.
func hotBucket(when, start, now int64) int {
	if now <= start {
		return hotBuckets - 1
	}
	i := int(float64(when-start) / float64(now-start) * hotBuckets)
	return max(0, min(i, hotBuckets-1))
}

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
	timeline       hotTimeline
}

type hotAuthor struct {
	name        string
	commits     int
	churn       int
	dirs        map[string]int
	areas       map[string]*hotArea
	first, last time.Time
	timeline    hotTimeline
	// person lists the names and emails merged into this author.
	person *hotPerson
}

// hotArea is one author's work in one top-level directory, the rows an
// author expands into.
type hotArea struct {
	commits     int
	first, last time.Time
	timeline    hotTimeline
}

func (a *hotArea) record(when time.Time, bucket int) {
	a.commits++
	a.timeline[bucket]++
	if a.first.IsZero() || when.Before(a.first) {
		a.first = when
	}
	if when.After(a.last) {
		a.last = when
	}
}

type hotItemsCache struct {
	items            []hotItem
	filled           bool
	builtAt          time.Time
	dataVersion      int
	collapsedVersion int
	sortPath         bool
	authorFilter     string
	// root is the aggregated tree items are flattened from, so folding only
	// walks the open rows instead of rebuilding every file.
	root    *hotNode
	rootKey hotTreeKey
	// authors and files hold the other views for the data version they were
	// built from.
	authors        []*hotAuthor
	authorsVersion int
	authorsReady   bool
	files          []hotItem
	filesKey       hotFilesKey
	filesReady     bool
	filesLongest   string
}

type hotTreeKey struct {
	dataVersion  int
	sortPath     bool
	authorFilter string
}

type hotFilesKey struct {
	dataVersion  int
	authorFilter string
	hiddenShown  bool
	query        string
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
	// scope limits the authors view to who changed one path; scopeReturn is
	// the view to go back to.
	scope        string
	scopeFile    bool
	scopeReturn  string
	scopeAuthors []*hotAuthor
	scopeLoading bool
	// expanded lists the authors opened into per-area rows.
	expanded map[string]bool
	// windows keeps each finished window so cycling back to it is instant.
	windows map[string]*hotWindow
	// aggregateCost is how long the last streamed refresh took to aggregate;
	// refreshes are spaced out so it never dominates the walk.
	aggregateCost time.Duration
}

type hotWindow struct {
	files   []hotFile
	authors map[string]*hotAuthor
	dirs    map[string]*hotDirStat
	hidden  hotHidden
	commits []git.HotCacheCommit
	at      time.Time
}

type hotspotScopeLoaded struct {
	scope   string
	authors []*hotAuthor
	request int
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

// hotspotPageLoaded carries every commit read so far and their aggregates,
// computed off the update loop so keys stay responsive while a large window
// streams in.
type hotspotPageLoaded struct {
	commits []git.HotCacheCommit
	files   []hotFile
	authors map[string]*hotAuthor
	dirs    map[string]*hotDirStat
	hidden  hotHidden
	root    *hotNode
	treeKey hotTreeKey
	cost    time.Duration
	stream  *git.HotSpotStream
	request int
	done    bool
	err     error
}

// hotspotRefresh is the shortest gap between tree refreshes while streaming.
var hotspotRefresh = 250 * time.Millisecond

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
	// Bots stay in owners so a bot-only area can still name someone; hotOwners
	// drops them when a human is available.
	d.owners[author] += weight
	if !isBotAuthor(author) {
		d.authors[author] = true
	}
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
	start := hotWindowStart(commits, cutoff)
	canonical, people := hotIdentities(commits)
	for _, commit := range commits {
		if commit.Time < cutoff {
			continue
		}
		bucket := hotBucket(commit.Time, start, now.Unix())
		when := time.Unix(commit.Time, 0)
		age := max(0, now.Sub(when).Seconds())
		decay := math.Pow(0.5, age/hotHalfLife.Seconds())
		// Damp huge diffs and sweeping commits so attention, not volume, ranks.
		sweep := math.Sqrt(float64(max(1, len(commit.Changes))))
		name := canonical[commit.Author]
		author := authors[name]
		if author == nil {
			author = &hotAuthor{name: name, dirs: make(map[string]int), areas: make(map[string]*hotArea), person: people[name]}
			authors[name] = author
		}
		author.commits++
		author.timeline[bucket]++
		if author.first.IsZero() || when.Before(author.first) {
			author.first = when
		}
		if when.After(author.last) {
			author.last = when
		}
		touched := make(map[string]bool, 4)
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
			attention := math.Log2(1+float64(change.Added+change.Deleted)) / sweep
			weight := decay * attention
			file.added += change.Added
			file.deleted += change.Deleted
			file.score += weight
			file.timeline[bucket] += attention
			if class == hotGenerated || class == hotVendored {
				// Hidden noise must not lift a directory's rank.
				file.authors[name] += change.Added + change.Deleted
				file.commits++
				continue
			}
			for _, prefix := range hotDirPrefixes(change.Path) {
				stat := dirs[prefix]
				if stat == nil {
					stat = &hotDirStat{}
					dirs[prefix] = stat
				}
				stat.record(commit.Hash, name, weight, when, class)
			}
			if when.After(file.last) {
				file.last = when
			}
			file.authors[name] += change.Added + change.Deleted
			file.commits++
			author.churn += change.Added + change.Deleted
			if dir := topDir(change.Path); dir != "" {
				author.dirs[dir]++
				if !touched[dir] {
					touched[dir] = true
					area := author.areas[dir]
					if area == nil {
						area = &hotArea{}
						author.areas[dir] = area
					}
					area.record(when, bucket)
				}
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

// hotWindowStart is where activity strips begin: the oldest commit inside the
// window rather than the window's edge, so a young repository's history fills
// the strip instead of crowding into its last cell.
func hotWindowStart(commits []git.HotCacheCommit, cutoff int64) int64 {
	start := int64(0)
	for _, commit := range commits {
		if commit.Time >= cutoff && (start == 0 || commit.Time < start) {
			start = commit.Time
		}
	}
	return start
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

// 5: bots are recognised by convention instead of a name list.
// 6: summaries carry activity timelines.
// 7: authors carry per-area activity; drops caches a cancelled walk could
// have written as a whole window.
// 8: commits carry the author's email so one person's names merge.
const hotCacheVersion = 8

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
	Path     string         `json:"path"`
	Added    int            `json:"added"`
	Deleted  int            `json:"deleted"`
	Commits  int            `json:"commits"`
	Score    float64        `json:"score"`
	Last     int64          `json:"last"`
	Authors  map[string]int `json:"authors"`
	Timeline hotTimeline    `json:"timeline"`
}

type hotCacheAuthor struct {
	Name     string         `json:"name"`
	Commits  int            `json:"commits"`
	Churn    int            `json:"churn"`
	Dirs     map[string]int `json:"dirs"`
	First    int64          `json:"first"`
	Last     int64          `json:"last"`
	Timeline hotTimeline    `json:"timeline"`
	Areas    []hotCacheArea `json:"areas"`
	Names    []string       `json:"names"`
	Emails   []string       `json:"emails"`
}

type hotCacheArea struct {
	Dir      string      `json:"dir"`
	Commits  int         `json:"commits"`
	First    int64       `json:"first"`
	Last     int64       `json:"last"`
	Timeline hotTimeline `json:"timeline"`
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
	Timeline  hotTimeline     `json:"timeline"`
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
		files[i] = hotFile{path: file.Path, added: file.Added, deleted: file.Deleted, commits: file.Commits, score: file.Score, last: time.Unix(file.Last, 0), authors: file.Authors, timeline: file.Timeline}
	}
	authors := make(map[string]*hotAuthor, len(summary.Authors))
	for _, author := range summary.Authors {
		areas := make(map[string]*hotArea, len(author.Areas))
		for _, area := range author.Areas {
			areas[area.Dir] = &hotArea{commits: area.Commits, first: unixOrZero(area.First), last: unixOrZero(area.Last), timeline: area.Timeline}
		}
		authors[author.Name] = &hotAuthor{
			name: author.Name, commits: author.Commits, churn: author.Churn, dirs: author.Dirs, areas: areas,
			first: unixOrZero(author.First), last: unixOrZero(author.Last), timeline: author.Timeline,
			person: &hotPerson{names: author.Names, emails: author.Emails},
		}
	}
	items := make([]hotItem, len(summary.Items))
	for i, item := range summary.Items {
		items[i] = hotItem{
			path: item.Path, name: item.Name, branch: item.Branch, directory: item.Directory,
			collapsed: item.Collapsed, score: item.Score, commits: item.Commits, people: item.People,
			churn: item.Churn, last: time.Unix(item.Last, 0), owner: item.Owner, class: hotClass(item.Class),
			timeline: item.Timeline,
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
		summary.Files = append(summary.Files, hotCacheFile{Path: file.path, Added: file.added, Deleted: file.deleted, Commits: file.commits, Score: file.score, Last: file.last.Unix(), Authors: file.authors, Timeline: file.timeline})
	}
	for _, item := range authors {
		cached := hotCacheAuthor{Name: item.name, Commits: item.commits, Churn: item.churn, Dirs: item.dirs, First: item.first.Unix(), Last: item.last.Unix(), Timeline: item.timeline}
		if item.person != nil {
			cached.Names, cached.Emails = item.person.names, item.person.emails
		}
		for dir, area := range item.areas {
			cached.Areas = append(cached.Areas, hotCacheArea{Dir: dir, Commits: area.commits, First: area.first.Unix(), Last: area.last.Unix(), Timeline: area.timeline})
		}
		summary.Authors = append(summary.Authors, cached)
	}
	for _, item := range items {
		cached := hotCacheItem{
			Path: item.path, Name: item.name, Branch: item.branch, Directory: item.directory,
			Collapsed: item.collapsed, Score: item.score, Commits: item.commits, People: item.people,
			Churn: item.churn, Last: item.last.Unix(), Owner: item.owner, Class: uint8(item.class),
			Timeline: item.timeline,
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
	// The full slice expression makes appends copy instead of writing into
	// the model's backing array.
	base := m.hotspots.commits[:len(m.hotspots.commits):len(m.hotspots.commits)]
	cutoff := m.hotspots.cutoff
	sortPath, authorFilter := m.hotspots.sortPath, m.hotspots.authorFilter
	// The tree key is filled in by update, which knows the next data version.
	budget := max(hotspotRefresh, 2*m.hotspots.aggregateCost)
	return func() tea.Msg {
		commits, started := base, time.Now()
		for {
			page, done, err := stream.NextPage(limit)
			commits = append(commits, page...)
			if err != nil {
				return hotspotPageLoaded{stream: stream, request: request, done: true, err: err}
			}
			limit = hotspotPageSize
			// The first page paints at once; later ones batch up to the budget.
			if done || len(base) == 0 || time.Since(started) >= budget {
				aggregated := time.Now()
				files, authors, dirs, hidden := aggregateHotspots(commits, builtinPathMarks(commits), cutoff, time.Now())
				root := buildHotTree(files, dirs, sortPath, false, authorFilter)
				return hotspotPageLoaded{
					commits: commits, files: files, authors: authors, dirs: dirs, hidden: hidden,
					root: root, treeKey: hotTreeKey{sortPath: sortPath, authorFilter: authorFilter},
					cost: time.Since(aggregated), stream: stream, request: request, done: done,
				}
			}
		}
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
	// Requests keep counting up so a reply from the window being replaced is
	// never mistaken for one from the new window.
	m.hotspots = hotspotState{loading: true, since: since, author: author, request: m.hotspots.request + 1, authors: make(map[string]*hotAuthor), items: &hotItemsCache{}}
	return m, m.loadHotspots()
}

// switchHotWindow moves to another window, keeping the view and filters, and
// reuses that window's results if they were loaded this session.
func (m model) switchHotWindow(since string) (tea.Model, tea.Cmd) {
	previous := m.hotspots
	mode := previous.mode
	if previous.scope != "" {
		mode = previous.scopeReturn
	}
	next, cmd := m.openHotspots(since, previous.author)
	m = next.(model)
	m.hotspots.mode, m.hotspots.authorFilter, m.hotspots.expanded = mode, previous.authorFilter, previous.expanded
	m.hotspots.collapsed, m.hotspots.opened, m.hotspots.showAll = previous.collapsed, previous.opened, previous.showAll
	m.hotspots.hiddenShown, m.hotspots.sortPath = previous.hiddenShown, previous.sortPath
	m.hotspots.windows = previous.windows
	if m.hotspots.windows == nil {
		m.hotspots.windows = make(map[string]*hotWindow)
	}
	saved := m.hotspots.windows[since]
	if saved == nil || time.Since(saved.at) > hotSummaryTTL {
		return m, cmd
	}
	m.hotspots.loading = false
	m.hotspots.files, m.hotspots.authors, m.hotspots.dirs, m.hotspots.hidden = saved.files, saved.authors, saved.dirs, saved.hidden
	m.hotspots.commits, m.hotspots.streamLoaded = saved.commits, len(saved.commits)
	m.hotspots.dataVersion++
	return m, nil
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
	timeline   hotTimeline
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
	key := hotTreeKey{dataVersion: m.hotspots.dataVersion, sortPath: m.hotspots.sortPath, authorFilter: m.hotspots.authorFilter}
	var root *hotNode
	if cache != nil && cache.root != nil && cache.rootKey == key {
		root = cache.root
	} else {
		root = buildHotTree(m.hotspots.files, m.hotspots.dirs, m.hotspots.sortPath, false, m.hotspots.authorFilter)
		if cache != nil {
			cache.root, cache.rootKey = root, key
		}
	}
	items := flattenHotTree(root, m.hotTreeState())
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

func (m model) hotFoldsDefault() bool {
	return len(m.hotspots.collapsed) == 0 && len(m.hotspots.opened) == 0 && len(m.hotspots.showAll) == 0 &&
		!m.hotspots.sortPath && m.hotspots.authorFilter == ""
}

func (m model) hotTreeState() hotTreeState {
	return hotTreeState{collapsed: m.hotspots.collapsed, opened: m.hotspots.opened, showAll: m.hotspots.showAll}
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
	timeline           hotTimeline
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
	humans := make(map[string]float64, len(owners))
	for name, share := range owners {
		if !isBotAuthor(name) {
			humans[name] = share
		}
	}
	if len(humans) == 0 {
		// A bot-only area has no one else to name, so the bot stays.
		humans = owners
	}
	var total float64
	names := make([]string, 0, len(humans))
	for name, share := range humans {
		total += share
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if humans[names[i]] != humans[names[j]] {
			return humans[names[i]] > humans[names[j]]
		}
		return names[i] < names[j]
	})
	if total <= 0 {
		return nil
	}
	var result []hotOwnerShare
	for _, name := range names[:min(2, len(names))] {
		result = append(result, hotOwnerShare{name: name, share: int(math.Round(humans[name] / total * 100))})
	}
	return result
}

// Bots stay in the author maps so the author filter still reaches their
// commits, but they are not people to ask about an area.
func hotHumanCount[T any](authors map[string]T) int {
	count := 0
	for name := range authors {
		if !isBotAuthor(name) {
			count++
		}
	}
	return count
}

func buildHotItems(files []hotFile, dirs map[string]*hotDirStat, sortPath bool, state hotTreeState, authorFilter string) []hotItem {
	return flattenHotTree(buildHotTree(files, dirs, sortPath, state.showHidden, authorFilter), state)
}

// buildHotTree aggregates files into directories and sorts them. The result
// is read-only afterwards, so models can share it.
func buildHotTree(files []hotFile, dirs map[string]*hotDirStat, sortPath, showHidden bool, authorFilter string) *hotNode {
	if !showHidden {
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
			node.people, node.class = hotHumanCount(node.file.authors), node.file.class
			node.timeline = node.file.timeline
			return node.score, node.commits, node.churn, node.last, node.owner, node.class, node.people, node.owners
		}
		node.class = hotVendored
		owners := make(map[string]float64)
		for _, child := range node.children {
			score, commits, churn, last, _, class, _, childOwners := aggregate(child, depth+1)
			node.score += score
			node.timeline.add(child.timeline)
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
	return root
}

// flattenHotTree lists the rows a fold state shows.
func flattenHotTree(root *hotNode, state hotTreeState) []hotItem {
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
				timeline: node.timeline,
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

// hotspotSequence reviews the window's commits to path, only one person's when
// person is set.
func (m model) hotspotSequence(path, person string) tea.Cmd {
	repo, request := m.repo, m.hotspots.request
	since, author := m.hotspots.since, m.hotspots.author
	var authors []string
	if author != "" {
		authors = []string{author}
	}
	if person != "" {
		// Git ORs repeated --author patterns, and the person is already
		// inside the window's author filter.
		authors = m.hotAuthorPatterns(person)
	}
	return func() tea.Msg {
		args := []string{"log", "-z", "--reverse", "--format=%H"}
		if since != "" {
			args = append(args, "--since="+since)
		}
		for _, author := range authors {
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

var hotLevels = []rune("▁▂▃▄▅▆▇█")

// hotStrip draws a timeline one cell per slice of the window, newest on the
// right. Heights share a log scale across the list so rows compare, and any
// activity at all shows at least the lowest block so one commit is never
// lost. Active cells are drawn at full strength so they stand out of the dim
// row; quiet slices are a faint dot.
func hotStrip(t hotTimeline, peak float64, color string) string {
	return hotDrawStrip(t, peak, color, func(v float64) float64 { return math.Log1p(v) / math.Log1p(peak) })
}

// hotAreaStrip scales heights by square root instead. Within one person a
// log scale lifts every area they touch often to full height, so their
// busiest areas look alike, while a linear one flattens everything but the
// top area; the square root keeps both ends readable.
func hotAreaStrip(t hotTimeline, peak float64, color string) string {
	return hotDrawStrip(t, peak, color, func(v float64) float64 { return math.Sqrt(v / peak) })
}

func hotDrawStrip(t hotTimeline, peak float64, color string, scale func(float64) float64) string {
	var b strings.Builder
	for _, v := range t {
		if v <= 0 || peak <= 0 {
			b.WriteString("·")
			continue
		}
		level := int(math.Ceil(scale(v) * float64(len(hotLevels))))
		level = max(1, min(level, len(hotLevels)))
		b.WriteString(styleNormal + color + string(hotLevels[level-1]) + styleReset)
	}
	return b.String()
}

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
	if n < 0 {
		return "-" + formatCount(-n)
	}
	digits := strconv.Itoa(n)
	if len(digits) <= 3 {
		return digits
	}
	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return grouped.String()
}

func hotOwnersText(owners []hotOwnerShare) string {
	var parts []string
	for _, owner := range owners {
		parts = append(parts, fmt.Sprintf("%s %d%%", owner.name, owner.share))
	}
	return strings.Join(parts, " · ")
}

// hotPeak is the busiest slice across a list, the one scale its strips share
// so a quiet file reads quiet next to a busy directory.
func hotPeak(items []hotItem) float64 {
	peak := 0.0
	for _, item := range items {
		peak = max(peak, item.timeline.peak())
	}
	return peak
}

// hotAgeStyle brightens recent changes so the eye lands on live work: the
// last day in the accent color, the last week at full strength, older left
// dim. A slice of the strip can span a month, so this is what tells three
// minutes from two days.
func (m model) hotAgeStyle(when, now time.Time) string {
	switch age := now.Sub(when); {
	case when.IsZero():
		return ""
	case age < 24*time.Hour:
		return styleNormal + m.accent()
	case age < 7*24*time.Hour:
		return styleNormal
	}
	return ""
}

// hotColumns formats the fixed columns that follow a tree or file name. The
// activity strip and age may carry colors, so they are padded by cell width.
// The owners text comes last and is the first thing cut on a narrow screen.
func hotColumns(activity, commits, people, last, owners string) string {
	text := padCell(activity, hotBuckets) + fmt.Sprintf("  %7s  %6s  ", commits, people) +
		strings.Repeat(" ", max(0, 5-ansi.StringWidth(last))) + last
	if owners != "" {
		text += "  " + owners
	}
	return text
}

// hotFixedWidth is the width of hotColumns without the owners text.
var hotFixedWidth = len(hotColumns("", "", "", "", ""))

// hotItemColumns returns a tree or file row's fixed columns and, apart, the
// owners text that ends it.
func (m model) hotItemColumns(item hotItem, peak float64, now time.Time) (string, string) {
	if item.more {
		return "", ""
	}
	people := ""
	if item.people > 0 {
		people = formatCount(item.people)
	}
	owners := ""
	if m.showHotOwners() {
		owners = syntax.Clean(hotOwnersText(item.owners))
	}
	last := hotAge(item.last, now)
	if style := m.hotAgeStyle(item.last, now); style != "" && last != "" {
		last = style + last + styleReset
	}
	return hotColumns(hotStrip(item.timeline, peak, ""), formatCount(item.commits), people, last, ""), owners
}

func (m model) showHotOwners() bool { return len(m.hotspots.authors) > 1 }

// hotAuthorColumns formats the columns that follow an author's name.
func hotAuthorColumns(activity, commits, active, areas string) string {
	text := padCell(activity, hotBuckets) + fmt.Sprintf("  %7s  %-13s", commits, active)
	if areas != "" {
		text += "  " + areas
	}
	return text
}

// hotAuthorFixedWidth reserves room for the activity and commit count only;
// the active range and areas are cut first on a narrow screen.
var hotAuthorFixedWidth = hotBuckets + len(fmt.Sprintf("  %7s", ""))

// hotBadgeWidth is the initials column before an author's name: two cells and
// a space.
const hotBadgeWidth = 3

// hotAuthorRow renders one author with the same initials and color history
// gives them, and their activity in that color.
func (m model) hotAuthorRow(i, selected, count int, author *hotAuthor, nameWidth int, peak float64, now time.Time) string {
	color, badge := m.authorNameColor(author.name), authorInitials(author.name)
	if isBotAuthor(author.name) {
		// Bots stay dim like the rest of the row.
		color, badge = "", "⚙ "
	}
	areas := ""
	if !m.hotspots.scopeFile {
		areas = syntax.Clean(topAuthorDirs(author, 3))
	}
	columns := hotAuthorColumns(hotStrip(author.timeline, peak, color), formatCount(author.commits), hotActive(author, now), "")
	name := styleNormal + color + badge + styleReset + " " + syntax.Clean(author.name)
	return m.hotRow(i, selected, count, name, nameWidth, columns, areas)
}

func hotActive(author *hotAuthor, now time.Time) string {
	return hotSpan(author.first, author.last, now)
}

// hotSpan is how long ago work began and last happened.
func hotSpan(from, to time.Time, now time.Time) string {
	first, last := hotAge(from, now), hotAge(to, now)
	switch {
	case first == "" && last == "":
		return ""
	case first == last || first == "":
		return last
	}
	return first + " – " + last
}

// hotAuthorEntry is one row of the authors view: an author, or with area set
// one top-level directory they worked in.
type hotAuthorEntry struct {
	author *hotAuthor
	area   string
	last   bool
}

// hotAuthorEntries lists the authors with the expanded ones followed by their
// areas, busiest first.
func (m model) hotAuthorEntries() []hotAuthorEntry {
	authors := m.hotFilteredAuthors()
	entries := make([]hotAuthorEntry, 0, len(authors))
	for _, author := range authors {
		entries = append(entries, hotAuthorEntry{author: author})
		if !m.hotAuthorExpandable() || !m.hotspots.expanded[author.name] {
			continue
		}
		areas := hotAuthorAreas(author)
		for i, area := range areas {
			entries = append(entries, hotAuthorEntry{author: author, area: area, last: i == len(areas)-1})
		}
	}
	return entries
}

// hotAuthorGap reports whether a blank line follows entry i: one separates
// each author, with their areas, from the next.
func hotAuthorGap(entries []hotAuthorEntry, i int) bool {
	return i+1 < len(entries) && entries[i+1].area == ""
}

// hotAuthorViewport is the range of entries that fits in rows lines once the
// gaps between authors are counted, keeping the selection on screen.
func hotAuthorViewport(entries []hotAuthorEntry, list listState, rows int) (int, int) {
	offset, _ := listViewport(list, len(entries), rows)
	if len(entries) == 0 {
		return 0, 0
	}
	selected := min(list.selected, len(entries)-1)
	height := func(i int) int {
		if hotAuthorGap(entries, i) {
			return 2
		}
		return 1
	}
	used := 1
	for i := offset; i < selected; i++ {
		used += height(i)
	}
	for used > rows && offset < selected {
		used -= height(offset)
		offset++
	}
	end, used := offset, 0
	for end < len(entries) && used+1 <= rows {
		used += height(end)
		end++
	}
	return offset, end
}

// A single file has no areas below it to expand into.
func (m model) hotAuthorExpandable() bool { return !m.hotspots.scopeFile }

// hotAuthorAreas lists where an author worked, busiest first. Areas under 1%
// of their commits are left out: they are mostly stray config edits, and
// would bury the areas that say what the person works on.
func hotAuthorAreas(author *hotAuthor) []string {
	areas := make([]string, 0, len(author.areas))
	for dir, area := range author.areas {
		if area.commits*100/max(1, author.commits) >= 1 {
			areas = append(areas, dir)
		}
	}
	sort.Slice(areas, func(i, j int) bool {
		left, right := author.areas[areas[i]], author.areas[areas[j]]
		if left.commits != right.commits {
			return left.commits > right.commits
		}
		return areas[i] < areas[j]
	})
	return areas
}

func hotAreaName(entry hotAuthorEntry) string {
	branch := "├─ "
	if entry.last {
		branch = "└─ "
	}
	name := dirLabel(entry.area)
	if entry.area != "." {
		name += "/"
	}
	return strings.Repeat(" ", hotBadgeWidth) + branch + name
}

// hotAreaPath is the repository path an area row names, inside the scope the
// authors view is limited to.
func (m model) hotAreaPath(area string) string {
	scope := m.hotspots.scope
	switch {
	case area == "." && scope == "":
		return "."
	case area == ".":
		return scope
	case scope == "":
		return area
	}
	return scope + "/" + area
}

// hotAreaRow shows one author's work in one area, in the author's color, on
// the same scale as the author rows.
func (m model) hotAreaRow(i, selected, count int, entry hotAuthorEntry, nameWidth int, now time.Time) string {
	color := m.authorNameColor(entry.author.name)
	if isBotAuthor(entry.author.name) {
		color = ""
	}
	area := entry.author.areas[entry.area]
	share := fmt.Sprintf("%d%% of their commits", area.commits*100/max(1, entry.author.commits))
	// Areas share their author's scale, not the list's, so the strips show
	// how that person's attention split between them.
	columns := hotAuthorColumns(hotAreaStrip(area.timeline, entry.author.timeline.peak(), color), formatCount(area.commits), hotSpan(area.first, area.last, now), "")
	return m.hotRow(i, selected, count, syntax.Clean(hotAreaName(entry)), nameWidth, columns, share)
}

// hotRow draws one row: the name padded to nameWidth, the fixed columns, then
// the free text that ends it. Rows are dim and the selected one is drawn at
// full strength with an accented number and arrow, so badges and strips keep
// their colors instead of turning into blocks under reverse video.
func (m model) hotRow(i, selected, count int, name string, nameWidth int, columns, tail string) string {
	text := padCell(name, nameWidth) + "  " + columns
	if tail != "" {
		text += "  " + tail
	}
	if i != selected {
		return m.styledRow(i, selected, count, text, "")
	}
	numberWidth := len(strconv.Itoa(max(1, count-1)))
	line := fmt.Sprintf("%s%*d ▸ %s%s", m.accent(), numberWidth, 0, styleReset, text)
	return bar(m.width, line, "") + styleReset
}

// hotHeaderRow labels the columns at the same offsets listTableRow uses.
func (m model) hotHeaderRow(count, nameWidth int) string {
	activity := m.hotSpanLabel()
	name, columns := "area", hotColumns(activity, "commits", "people", "last", "")
	if m.showHotOwners() {
		columns += "  owners"
	}
	if m.hotspots.mode == "authors" {
		name, columns = strings.Repeat(" ", hotBadgeWidth)+"author", hotAuthorColumns(activity, "commits", "active", "areas")
	}
	indent := strings.Repeat(" ", listPrefixWidth(count))
	return styleDim + ansi.Truncate(indent+padCell(name, nameWidth)+"  "+columns, m.width, "") + styleReset
}

// hotSpanLabel heads the activity column with how far back its strips reach.
// Every commit has one author, so the earliest first commit among them is
// where the strips start, whether the data came from a stream or the summary.
func (m model) hotSpanLabel() string {
	var start time.Time
	for _, author := range m.hotspots.authors {
		if !author.first.IsZero() && (start.IsZero() || author.first.Before(start)) {
			start = author.first
		}
	}
	if age := hotAge(start, time.Now()); age != "" {
		return age + " → now"
	}
	return "activity"
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
	key := hotFilesKey{dataVersion: m.hotspots.dataVersion, authorFilter: author, hiddenShown: m.hotspots.hiddenShown, query: query}
	cache := m.hotspots.items
	if cache != nil && cache.filesReady && cache.filesKey == key {
		return cache.files
	}
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
			commits: file.commits, people: hotHumanCount(file.authors), churn: file.added + file.deleted,
			last: file.last, owners: hotOwners(owners), timeline: file.timeline,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
	if cache != nil {
		cache.files, cache.filesKey, cache.filesReady = items, key, true
		cache.filesLongest = hotLongestName(items)
	}
	return items
}

// hotFilesLongest is the widest path in the file view, kept with the cached
// rows so sizing the column does not measure every file on each frame.
func (m model) hotFilesLongest() string {
	items := m.hotFileItems()
	if cache := m.hotspots.items; cache != nil && cache.filesReady {
		return cache.filesLongest
	}
	return hotLongestName(items)
}

func hotLongestName(items []hotItem) string {
	longest, width := "", 0
	for _, item := range items {
		if w := ansi.StringWidth(syntax.Clean(item.name)); w > width {
			longest, width = syntax.Clean(item.name), w
		}
	}
	return longest
}

// botWords are the words automation accounts use where a person's name would
// go. They match as whole tokens, so "Robot Human" and "Henry Talbot" stay
// human.
var botWords = map[string]bool{
	"bot": true, "automation": true, "actions": true, "ci": true, "cd": true,
	"build": true, "builder": true, "release": true, "deploy": true,
	"pipeline": true, "scheduler": true, "cron": true,
}

// isBotAuthor reports whether an author is automation rather than a person.
// GitHub Apps carry a [bot] suffix, and CI services use a token such as ci or
// automation. Anything else is a person as far as Git can say.
func isBotAuthor(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "[bot]") {
		return true
	}
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if botWords[word] {
			return true
		}
	}
	return false
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
	title := fmt.Sprintf(" hotspots · %s · %s commits · %s", m.hotWindowLabel(), formatCount(commits), hotPeople(hotHumanCount(m.hotspots.authors)))
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
	if m.hotspots.scope != "" {
		title += " in " + m.hotspots.scope
		if !m.hotspots.scopeFile {
			title += "/"
		}
	}
	lines := []string{m.titleBar(" "+m.accent()+syntax.Clean(title)+styleReset, stateBadges(m.filterBadge(m.hotspots.list.filter)))}
	now := time.Now()
	rows := m.hotRows()
	var items []hotItem
	selected := m.hotspots.list.selected
	var body []string
	var header string
	switch m.hotspots.mode {
	case "authors":
		entries := m.hotAuthorEntries()
		selected = min(selected, max(0, len(entries)-1))
		offset, end := hotAuthorViewport(entries, m.hotspots.list, rows)
		// The column fits every entry, not just the visible ones, so it holds
		// still while scrolling.
		names := make([]string, 0, len(entries))
		pad := strings.Repeat(" ", hotBadgeWidth)
		for _, entry := range entries {
			if entry.area != "" {
				names = append(names, syntax.Clean(hotAreaName(entry)))
			} else {
				names = append(names, pad+syntax.Clean(entry.author.name))
			}
		}
		nameWidth := m.listNameWidth(len(entries), append(names, pad+"author"), hotAuthorFixedWidth)
		header = m.hotHeaderRow(len(entries), nameWidth)
		peak := 0.0
		for _, entry := range entries {
			if entry.area == "" {
				peak = max(peak, entry.author.timeline.peak())
			}
		}
		for i := offset; i < end; i++ {
			if entries[i].area != "" {
				body = append(body, m.hotAreaRow(i, selected, len(entries), entries[i], nameWidth, now))
			} else {
				body = append(body, m.hotAuthorRow(i, selected, len(entries), entries[i].author, nameWidth, peak, now))
			}
			if i+1 < end && hotAuthorGap(entries, i) {
				body = append(body, "")
			}
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
		peak := hotPeak(items)
		// The column fits every row, not just the visible ones, so it holds
		// still while scrolling.
		var names []string
		if m.hotspots.mode == "files" {
			names = []string{m.hotFilesLongest()}
		} else {
			names = make([]string, 0, len(items))
			for _, item := range items {
				names = append(names, syntax.Clean(item.branch+item.name))
			}
		}
		nameWidth := m.listNameWidth(len(items), append(names, "area"), hotFixedWidth)
		header = m.hotHeaderRow(len(items), nameWidth)
		for i := offset; i < end; i++ {
			columns, owners := m.hotItemColumns(items[i], peak, now)
			body = append(body, m.hotRow(i, selected, len(items), syntax.Clean(items[i].branch+items[i].name), nameWidth, columns, owners))
		}
	}
	lines = append(lines, header)
	lines = append(lines, body...)
	if m.hotspots.mode == "authors" && len(m.hotFilteredAuthors()) == 0 && m.hotspots.scopeLoading {
		lines = append(lines, fit(" Loading contributors…", m.width))
	}
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
	lines = append(lines, styleDim+truncateCell(detail, m.width)+styleReset)
	status := ""
	if filter := m.hotspots.list.listStatus(m.width); filter != "" {
		status = filter
	}
	hints := m.hotFooter()
	if m.flash != "" {
		status = syntax.Clean(m.flash)
	}
	lines = append(lines, m.statusBar(status, truncateCell(hints, max(1, m.width-ansi.StringWidth(status)-2))))
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
	actions := []shortcut{hint(view, "hotspots.switch-view"), hint("window", "hotspots.window"), hint(a, "hotspots.show-hidden"), hint("who", "hotspots.contributors")}
	if m.hotspots.mode == "authors" {
		actions = []shortcut{hint(view, "hotspots.switch-view"), hint("window", "hotspots.window"), hint("filter", "open")}
		if m.hotspots.scope != "" {
			actions[2] = hint("history", "open")
		}
		if m.hotAuthorExpandable() {
			actions = append(actions, hint("areas", "unfold"))
		}
	}
	return footer(true, actions...)
}

// dirLabel shows the repository root as a name instead of the "." key the
// per-directory counts use.
func dirLabel(dir string) string {
	if dir == "." {
		return "(root)"
	}
	return dir
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
		parts = append(parts, fmt.Sprintf("%s %d%%", dirLabel(s.dir), s.count*100/max(1, total)))
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
	return dirLabel(top)
}

func (m model) hotAuthorList() []*hotAuthor {
	if m.hotspots.scope != "" {
		return m.hotspots.scopeAuthors
	}
	if cache := m.hotspots.items; cache != nil && cache.authorsReady && cache.authorsVersion == m.hotspots.dataVersion {
		return cache.authors
	}
	authors := make([]*hotAuthor, 0, len(m.hotspots.authors))
	for _, author := range m.hotspots.authors {
		authors = append(authors, author)
	}
	sortHotAuthors(authors)
	if cache := m.hotspots.items; cache != nil {
		cache.authors, cache.authorsVersion, cache.authorsReady = authors, m.hotspots.dataVersion, true
	}
	return authors
}

// sortHotAuthors puts people before bots, then the most commits first.
func sortHotAuthors(authors []*hotAuthor) {
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
}

// loadHotScope finds who changed one path. The commits stay in memory after a
// streamed load; a load from the summary kept none, so they come from the
// per-commit cache instead.
func (m model) loadHotScope(scope string) tea.Cmd {
	commits := append([]git.HotCacheCommit(nil), m.hotspots.commits...)
	root, since, author, request := m.repo.Root, m.hotspots.since, m.hotspots.author, m.hotspots.request
	return func() tea.Msg {
		if len(commits) == 0 {
			if cache, ok := readHotCache(hotCachePath(root, since, author), since, author); ok {
				commits = cache.Commits
			}
		}
		now := time.Now()
		return hotspotScopeLoaded{scope: scope, authors: hotScopeAuthors(commits, scope, advanceHotCutoff(since, now), now), request: request}
	}
}

// hotScopeAuthors aggregates only the changes to scope. Paths are made
// relative to it so an author's areas name its subdirectories, and the window
// keeps the whole repository's start so strips line up with the tree.
func hotScopeAuthors(commits []git.HotCacheCommit, scope string, cutoff int64, now time.Time) []*hotAuthor {
	// No commit falls between the cutoff and the window's start, so the
	// start serves as the cutoff and the scoped strips begin where the
	// tree's do.
	cutoff = hotWindowStart(commits, cutoff)
	var scoped []git.HotCacheCommit
	for _, commit := range commits {
		var changes []git.HotCacheChange
		for _, change := range commit.Changes {
			switch {
			case change.Path == scope:
				change.Path = scope[strings.LastIndex(scope, "/")+1:]
			case strings.HasPrefix(change.Path, scope+"/"):
				change.Path = change.Path[len(scope)+1:]
			default:
				continue
			}
			changes = append(changes, change)
		}
		if len(changes) > 0 {
			commit.Changes = changes
			scoped = append(scoped, commit)
		}
	}
	_, byName, _, _ := aggregateHotspots(scoped, builtinPathMarks(scoped), cutoff, now)
	authors := make([]*hotAuthor, 0, len(byName))
	for _, author := range byName {
		authors = append(authors, author)
	}
	sortHotAuthors(authors)
	return authors
}

func (m model) hotVisible() []hotItem {
	if m.hotspots.mode == "files" {
		return m.hotFileItems()
	}
	return m.hotFilteredItems()
}

func (m model) handleHotspotKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	count := 0
	if m.hotspots.mode == "authors" {
		count = len(m.hotAuthorEntries())
	} else {
		count = len(m.hotVisible())
	}
	p := m.listPress(&m.hotspots.list, key, scopeHotspots)
	if m.handleListKey(&m.hotspots.list, p, count, m.hotRows(), nil) {
		return m, nil
	}
	if next, cmd, ok := m.handleSharedAction(p); ok {
		return next, cmd
	}
	resetItems := func() {
		if m.hotspots.items != nil {
			m.hotspots.items.filled = false
		}
	}
	switch p.action {
	case actBack, actQuit:
		if m.hotspots.scope != "" {
			m.hotspots.mode = m.hotspots.scopeReturn
			m.hotspots.scope, m.hotspots.scopeAuthors = "", nil
			m.hotspots.list.reset()
			break
		}
		if m.hotspots.authorFilter != "" {
			m.hotspots.authorFilter = ""
			m.hotspots.list.reset()
			resetItems()
			break
		}
		m.closeHotspotStream()
		return m.openHistory()
	case actInterrupt:
		m.closeHotspotStream()
		return m, tea.Quit
	case actContributors:
		if m.hotspots.mode == "authors" {
			break
		}
		items := m.hotVisible()
		if len(items) == 0 {
			break
		}
		item := items[min(m.hotspots.list.selected, len(items)-1)]
		if item.more {
			break
		}
		m.hotspots.scope, m.hotspots.scopeFile, m.hotspots.scopeReturn = item.path, !item.directory, m.hotspots.mode
		m.hotspots.scopeAuthors, m.hotspots.scopeLoading = nil, true
		m.hotspots.mode = "authors"
		m.hotspots.list.reset()
		return m, m.loadHotScope(item.path)
	case actSwitchView:
		m.hotspots.scope, m.hotspots.scopeAuthors = "", nil
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
	case actShowHidden:
		m.hotspots.hiddenShown = !m.hotspots.hiddenShown
		m.hotspots.collapsedVersion++
		resetItems()
		if m.hotspots.hiddenShown {
			m.flash = "Showing generated and vendored files"
		} else {
			m.flash = "Hiding generated and vendored files"
		}
	case actWindow:
		return m.switchHotWindow(nextHotWindow(m.hotspots.since))
	case actFold, actUnfold:
		if m.hotspots.mode == "authors" {
			m.foldHotAuthor(p.action == actUnfold)
			break
		}
		items := m.hotVisible()
		if m.hotspots.list.selected < 0 || m.hotspots.list.selected >= len(items) {
			break
		}
		item := items[m.hotspots.list.selected]
		if item.more {
			if m.hotspots.showAll == nil {
				m.hotspots.showAll = make(map[string]bool)
			}
			if p.action == actUnfold {
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
		if p.action == actFold {
			m.hotspots.collapsed[item.path] = true
			delete(m.hotspots.opened, item.path)
		} else {
			delete(m.hotspots.collapsed, item.path)
			m.hotspots.opened[item.path] = true
		}
		m.hotspots.collapsedVersion++
		resetItems()
	case actReviewRange:
		if m.hotspots.mode != "authors" {
			items := m.hotVisible()
			if len(items) > 0 {
				m.closeHotspotStream()
				return m, m.hotspotSequence(items[min(m.hotspots.list.selected, len(items)-1)].path, m.hotspots.authorFilter)
			}
		}
	case actEditor:
		items := m.hotVisible()
		if len(items) > 0 {
			item := items[min(m.hotspots.list.selected, len(items)-1)]
			if !item.directory {
				return m.openFilePath(item.path, 0)
			}
		}
	case actFileHistory:
		items := m.hotVisible()
		if len(items) > 0 {
			item := items[min(m.hotspots.list.selected, len(items)-1)]
			if item.directory {
				return m.openAuthorPathHistory(item.path, m.hotspots.authorFilter)
			}
		}
	case actOpen:
		if m.hotspots.mode == "authors" {
			entries := m.hotAuthorEntries()
			if len(entries) == 0 {
				break
			}
			entry := entries[min(m.hotspots.list.selected, len(entries)-1)]
			// An area, or an author of one path, opens their commits there.
			if entry.area != "" || m.hotspots.scope != "" {
				path := m.hotspots.scope
				if entry.area != "" {
					path = m.hotAreaPath(entry.area)
				}
				m.closeHotspotStream()
				return m.openAuthorPathHistory(path, entry.author.name)
			}
			m.hotspots.authorFilter = entry.author.name
			m.hotspots.scope, m.hotspots.scopeAuthors = "", nil
			m.hotspots.mode = ""
			m.hotspots.list.reset()
			resetItems()
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
		return m.openAuthorPathHistory(item.path, m.hotspots.authorFilter)
	}
	return m, nil
}

// foldHotAuthor opens the selected author into their areas or closes them.
// Folding on an area closes its author and moves the cursor back up to them.
func (m *model) foldHotAuthor(open bool) {
	entries := m.hotAuthorEntries()
	if !m.hotAuthorExpandable() || len(entries) == 0 {
		return
	}
	selected := min(m.hotspots.list.selected, len(entries)-1)
	name := entries[selected].author.name
	if open {
		if m.hotspots.expanded == nil {
			m.hotspots.expanded = make(map[string]bool)
		}
		m.hotspots.expanded[name] = true
		return
	}
	delete(m.hotspots.expanded, name)
	for selected > 0 && entries[selected].area != "" {
		selected--
	}
	m.hotspots.list.selected = selected
	m.hotspots.list.offset = min(m.hotspots.list.offset, selected)
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

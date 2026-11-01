package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const hotHalfLife = 90 * 24 * time.Hour

type hotFile struct {
	path           string
	added, deleted int
	commits        int
	score          float64
	last           time.Time
	authors        map[string]int
}

type hotAuthor struct {
	name    string
	commits int
	churn   int
	dirs    map[string]int
}

type hotspotState struct {
	files        []hotFile
	authors      map[string]*hotAuthor
	collapsed    map[string]bool
	selected     int
	offset       int
	request      int
	loading      bool
	sortPath     bool
	mode         string
	authorFilter string
	since        string
	author       string
}

type hotspotsLoaded struct {
	files   []hotFile
	authors map[string]*hotAuthor
	request int
	err     error
}

type hotspotSequenceLoaded struct {
	commits []commitInfo
	request int
	err     error
}

func hotSinceDefault() string { return "1.year" }

// hotspots runs one streamed git log over the window and folds numstat into
// per-file and per-author churn.
func (r repository) hotspots(since, author string) ([]hotFile, map[string]*hotAuthor, error) {
	head := ""
	if out, err := r.command("rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		head = strings.TrimSpace(string(out))
	}
	cachePath := hotCachePath(r.root, since, author)
	if files, authors, ok := readHotCache(cachePath, head); ok {
		return files, authors, nil
	}
	args := []string{"log", "--numstat", "-M", "-z", "--format=%x00%H%x00%at%x00%aN"}
	if since != "" {
		args = append(args, "--since="+since)
	}
	if author != "" {
		args = append(args, "--author="+author)
	}
	args = append(args, "--")
	out, err := r.commandStdout(args...)
	if err != nil {
		return nil, nil, gitError(out, err)
	}
	files := make(map[string]*hotFile)
	authors := make(map[string]*hotAuthor)
	now := time.Now()
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); {
		if len(fields[i]) != 0 {
			i++
			continue
		}
		if i+3 >= len(fields) {
			break
		}
		seconds, _ := strconv.ParseInt(string(fields[i+2]), 10, 64)
		authorName := string(fields[i+3])
		i += 4
		when := time.Unix(seconds, 0)
		age := max(0, now.Sub(when).Seconds())
		weight := math.Pow(0.5, age/hotHalfLife.Seconds())
		author := authors[authorName]
		if author == nil {
			author = &hotAuthor{name: authorName, dirs: make(map[string]int)}
			authors[authorName] = author
		}
		author.commits++
		seen := make(map[string]bool)
		for i < len(fields) && len(fields[i]) != 0 {
			line := strings.TrimPrefix(string(fields[i]), "\n")
			i++
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			path := parts[2]
			if path == "" && i+1 < len(fields) {
				// A rename record carries the old and new paths separately.
				path = string(fields[i+1])
				i += 2
			}
			if path == "" {
				continue
			}
			added, addErr := strconv.Atoi(parts[0])
			deleted, delErr := strconv.Atoi(parts[1])
			if addErr != nil || delErr != nil {
				continue
			}
			file := files[path]
			if file == nil {
				file = &hotFile{path: path, authors: make(map[string]int)}
				files[path] = file
			}
			file.added += added
			file.deleted += deleted
			file.score += float64(added+deleted) * weight
			if when.After(file.last) {
				file.last = when
			}
			file.authors[authorName] += added + deleted
			author.churn += added + deleted
			if dir := topDir(path); dir != "" {
				author.dirs[dir]++
			}
			if !seen[path] {
				file.commits++
				seen[path] = true
			}
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	generated := r.generatedPaths(paths)
	result := make([]hotFile, 0, len(files))
	for _, file := range files {
		if generated[file.path] {
			continue
		}
		result = append(result, *file)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].score > result[j].score })
	writeHotCache(cachePath, head, since, author, result, authors)
	return result, authors, nil
}

func topDir(path string) string {
	if i := strings.Index(path, "/"); i > 0 {
		return path[:i]
	}
	return "."
}

type hotCache struct {
	Head    string           `json:"head"`
	Since   string           `json:"since"`
	Author  string           `json:"author"`
	Files   []hotCacheFile   `json:"files"`
	Authors []hotCacheAuthor `json:"authors"`
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
}

func hotCachePath(root, since, author string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(root + "\x00" + since + "\x00" + author))
	return filepath.Join(dir, "pane", "hot-"+hex.EncodeToString(sum[:8]))
}

func readHotCache(path, head string) ([]hotFile, map[string]*hotAuthor, bool) {
	if path == "" || head == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var cache hotCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Head != head {
		return nil, nil, false
	}
	files := make([]hotFile, len(cache.Files))
	for i, file := range cache.Files {
		files[i] = hotFile{path: file.Path, added: file.Added, deleted: file.Deleted, commits: file.Commits, score: file.Score, last: time.Unix(file.Last, 0), authors: file.Authors}
	}
	authors := make(map[string]*hotAuthor, len(cache.Authors))
	for _, author := range cache.Authors {
		authors[author.Name] = &hotAuthor{name: author.Name, commits: author.Commits, churn: author.Churn, dirs: author.Dirs}
	}
	return files, authors, true
}

func writeHotCache(path, head, since, author string, files []hotFile, authors map[string]*hotAuthor) {
	if path == "" {
		return
	}
	cache := hotCache{Head: head, Since: since, Author: author}
	for _, file := range files {
		cache.Files = append(cache.Files, hotCacheFile{Path: file.path, Added: file.added, Deleted: file.deleted, Commits: file.commits, Score: file.score, Last: file.last.Unix(), Authors: file.authors})
	}
	for _, item := range authors {
		cache.Authors = append(cache.Authors, hotCacheAuthor{Name: item.name, Commits: item.commits, Churn: item.churn, Dirs: item.dirs})
	}
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
		files, authors, err := repo.hotspots(since, author)
		return hotspotsLoaded{files: files, authors: authors, request: request, err: err}
	}
}

func (m model) openHotspots(since, author string) (tea.Model, tea.Cmd) {
	m.screen = "hotspots"
	m.hotspots = hotspotState{loading: true, since: since, author: author, request: 1, authors: make(map[string]*hotAuthor)}
	return m, m.loadHotspots()
}

type hotNode struct {
	name, path string
	file       *hotFile
	children   []*hotNode
	byName     map[string]*hotNode
	score      float64
	commits    int
	churn      int
	last       time.Time
	author     string
}

type hotItem struct {
	path, name, branch string
	directory          bool
	collapsed          bool
	file               *hotFile
	score              float64
	commits, churn     int
	last               time.Time
	author             string
}

func (m model) hotItems() []hotItem {
	files := m.hotspots.files
	if filter := m.hotspots.authorFilter; filter != "" {
		var filtered []hotFile
		for _, file := range files {
			if file.authors[filter] > 0 {
				filtered = append(filtered, file)
			}
		}
		files = filtered
	}
	root := &hotNode{}
	for _, file := range files {
		parts := strings.Split(filepathToSlash(file.path), "/")
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
	var aggregate func(*hotNode) (float64, int, int, time.Time, string)
	aggregate = func(node *hotNode) (float64, int, int, time.Time, string) {
		if node.file != nil {
			top := ""
			for name, churn := range node.file.authors {
				if top == "" || churn > node.file.authors[top] {
					top = name
				}
			}
			node.score, node.commits, node.churn, node.last, node.author = node.file.score, node.file.commits, node.file.added+node.file.deleted, node.file.last, top
			return node.score, node.commits, node.churn, node.last, node.author
		}
		authors := make(map[string]int)
		for _, child := range node.children {
			score, commits, churn, last, author := aggregate(child)
			node.score += score
			node.commits += commits
			node.churn += churn
			if last.After(node.last) {
				node.last = last
			}
			if author != "" {
				authors[author] += churn
			}
		}
		for name, churn := range authors {
			if node.author == "" || churn > authors[node.author] {
				node.author = name
			}
		}
		return node.score, node.commits, node.churn, node.last, node.author
	}
	aggregate(root)
	var sortNodes func(*hotNode)
	sortNodes = func(node *hotNode) {
		sort.SliceStable(node.children, func(i, j int) bool {
			if m.hotspots.sortPath {
				return node.children[i].name < node.children[j].name
			}
			if node.children[i].score != node.children[j].score {
				return node.children[i].score > node.children[j].score
			}
			return node.children[i].name < node.children[j].name
		})
		for _, child := range node.children {
			sortNodes(child)
		}
	}
	sortNodes(root)
	var items []hotItem
	var appendChildren func(*hotNode, string)
	appendChildren = func(parent *hotNode, prefix string) {
		for i, node := range parent.children {
			last := i == len(parent.children)-1
			branch, childPrefix := "├─ ", prefix+"│  "
			if last {
				branch, childPrefix = "└─ ", prefix+"   "
			}
			item := hotItem{
				path: node.path, name: node.name, branch: prefix + branch, directory: node.file == nil,
				collapsed: m.hotspots.collapsed[node.path], file: node.file,
				score: node.score, commits: node.commits, churn: node.churn, last: node.last, author: node.author,
			}
			items = append(items, item)
			if item.directory && !item.collapsed {
				appendChildren(node, childPrefix)
			}
		}
	}
	appendChildren(root, "")
	return items
}

func filepathToSlash(path string) string { return strings.ReplaceAll(path, "\\", "/") }

func (m model) hotspotSequence(path string) tea.Cmd {
	repo, request := m.repo, m.hotspots.request
	since, author := m.hotspots.since, m.hotspots.author
	return func() tea.Msg {
		args := []string{"log", "-z", "--format=%H"}
		if since != "" {
			args = append(args, "--since="+since)
		}
		if author != "" {
			args = append(args, "--author="+author)
		}
		args = append(args, "--", path)
		out, err := repo.commandStdout(args...)
		if err != nil {
			return hotspotSequenceLoaded{request: request, err: gitError(out, err)}
		}
		var commits []commitInfo
		for _, field := range bytes.Split(out, []byte{0}) {
			hash := strings.TrimSpace(string(field))
			if hash == "" {
				continue
			}
			if len(commits) == maxSequenceCommits {
				break
			}
			commits = append(commits, commitInfo{hash: hash})
		}
		return hotspotSequenceLoaded{commits: commits, request: request}
	}
}

func (m model) hotViewport(count int) (int, int) {
	available := max(1, m.height-2)
	selected := min(m.hotspots.selected, max(0, count-1))
	offset := m.hotspots.offset
	if selected < offset {
		offset = selected
	}
	if selected >= offset+available {
		offset = selected - available + 1
	}
	return min(offset, count), available
}

func hotBar(score, maxScore float64, width int) string {
	if maxScore <= 0 {
		return strings.Repeat("░", width)
	}
	filled := int(math.Round(score / maxScore * float64(width)))
	filled = max(0, min(filled, width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func (m model) hotView() string {
	title := "hotspots"
	if m.hotspots.authorFilter != "" {
		title += " · " + m.hotspots.authorFilter
	}
	if m.hotspots.author != "" {
		title += " · author " + m.hotspots.author
	}
	lines := []string{m.titleBar(" "+m.accent()+clean(title)+styleReset, styleDim+"read-only"+styleReset)}
	now := time.Now()
	if m.hotspots.mode == "authors" {
		authors := m.hotAuthorList()
		offset, visible := m.hotViewport(len(authors))
		for i := offset; i < min(len(authors), offset+visible); i++ {
			author := authors[i]
			marker := "  "
			if i == m.hotspots.selected {
				marker = "▸ "
			}
			dir := topAuthorDir(author)
			row := fit(fmt.Sprintf("%s%s  %d commits  %d churn  %s", marker, clean(author.name), author.commits, author.churn, dir), m.width)
			if i == m.hotspots.selected {
				row = "\x1b[7m" + row + strings.Repeat(" ", max(0, m.width-stringWidth(row))) + styleReset
			} else {
				row = styleDim + row + styleReset
			}
			lines = append(lines, row)
		}
	} else {
		items := m.hotItems()
		maxScore := 0.0
		for _, item := range items {
			if item.directory {
				maxScore = max(maxScore, item.score)
			}
		}
		if maxScore == 0 {
			for _, item := range items {
				maxScore = max(maxScore, item.score)
			}
		}
		offset, visible := m.hotViewport(len(items))
		if len(items) == 0 {
			text := " No changes in the window"
			if m.hotspots.loading {
				text = " Loading hotspots…"
			}
			lines = append(lines, fit(text, m.width))
		}
		for i := offset; i < min(len(items), offset+visible); i++ {
			item := items[i]
			marker := "  "
			if i == m.hotspots.selected {
				marker = "▸ "
			}
			name := item.branch + clean(item.name)
			if item.directory {
				arrow := "▾ "
				if item.collapsed {
					arrow = "▸ "
				}
				name += arrow + "/"
			}
			meta := hotBar(item.score, maxScore, 6) + fmt.Sprintf(" %d↻ %d±", item.commits, item.churn)
			if !item.last.IsZero() {
				meta += " " + historyCommitTime(item.last.Format(time.RFC3339), now)
			}
			if item.author != "" {
				meta += " " + clean(item.author)
			}
			row := fit(fmt.Sprintf("%s%s  %s", marker, name, meta), m.width)
			if i == m.hotspots.selected {
				row = "\x1b[7m" + row + strings.Repeat(" ", max(0, m.width-stringWidth(row))) + styleReset
			} else {
				row = styleDim + row + styleReset
			}
			lines = append(lines, row)
		}
	}
	status, hints := "", keyHints("tab", "authors", "s", "sort", "↵", "open", "esc", "back")
	if m.hotspots.mode == "authors" {
		hints = keyHints("tab", "files", "↵", "filter", "esc", "back")
	}
	if m.flash != "" {
		status = clean(m.flash)
	}
	lines = append(lines, m.statusBar(status, hints))
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
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
	authors := make([]*hotAuthor, 0, len(m.hotspots.authors))
	for _, author := range m.hotspots.authors {
		authors = append(authors, author)
	}
	sort.Slice(authors, func(i, j int) bool {
		if authors[i].commits != authors[j].commits {
			return authors[i].commits > authors[j].commits
		}
		return authors[i].name < authors[j].name
	})
	return authors
}

func (m model) handleHotspotKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		return m.openHistory()
	case "ctrl+c":
		return m, tea.Quit
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "tab":
		if m.hotspots.mode == "authors" {
			m.hotspots.mode = ""
		} else {
			m.hotspots.mode = "authors"
		}
		m.hotspots.selected, m.hotspots.offset = 0, 0
	case "s":
		m.hotspots.sortPath = !m.hotspots.sortPath
	case "j", "down":
		m.hotspots.selected++
	case "k", "up":
		m.hotspots.selected = max(0, m.hotspots.selected-1)
	case "g", "home":
		m.hotspots.selected = 0
	case "G", "end":
		if m.hotspots.mode == "authors" {
			m.hotspots.selected = max(0, len(m.hotAuthorList())-1)
		} else {
			m.hotspots.selected = max(0, len(m.hotItems())-1)
		}
	case "h", "l":
		if m.hotspots.mode == "" {
			items := m.hotItems()
			if m.hotspots.selected >= 0 && m.hotspots.selected < len(items) && items[m.hotspots.selected].directory {
				if m.hotspots.collapsed == nil {
					m.hotspots.collapsed = make(map[string]bool)
				}
				m.hotspots.collapsed[items[m.hotspots.selected].path] = key.String() == "h"
			}
		}
	case "V":
		if m.hotspots.mode == "" {
			items := m.hotItems()
			if len(items) > 0 {
				return m, m.hotspotSequence(items[min(m.hotspots.selected, len(items)-1)].path)
			}
		}
	case "enter":
		if m.hotspots.mode == "authors" {
			authors := m.hotAuthorList()
			if len(authors) > 0 {
				m.hotspots.authorFilter = authors[min(m.hotspots.selected, len(authors)-1)].name
				m.hotspots.mode = ""
				m.hotspots.selected, m.hotspots.offset = 0, 0
			}
			break
		}
		items := m.hotItems()
		if len(items) == 0 {
			break
		}
		item := items[min(m.hotspots.selected, len(items)-1)]
		if item.directory {
			if m.hotspots.collapsed == nil {
				m.hotspots.collapsed = make(map[string]bool)
			}
			m.hotspots.collapsed[item.path] = !item.collapsed
			break
		}
		return m.openPathHistory(item.path)
	}
	return m, nil
}

func (m model) hotCount() int {
	if m.hotspots.mode == "authors" {
		return len(m.hotAuthorList())
	}
	return len(m.hotItems())
}

package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

type sequenceState struct {
	commits         []git.CommitInfo
	changes         [][]git.Change
	states          []sequenceLoadState
	errors          []string
	rowCache        [][]row
	gutterWidths    []int
	cacheWidth      int
	cacheWrap       bool
	cacheShortPaths bool
	loading         bool
	request         int
	endPending      bool
	cancel          context.CancelFunc
	streamContext   context.Context
	streamTotal     []int
	streamLoaded    []int
}

type sequenceLoadState uint8

const maxSequenceCommits = 1000

// Sequence input is capped before any index is stored in a row.
const _ uint16 = maxSequenceCommits - 1

const (
	sequencePending sequenceLoadState = iota
	sequenceLoading
	sequenceReady
	sequenceFailed
)

type sequenceLoaded struct {
	index   int
	commit  git.CommitInfo
	changes []git.Change
	request int
	err     error
}

func initialSequence(repo git.Repo, input io.Reader) (model, error) {
	scanner := bufio.NewScanner(io.LimitReader(input, 1024*1024+1))
	var commits []git.CommitInfo
	total := 0
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > 1024*1024 {
			return model{}, fmt.Errorf("commit input exceeds 1 MiB")
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		hash := fields[0]
		if len(hash) < 4 || len(hash) > 64 || strings.Trim(hash, "0123456789abcdefABCDEF") != "" {
			return model{}, fmt.Errorf("invalid commit hash %q in input; use git log --oneline", hash)
		}
		if len(commits) >= maxSequenceCommits {
			return model{}, fmt.Errorf("commit input exceeds %d commits", maxSequenceCommits)
		}
		commits = append(commits, git.CommitInfo{Hash: hash})
	}
	if err := scanner.Err(); err != nil {
		return model{}, fmt.Errorf("reading commit input: %w", err)
	}
	if len(commits) == 0 {
		return model{}, fmt.Errorf("no commits in input; use git log --oneline | pane")
	}
	return newSequenceModel(repo, commits)
}

// newSequenceModel starts the multi-commit review for already parsed commits.
func newSequenceModel(repo git.Repo, commits []git.CommitInfo) (model, error) {
	if len(commits) == 0 {
		return model{}, fmt.Errorf("no commits in input; use git log --oneline | pane")
	}
	if len(commits) > maxSequenceCommits {
		return model{}, fmt.Errorf("commit input exceeds %d commits", maxSequenceCommits)
	}
	state := sequenceState{commits: commits, changes: make([][]git.Change, len(commits)), states: make([]sequenceLoadState, len(commits)), errors: make([]string, len(commits)), loading: true, request: 1}
	state.states[0] = sequenceLoading
	state.states[len(commits)-1] = sequenceLoading
	return model{
		repo: repo, wrap: true, loading: true, startup: true, screen: "sequence",
		sequence: state, viewed: newViewedStore(repo.Root),
	}, nil
}

type sequenceChunkLoaded struct {
	index   int
	request int
	chunk   git.CommitChunk
	stream  <-chan git.CommitChunk
}

func waitSequenceChunk(stream <-chan git.CommitChunk, index, request int) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-stream
		if !ok {
			chunk = git.CommitChunk{Done: true, Err: fmt.Errorf("commit stream ended unexpectedly")}
		}
		return sequenceChunkLoaded{index: index, request: request, chunk: chunk, stream: stream}
	}
}

// startSequenceStream opens a commit inside a range and delivers its files in
// chunks, so a huge commit does not block the whole review.
func (m *model) startSequenceStream(index int) tea.Cmd {
	if index < 0 || index >= len(m.sequence.commits) {
		return nil
	}
	if m.sequence.streamContext == nil {
		m.sequence.streamContext, m.sequence.cancel = context.WithCancel(context.Background())
	}
	repo, hash := m.repo, m.sequence.commits[index].Hash
	repo.Context = m.sequence.streamContext
	ctx := m.sequence.streamContext
	request, height := m.sequence.request, m.height
	return func() tea.Msg {
		stream := repo.StreamCommit(ctx, hash, max(height, git.StartupSyntaxLines))
		return waitSequenceChunk(stream, index, request)()
	}
}

// resetSequenceStream cancels a running range load.
func (m *model) resetSequenceStream() {
	if m.sequence.cancel != nil {
		m.sequence.cancel()
	}
	m.sequence.cancel, m.sequence.streamContext = nil, nil
}

func (m *model) nextSequenceLoad() tea.Cmd {
	for i, state := range m.sequence.states {
		if state == sequencePending {
			m.sequence.states[i] = sequenceLoading
			return m.startSequenceStream(i)
		}
	}
	m.sequence.loading = false
	for _, state := range m.sequence.states {
		if state == sequenceLoading {
			m.sequence.loading = true
			break
		}
	}
	return nil
}

func (m model) sequenceIdentity() string {
	if len(m.sequence.commits) == 0 {
		return ""
	}
	// Key viewed marks by (newest, oldest) so reversing the review order keeps
	// existing marks and the key has the same shape as a Git range.
	return "range:" + m.sequence.commits[len(m.sequence.commits)-1].Hash + ".." + m.sequence.commits[0].Hash
}

func (m *model) rebuildSequence() {
	var anchor row
	var searchAnchor row
	hasSearchAnchor := m.modal == "search" && m.search.origin >= 0 && m.search.origin < len(m.rows)
	if hasSearchAnchor {
		searchAnchor = m.rows[m.search.origin]
	}
	hasAnchor := m.pos >= 0 && m.pos < len(m.rows)
	if hasAnchor {
		anchor = m.rows[m.pos]
	}
	oldPos, oldTop := m.pos, m.viewTop
	width := m.diffContentWidth()
	if len(m.sequence.rowCache) != len(m.sequence.commits) || m.sequence.cacheWidth != width || m.sequence.cacheWrap != m.wrap || m.sequence.cacheShortPaths != m.shortPaths {
		m.sequence.rowCache = make([][]row, len(m.sequence.commits))
		m.sequence.gutterWidths = make([]int, len(m.sequence.commits))
		m.sequence.cacheWidth, m.sequence.cacheWrap = width, m.wrap
		m.sequence.cacheShortPaths = m.shortPaths
	}
	if len(m.sequence.gutterWidths) != len(m.sequence.commits) {
		m.sequence.gutterWidths = make([]int, len(m.sequence.commits))
	}
	var rows []row
	now := time.Now()
	for i := range m.sequence.commits {
		if len(m.sequence.states) > 0 && m.sequence.states[i] == sequenceFailed {
			rows = append(rows, row{kind: '@', text: "  " + syntax.Clean(m.sequence.errors[i]), commitIndex: uint16(i)})
			continue
		}
		if i >= len(m.sequence.changes) || (len(m.sequence.states) > 0 && m.sequence.states[i] != sequenceReady) {
			rows = append(rows, row{kind: '@', text: "  Loading commit…", commitIndex: uint16(i)})
			continue
		}
		files := m.sequence.rowCache[i]
		if files == nil {
			expanded := make(map[string]bool, len(m.sequence.changes[i]))
			for _, c := range m.sequence.changes[i] {
				expanded[c.Path] = true
			}
			view := *m
			view.contextCommit = m.sequence.commits[i].Hash
			view.contextBase, view.staged = false, false
			view.viewIdentity = m.sequenceIdentity()
			view.collapseGenerated(m.sequence.changes[i])
			view.seedViewedCollapse(m.sequence.changes[i])
			m.collapsedFiles, m.generatedHandled = view.collapsedFiles, view.generatedHandled
			changes := m.applySyntaxDemand(git.Context{Commit: m.sequence.commits[i].Hash}, m.sequence.changes[i])
			files, m.sequence.gutterWidths[i] = buildRowsDisplay(changes, width, m.wrap, expanded, 0, m.shortPaths)
			if len(files) == 0 {
				files = append(files, row{kind: '@', text: "  No file changes"})
			}
			block := commitHeaderBlock(m.sequence.commits[i], width, m.history.headerCollapsed, now)
			if len(block) > 0 && m.sequence.commits[i].Author != "" {
				files = append(block, files...)
			}
			view.contextCommit = m.sequence.commits[i].Hash
			view.applyViewed(files, changes)
			files = view.insertNoteRows(files)
			files = view.foldContextFiles(files)
			for j := range files {
				files[j].commitIndex = uint16(i)
			}
			m.sequence.rowCache[i] = files
		} else if m.sequence.gutterWidths[i] == 0 {
			for _, c := range m.sequence.changes[i] {
				m.sequence.gutterWidths[i] = max(m.sequence.gutterWidths[i], lineNumberWidth(strings.Split(c.Diff, "\n")))
			}
		}
		rows = append(rows, files...)
	}
	indexRowHeaders(rows)
	m.rows = rows
	if hasSearchAnchor {
		found := false
		for i, r := range rows {
			if r.commitIndex == searchAnchor.commitIndex && r.path == searchAnchor.path && r.kind == searchAnchor.kind && r.lineNumber() == searchAnchor.lineNumber() && r.offset == searchAnchor.offset {
				m.search.origin = i
				found = true
				break
			}
		}
		if !found && searchAnchor.kind == '@' {
			for i, r := range rows {
				if r.commitIndex == searchAnchor.commitIndex {
					m.search.origin = i
					break
				}
			}
		}
	}
	m.pos = 0
	if hasAnchor {
		best, bestDistance := -1, 0
		for i, r := range rows {
			if r.commitIndex != anchor.commitIndex || r.path != anchor.path || r.kind != anchor.kind {
				continue
			}
			if anchor.path == "" {
				// Details and placeholder rows all sit on line 0, so their text
				// is the only identity that keeps the cursor on the same row.
				if r.text != anchor.text {
					continue
				}
			} else if r.lineNumber() != anchor.lineNumber() || r.offset > anchor.offset {
				continue
			}
			distance := i - oldPos
			if distance < 0 {
				distance = -distance
			}
			if best < 0 || distance < bestDistance {
				best, bestDistance = i, distance
			}
		}
		if best >= 0 {
			m.pos = best
		} else if anchor.kind == '@' {
			for i, r := range rows {
				if r.commitIndex == anchor.commitIndex {
					m.pos = i
					break
				}
			}
		}
		m.viewTop = max(0, oldTop+m.pos-oldPos)
	}
	if m.sequence.endPending && len(m.sequence.states) > 0 && m.sequence.states[len(m.sequence.states)-1] >= sequenceReady {
		m.pos = max(0, len(rows)-1)
		m.sequence.endPending = false
		m.viewTop = max(0, m.pos-max(1, m.height-2)+1)
	}
	m.clamp()
	m.viewTop = m.viewportStart(max(1, m.height-2))
}

func (m model) sequenceView() string {
	view := m
	view.screen = ""
	view.sequenceGutters = true
	view.relativeLines = true
	view.viewTitle = " "
	if len(m.rows) > 0 && len(m.sequence.commits) > 0 {
		start := m.viewportStart(max(1, m.height-2))
		index := int(m.rows[start].commitIndex)
		if index < len(m.sequence.commits) {
			commit := m.sequence.commits[index]
			view.viewTitle = m.accent() + shortHash(commit.Hash) + styleReset + "  " + syntax.Clean(commit.Subject)
			view.viewBadge = styleDim + fmt.Sprintf("read-only · %d/%d", index+1, len(m.sequence.commits)) + styleReset
			if viewed, total := viewedCountRows(m.rows); viewed > 0 {
				view.viewBadge += styleDim + fmt.Sprintf(" · %d/%d viewed", viewed, total) + styleReset
			}
		}
	}
	view.viewHints = keyHints("esc", "back", "?", "help")
	view.message = ""
	if m.sequence.loading {
		view.message = "Loading commits…"
		loaded := 0
		for _, state := range m.sequence.states {
			if state == sequenceReady {
				loaded++
			}
		}
		view.searchProgress = fmt.Sprintf("searching %d/%d loaded", loaded, len(m.sequence.commits))
	}
	return view.View()
}

func (m model) handleSequenceKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "G" && m.pending == "" && m.count == 0 && len(m.sequence.states) > 0 {
		last := m.sequence.states[len(m.sequence.states)-1]
		m.sequence.endPending = last == sequencePending || last == sequenceLoading
	} else if key.String() != "G" {
		m.sequence.endPending = false
	}
	if m.pending == "" && (key.String() == "h" || key.String() == "l") && m.onCommitHeader() {
		m.history.headerCollapsed = key.String() == "h"
		m.sequence.rowCache = nil
		m.rebuildSequence()
		return m, nil
	}
	if m.pending == "" && (key.String() == "h" || key.String() == "l") && m.pos >= 0 && m.pos < len(m.rows) && m.rows[m.pos].kind == 'f' {
		index := int(m.rows[m.pos].commitIndex)
		if index < len(m.sequence.commits) {
			if m.collapsedFiles == nil {
				m.collapsedFiles = make(map[git.Context]bool)
			}
			if m.generatedHandled == nil {
				m.generatedHandled = make(map[git.Context]bool)
			}
			fileKey := git.Context{Commit: m.sequence.commits[index].Hash, Path: m.rows[m.pos].path}
			m.generatedHandled[fileKey] = true
			if key.String() == "h" {
				m.collapsedFiles[fileKey] = true
			} else {
				delete(m.collapsedFiles, fileKey)
			}
			m.sequence.rowCache[index] = nil
			m.rebuildSequence()
			return m, nil
		}
	}
	if m.handleDiffNavigation(key.String()) {
		return m, nil
	}
	switch key.String() {
	case "/":
		m.startSearch()
	case "v":
		return m.toggleViewedSequence()
	case "M":
		if cmd := m.copyNotes(); cmd != nil {
			return m, cmd
		}
		m.flash = "No notes in this view"
		return m, nil
	case "o":
		return m.openEditor()
	case "y":
		if len(m.rows) > 0 {
			index := int(m.rows[m.pos].commitIndex)
			if index < len(m.sequence.commits) {
				hash := m.sequence.commits[index].Hash
				return m, copyCmd(hash, shortHash(hash))
			}
		}
	case "Y":
		if m.pos >= 0 && m.pos < len(m.rows) {
			index := int(m.rows[m.pos].commitIndex)
			if index < len(m.sequence.commits) {
				return m, m.copyDiffLocation(m.sequence.commits[index].Hash)
			}
		}
	case "w":
		m.wrap = !m.wrap
		m.horizontal = 0
		m.flash = wrapFlash(m.wrap)
		m.rebuildSequence()
	case "?":
		m.modal = "help"
		m.helpPos = 0
	case "f":
		m.openPicker()
	case "e":
		m.flash = "Not available in multi-commit review"
	case "esc", "q":
		if m.pickerReturn {
			m.restorePicker()
			return m, nil
		}
		return m.leaveSequence()
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) leaveSequence() (tea.Model, tea.Cmd) {
	m.screen = ""
	m.rows = nil
	m.pos, m.viewTop = 0, 0
	m.rebuild()
	return m.openHistory()
}

package ui

import (
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
	"os"
	"sort"
	"strings"
)

// movedDetectionEnabled gates the lazy moved-block pass. The synthetic
// harness measures moved_index_ns before the feature is relied on for large
// commits; PANE_MOVED=0 turns it off for an A/B run.
var movedDetectionEnabled = os.Getenv("PANE_MOVED") != "0"

type movedRange struct {
	start, end int
	hash       uint64
	// path is the removed side's path, used by the moved-from hint.
	path string
}

type movedIndex struct {
	added       map[string][]movedRange
	removed     map[string][]movedRange
	addedHashes map[uint64]bool
	removedOne  map[uint64]movedRange
}

func newMovedIndex() *movedIndex {
	return &movedIndex{
		added:       make(map[string][]movedRange),
		removed:     make(map[string][]movedRange),
		addedHashes: make(map[uint64]bool),
		removedOne:  make(map[uint64]movedRange),
	}
}

func (idx *movedIndex) addChanges(changes []git.Change) {
	if idx == nil || !movedDetectionEnabled {
		return
	}
	for _, c := range changes {
		idx.addChange(c)
	}
}

func (idx *movedIndex) addChange(c git.Change) {
	if idx == nil || !movedDetectionEnabled {
		return
	}
	removedPath := c.Path
	if c.OldPath != "" {
		removedPath = c.OldPath
	}
	oldLine, newLine := 0, 0
	inHunk := false
	var kind byte
	var count int
	var hash uint64
	var pending []string
	start, path := 0, ""
	flush := func() {
		if kind != 0 && count >= 3 {
			block := movedRange{start: start, end: start + count - 1, hash: hash, path: path}
			if kind == '+' {
				idx.added[c.Path] = append(idx.added[c.Path], block)
				idx.addedHashes[hash] = true
			} else {
				idx.removed[c.Path] = append(idx.removed[c.Path], block)
				if _, ok := idx.removedOne[hash]; !ok {
					idx.removedOne[hash] = block
				}
			}
		}
		count = 0
		kind = 0
		pending = pending[:0]
	}
	for rest := c.Diff; rest != ""; {
		line, next, _ := strings.Cut(rest, "\n")
		rest = next
		if match := syntax.MatchHunkHeader(line); match != nil {
			flush()
			oldLine, _ = atoiHeader(match[1])
			newLine, _ = atoiHeader(match[3])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			flush()
			continue
		}
		lineKind := line[0]
		if lineKind != '+' && lineKind != '-' && lineKind != ' ' {
			flush()
			continue
		}
		if lineKind == ' ' {
			flush()
			oldLine++
			newLine++
			continue
		}
		if lineKind != kind {
			flush()
			kind = lineKind
			path = c.Path
			if kind == '-' {
				start = oldLine
				path = removedPath
			} else {
				start = newLine
			}
		}
		// A block needs at least three lines, so hold the first two and only
		// start hashing once a third arrives. Most one- and two-line changes
		// are never candidates, including very long minified lines.
		text := line[1:]
		if count < 3 {
			pending = append(pending, text)
			count++
			if count == 3 {
				hash = movedHashOffset
				for _, held := range pending {
					hash = movedHashLine(hash, held)
				}
				pending = pending[:0]
			}
		} else {
			hash = movedHashLine(hash, text)
			count++
		}
		if kind == '+' {
			newLine++
		} else {
			oldLine++
		}
	}
	flush()
}

func atoiHeader(value string) (int, bool) {
	n := 0
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
		n = n*10 + int(value[i]-'0')
	}
	return n, value != ""
}

// FNV-1a constants for the incremental moved-block hash.
const (
	movedHashOffset = 14695981039346656037
	movedHashPrime  = 1099511628211
)

// movedHashLine folds one line into an FNV-1a block hash, including the
// separator between lines.
func movedHashLine(hash uint64, line string) uint64 {
	for i := 0; i < len(line); i++ {
		hash ^= uint64(line[i])
		hash *= movedHashPrime
	}
	return hash * movedHashPrime
}

func findMovedRange(ranges []movedRange, line int) (movedRange, bool) {
	index := sort.Search(len(ranges), func(i int) bool { return ranges[i].start > line })
	if index == 0 {
		return movedRange{}, false
	}
	block := ranges[index-1]
	if line < block.start || line > block.end {
		return movedRange{}, false
	}
	return block, true
}

// lookup returns whether the row belongs to a block that moved, and the hint
// for an added block's last line.
func (idx *movedIndex) lookup(r row) (moved bool, hint string, showHint bool) {
	if idx == nil || !movedDetectionEnabled || r.path == "" {
		return false, "", false
	}
	kind := r.kind
	line := r.sourceLine
	var ranges []movedRange
	if kind == '+' {
		line = r.target
		ranges = idx.added[r.path]
	} else if kind == '-' || kind == 'd' {
		ranges = idx.removed[r.path]
	} else {
		return false, "", false
	}
	if line <= 0 || len(ranges) == 0 {
		return false, "", false
	}
	block, ok := findMovedRange(ranges, line)
	if !ok {
		return false, "", false
	}
	if kind == '+' {
		from, matched := idx.removedOne[block.hash]
		if !matched {
			return false, "", false
		}
		if line == block.end {
			return true, "  moved from " + locationText(from.path, from.start), true
		}
		return true, "", false
	}
	if !idx.addedHashes[block.hash] {
		return false, "", false
	}
	return true, "", false
}

// movedFor reports whether the row is part of a moved block in the commit on
// screen. It only runs for rows the viewport is about to draw.
func (m model) movedFor(r row) (moved bool, hint string, showHint bool) {
	if m.contextCommit == "" || m.contextBase {
		return false, "", false
	}
	return m.history.moved.lookup(r)
}

// nextRowContinues reports whether the next row wraps the same source line, so
// the moved-from hint waits until after the last physical row.
func (m model) nextRowContinues(index int) bool {
	if index < 0 || index+1 >= len(m.rows) {
		return false
	}
	next, current := m.rows[index+1], m.rows[index]
	return next.path == current.path && next.kind == current.kind && next.offset > 0
}

func (m model) movedHintRow(hint string) string {
	text := strings.Repeat(" ", diffMargin(m.width)+max(0, m.gutterWidth)+2) + strings.TrimSpace(hint)
	return styleDim + fit(text, m.width) + styleReset
}

// movedHintCount counts moved-from hint lines the viewport would render.
func (m model) movedHintCount(start, end int) int {
	if m.contextCommit == "" || m.contextBase || m.history.moved == nil {
		return 0
	}
	count := 0
	for i := max(0, start); i < end && i < len(m.rows); i++ {
		if _, _, show := m.movedFor(m.rows[i]); show && !m.nextRowContinues(i) {
			count++
		}
	}
	return count
}

package main

import (
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

func (idx *movedIndex) addChanges(changes []change) {
	if idx == nil || !movedDetectionEnabled {
		return
	}
	for _, c := range changes {
		idx.addChange(c)
	}
}

func (idx *movedIndex) addChange(c change) {
	if idx == nil || !movedDetectionEnabled {
		return
	}
	removedPath := c.path
	if c.oldPath != "" {
		removedPath = c.oldPath
	}
	oldLine, newLine := 0, 0
	inHunk := false
	var kind byte
	var texts []string
	start, path := 0, ""
	flush := func() {
		if kind != 0 && len(texts) >= 3 {
			hash := movedHash(texts)
			block := movedRange{start: start, end: start + len(texts) - 1, hash: hash, path: path}
			if kind == '+' {
				idx.added[c.path] = append(idx.added[c.path], block)
				idx.addedHashes[hash] = true
			} else {
				idx.removed[c.path] = append(idx.removed[c.path], block)
				if _, ok := idx.removedOne[hash]; !ok {
					idx.removedOne[hash] = block
				}
			}
		}
		texts = nil
		kind = 0
	}
	for _, line := range strings.Split(c.diff, "\n") {
		if match := matchHunkHeader(line); match != nil {
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
			path = c.path
			if kind == '-' {
				start = oldLine
				path = removedPath
			} else {
				start = newLine
			}
		}
		texts = append(texts, line[1:])
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

// movedHash is an allocation-free FNV-1a over the block's line contents.
func movedHash(lines []string) uint64 {
	const offset = 14695981039346656037
	const prime = 1099511628211
	hash := uint64(offset)
	for _, line := range lines {
		for i := 0; i < len(line); i++ {
			hash ^= uint64(line[i])
			hash *= prime
		}
		hash *= prime
	}
	return hash
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

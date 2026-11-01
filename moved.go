package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// movedHighlightLimit bounds the post-pass; streamed commits above it skip the
// cross-file comparison so first paint and memory stay predictable.
var movedHighlightLimit = 300_000

type movedBlock struct {
	key        string
	hint       string
	start, end int
	path       string
	line       int
}

func blockKey(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:12])
}

// markMovedBlocks dims added and removed runs of three or more lines that
// reappear elsewhere in the same commit, and adds a `moved from` hint after
// each moved addition.
func markMovedBlocks(rows []row) []row {
	if len(rows) < 6 || len(rows) > movedHighlightLimit {
		return rows
	}
	removed := make(map[string][]movedBlock)
	var added []movedBlock
	for i := 0; i < len(rows); {
		r := rows[i]
		if (r.kind != '+' && r.kind != '-') || r.offset > 0 {
			i++
			continue
		}
		path, kind := r.path, r.kind
		j := i
		var texts []string
		line := 0
		for j < len(rows) && rows[j].path == path && rows[j].kind == kind {
			if rows[j].offset == 0 {
				texts = append(texts, rows[j].text)
				if line == 0 {
					if kind == '-' {
						line = rows[j].sourceLine
					} else {
						line = rows[j].target
					}
				}
			}
			j++
		}
		if len(texts) >= 3 {
			block := movedBlock{key: blockKey(texts), start: i, end: j, path: path, line: line}
			if kind == '-' {
				removed[block.key] = append(removed[block.key], block)
			} else {
				added = append(added, block)
			}
		}
		i = j
	}
	if len(removed) == 0 || len(added) == 0 {
		return rows
	}
	var addedHints []movedBlock
	for _, block := range added {
		from, ok := removed[block.key]
		if !ok {
			continue
		}
		for i := block.start; i < block.end; i++ {
			rows[i].setMoved()
		}
		for i := from[0].start; i < from[0].end; i++ {
			rows[i].setMoved()
		}
		block.hint = fmt.Sprintf("  moved from %s", locationText(from[0].path, from[0].line))
		addedHints = append(addedHints, block)
	}
	if len(addedHints) == 0 {
		return rows
	}
	result := make([]row, 0, len(rows)+len(addedHints))
	hints := make(map[int]string, len(addedHints))
	for _, block := range addedHints {
		hints[block.end] = block.hint
	}
	for i, r := range rows {
		result = append(result, r)
		if hint, ok := hints[i+1]; ok {
			result = append(result, row{kind: 'm', text: hint, path: rows[i].path})
		}
	}
	return result
}

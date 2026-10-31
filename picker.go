package main

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
)

type pickerState struct {
	query     string
	selected  int
	searching bool
}

type fileEntry struct {
	path           string
	row            int
	added, deleted int
}

func (m model) fileEntries() []fileEntry {
	var entries []fileEntry
	for i, r := range m.rows {
		switch {
		case r.kind == 'f':
			entries = append(entries, fileEntry{path: r.path, row: i, deleted: r.deletedLines})
		case len(entries) == 0 || r.offset > 0:
		case r.kind == '+':
			entries[len(entries)-1].added++
		case r.kind == '-' || r.kind == 'd':
			entries[len(entries)-1].deleted++
		}
	}
	return entries
}

// filterFiles ranks basename substring matches first, then path substring
// matches, then paths containing the query as a subsequence.
func filterFiles(entries []fileEntry, query string) []fileEntry {
	if query == "" {
		return entries
	}
	query = strings.ToLower(query)
	type scored struct {
		entry fileEntry
		score int
	}
	var matches []scored
	for _, e := range entries {
		full := strings.ToLower(e.path)
		if i := strings.Index(strings.ToLower(path.Base(e.path)), query); i >= 0 {
			matches = append(matches, scored{e, i})
		} else if i := strings.Index(full, query); i >= 0 {
			matches = append(matches, scored{e, 1000 + i})
		} else if isSubsequence(full, query) {
			matches = append(matches, scored{e, 2000 + len(full)})
		}
	}
	slices.SortStableFunc(matches, func(a, b scored) int { return a.score - b.score })
	result := make([]fileEntry, len(matches))
	for i, s := range matches {
		result[i] = s.entry
	}
	return result
}

func isSubsequence(s, sub string) bool {
	for _, r := range s {
		if sub == "" {
			break
		}
		if first := []rune(sub)[0]; r == first {
			sub = sub[len(string(first)):]
		}
	}
	return sub == ""
}

func (m *model) openPicker() {
	if len(m.rows) == 0 {
		return
	}
	m.modal = "files"
	m.picker = pickerState{}
	current := m.rows[m.pos].path
	for i, e := range m.fileEntries() {
		if e.path == current {
			m.picker.selected = i
		}
	}
}

func (m *model) handlePickerKey(key string, runes []rune) {
	matches := filterFiles(m.fileEntries(), m.picker.query)
	switch key {
	case "ctrl+c":
		m.modal = ""
	case "esc":
		if m.picker.searching {
			m.picker.searching = false
		} else {
			m.modal = ""
		}
	case "enter":
		m.modal = ""
		if len(matches) > 0 {
			m.pos = matches[min(m.picker.selected, len(matches)-1)].row
			m.positionViewport("t")
		}
	case "up", "ctrl+p", "ctrl+k", "shift+tab":
		m.picker.selected = max(0, m.picker.selected-1)
	case "down", "ctrl+n", "ctrl+j", "tab":
		m.picker.selected = min(m.picker.selected+1, max(0, len(matches)-1))
	case "backspace", "ctrl+h":
		if m.picker.searching {
			query := []rune(m.picker.query)
			if len(query) > 0 {
				m.picker.query = string(query[:len(query)-1])
				m.picker.selected = 0
			}
		}
	default:
		if m.picker.searching && len(runes) > 0 {
			m.picker.query += string(runes)
			m.picker.selected = 0
		} else if !m.picker.searching {
			switch key {
			case "/":
				m.picker.searching = true
			case "j":
				m.picker.selected = min(m.picker.selected+1, max(0, len(matches)-1))
			case "k":
				m.picker.selected = max(0, m.picker.selected-1)
			case "q":
				m.modal = ""
			}
		}
	}
}

func (m model) showFilesDialog(lines []string) {
	entries := m.fileEntries()
	matches := filterFiles(entries, m.picker.query)
	selected := min(m.picker.selected, max(0, len(matches)-1))
	if m.width < 20 || len(lines) < 7 {
		status := "file: /" + clean(m.picker.query)
		if m.picker.searching {
			status += "_"
		}
		if len(matches) > 0 {
			status += "  " + clean(matches[selected].path)
		}
		lines[len(lines)-1] = fit(status, m.width)
		return
	}
	boxWidth := min(72, m.width-2)
	innerWidth := boxWidth - 4
	visible := max(1, min(len(matches), len(lines)-7))
	start := max(0, min(selected-visible+1, len(matches)-visible))
	countWidth := 0
	for _, e := range matches {
		countWidth = max(countWidth, len(fileCounts(e)))
	}
	query := sliceWidth(clean(m.picker.query), max(0, runewidth.StringWidth(m.picker.query)-innerWidth+2), max(0, innerWidth-2))
	queryLine := dialogLine("/"+query, innerWidth)
	hint := "j/k select  / search  ↵ jump  esc close"
	if m.picker.searching {
		queryLine = dialogLineWithCursor("/"+query, innerWidth)
		hint = "↑↓ select  ↵ jump  esc navigate"
	}
	content := []string{
		"┌" + strings.Repeat("─", boxWidth-2) + "┐",
		dialogLine(fmt.Sprintf("Files  %d/%d", len(matches), len(entries)), innerWidth),
		queryLine,
	}
	if len(matches) == 0 {
		content = append(content, dialogLine("  No matching files", innerWidth))
	}
	for i := start; i < min(len(matches), start+visible); i++ {
		e := matches[i]
		marker := "  "
		if i == selected {
			marker = "▸ "
		}
		counts := fmt.Sprintf("%-*s  ", countWidth, fileCounts(e))
		line := dialogLine(marker+counts+leftFit(clean(e.path), max(1, innerWidth-2-len(counts))), innerWidth)
		if i == selected {
			line = "\x1b[1m" + line + "\x1b[0m"
		}
		content = append(content, line)
	}
	content = append(content, dialogLine(hint, innerWidth), "└"+strings.Repeat("─", boxWidth-2)+"┘")
	left := strings.Repeat(" ", max(0, (m.width-boxWidth)/2))
	top := max(0, (len(lines)-len(content))/2)
	for i, line := range content {
		lines[top+i] = left + line
	}
}

func fileCounts(e fileEntry) string { return fmt.Sprintf("+%d -%d", e.added, e.deleted) }

// leftFit keeps the end of s, where a path's basename is, within width.
func leftFit(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && runewidth.StringWidth(string(runes))+1 > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

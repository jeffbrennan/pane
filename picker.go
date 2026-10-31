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
	collapsed map[string]bool
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
	for i, e := range m.pickerItems() {
		if !e.directory && e.path == current {
			m.picker.selected = i
		}
	}
}

func (m model) pickerItems() []pickerItem {
	return pickerTree(filterFiles(m.fileEntries(), m.picker.query), m.picker.collapsed)
}

func (m *model) resetPickerSearch() {
	m.picker.collapsed = nil
	m.picker.selected = 0
	for i, item := range m.pickerItems() {
		if !item.directory {
			m.picker.selected = i
			break
		}
	}
}

func (m *model) setPickerDirectory(item pickerItem, collapsed bool) {
	if !item.directory {
		return
	}
	if m.picker.collapsed == nil {
		m.picker.collapsed = make(map[string]bool)
	}
	m.picker.collapsed[item.path] = collapsed
}

func (m *model) handlePickerKey(key string, runes []rune) {
	items := m.pickerItems()
	selected := min(m.picker.selected, max(0, len(items)-1))
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
		if len(items) == 0 {
			m.modal = ""
		} else if items[selected].directory {
			m.setPickerDirectory(items[selected], !items[selected].collapsed)
		} else {
			m.modal = ""
			m.pos = items[selected].file.row
			m.positionViewport("t")
		}
	case "up", "ctrl+p", "ctrl+k", "shift+tab":
		m.picker.selected = max(0, m.picker.selected-1)
	case "down", "ctrl+n", "ctrl+j", "tab":
		m.picker.selected = min(m.picker.selected+1, max(0, len(items)-1))
	case "backspace", "ctrl+h":
		if m.picker.searching {
			query := []rune(m.picker.query)
			if len(query) > 0 {
				m.picker.query = string(query[:len(query)-1])
				m.resetPickerSearch()
			}
		}
	default:
		if m.picker.searching && len(runes) > 0 {
			m.picker.query += string(runes)
			m.resetPickerSearch()
		} else if !m.picker.searching {
			switch key {
			case "/":
				m.picker.searching = true
			case "j":
				m.picker.selected = min(m.picker.selected+1, max(0, len(items)-1))
			case "k":
				m.picker.selected = max(0, m.picker.selected-1)
			case "h", "left":
				if len(items) > 0 {
					item := items[selected]
					if item.directory && !item.collapsed {
						m.setPickerDirectory(item, true)
					} else {
						parent := path.Dir(item.path)
						for i, candidate := range items {
							if candidate.directory && candidate.path == parent {
								m.picker.selected = i
								m.setPickerDirectory(candidate, true)
								break
							}
						}
					}
				}
			case "l", "right":
				if len(items) > 0 {
					m.setPickerDirectory(items[selected], false)
				}
			case "q":
				m.modal = ""
			}
		}
	}
}

func (m model) showFilesDialog(lines []string) {
	entries := m.fileEntries()
	matches := filterFiles(entries, m.picker.query)
	items := pickerTree(matches, m.picker.collapsed)
	selected := min(m.picker.selected, max(0, len(items)-1))
	if m.width < 20 || len(lines) < 8 {
		status := "file: /" + clean(m.picker.query)
		if m.picker.searching {
			status += "_"
		}
		if len(items) > 0 {
			status += "  " + clean(items[selected].path)
		}
		lines[len(lines)-1] = fit(status, m.width)
		return
	}
	boxWidth := min(120, m.width-2)
	innerWidth := boxWidth - 4
	visible := max(1, len(lines)-8)
	start := max(0, min(selected-visible+1, len(items)-visible))
	countWidth := len("Changes")
	for _, item := range pickerTree(matches, nil) {
		countWidth = max(countWidth, len(fmt.Sprintf("+%d -%d", item.added, item.deleted)))
	}
	query := sliceWidth(clean(m.picker.query), max(0, runewidth.StringWidth(m.picker.query)-innerWidth+2), max(0, innerWidth-2))
	queryLine := dialogLine("/"+query, innerWidth)
	hint := "j/k select  h/l fold  / search  ↵ open  esc close"
	if m.picker.searching {
		queryLine = dialogLineWithCursor("/"+query, innerWidth)
		hint = "↑↓ select  ↵ jump  esc navigate"
	}
	content := []string{
		"┌" + strings.Repeat("─", boxWidth-2) + "┐",
		dialogLine(fmt.Sprintf("Files  %d/%d", len(matches), len(entries)), innerWidth),
		queryLine,
		dialogLine("  "+fmt.Sprintf("%-*s │ ", countWidth, "Changes")+"Files", innerWidth),
	}
	shown := 0
	if len(matches) == 0 {
		content = append(content, dialogLine("  No matching files", innerWidth))
		shown++
	}
	for i := start; i < min(len(items), start+visible); i++ {
		e := items[i]
		marker := "  "
		if i == selected {
			marker = "▸ "
		}
		counts := ""
		name := e.branch
		if e.directory {
			arrow := "▾ "
			if e.collapsed {
				arrow = "▸ "
				counts = fmt.Sprintf("+%d -%d", e.added, e.deleted)
			}
			name += arrow + clean(e.name) + "/"
		} else {
			counts = fileCounts(e.file)
			name += "  " + clean(e.name)
		}
		prefix := marker + fmt.Sprintf("%-*s │ ", countWidth, counts)
		line := dialogLine(prefix+fit(name, max(0, innerWidth-runewidth.StringWidth(prefix))), innerWidth)
		if i == selected {
			line = "\x1b[1m" + line + "\x1b[0m"
		}
		content = append(content, line)
		shown++
	}
	for shown < visible {
		content = append(content, dialogLine("", innerWidth))
		shown++
	}
	content = append(content, dialogLine(hint, innerWidth), "└"+strings.Repeat("─", boxWidth-2)+"┘")
	left := strings.Repeat(" ", max(0, (m.width-boxWidth)/2))
	top := max(0, (len(lines)-len(content))/2)
	for i, line := range content {
		lines[top+i] = left + line
	}
}

func fileCounts(e fileEntry) string { return fmt.Sprintf("+%d -%d", e.added, e.deleted) }

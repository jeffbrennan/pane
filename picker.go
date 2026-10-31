package main

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type pickerState struct {
	query        string
	cursor       int
	selected     int
	offset       int
	count        int
	pending      string
	pendingPath  string
	searching    bool
	collapsed    map[string]bool
	commitScoped bool
	commitIndex  int
	cache        *pickerCache
}

// pickerCache keeps the file list between keystrokes and renders, since
// collecting it walks every diff row. Model copies share it, and it is
// collected again when the rows are rebuilt.
type pickerCache struct {
	rows    []row
	entries []fileEntry
	// total counts the items of the unfiltered, unfolded tree.
	total int
}

// cached returns the cache if it was filled from rows.
func (c *pickerCache) cached(rows []row) bool {
	return c != nil && c.entries != nil && len(c.rows) == len(rows) && len(rows) > 0 && &c.rows[0] == &rows[0]
}

func (m *model) restorePicker() {
	m.pickerReturn = false
	m.pickerBackToHistory = true
	m.picker.searching = false
	m.picker.pending = ""
	m.picker.count = 0
	m.modal = "files"
	m.picker.cache = &pickerCache{}
}

type fileEntry struct {
	path           string
	oldPath        string
	submodule      bool
	row            int
	added, deleted int
}

func (m model) fileEntries() []fileEntry {
	if m.picker.cache.cached(m.rows) {
		return m.picker.cache.entries
	}
	entries := []fileEntry{}
	for i, r := range m.rows {
		if m.modal == "files" && m.picker.commitScoped && r.commitIndex != m.picker.commitIndex {
			continue
		}
		switch {
		case r.kind == 'f':
			entries = append(entries, fileEntry{path: r.path, oldPath: r.oldPath, row: i, deleted: r.deletedLines, submodule: r.submodule})
		case len(entries) == 0 || r.offset > 0:
		case r.kind == '+':
			entries[len(entries)-1].added++
		case r.kind == '-' || r.kind == 'd':
			entries[len(entries)-1].deleted++
		}
	}
	if m.contextCommit != "" && len(m.history.streamPaths) > 0 && (m.history.streamActive || m.history.streamError == "") {
		loaded := make(map[string]fileEntry, len(entries))
		for _, entry := range entries {
			loaded[entry.path] = entry
		}
		ordered := make([]fileEntry, 0, len(m.history.streamPaths))
		for _, path := range m.history.streamPaths {
			entry, ok := loaded[path]
			if !ok {
				entry = fileEntry{path: path, row: -1}
			}
			ordered = append(ordered, entry)
		}
		entries = ordered
	}
	if m.picker.cache != nil {
		*m.picker.cache = pickerCache{rows: m.rows, entries: entries, total: len(pickerTree(entries, nil))}
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
		} else if old := strings.ToLower(e.oldPath); old != "" && strings.Contains(old, query) {
			matches = append(matches, scored{e, 1000 + strings.Index(old, query)})
		} else if old != "" && isSubsequence(old, query) {
			matches = append(matches, scored{e, 2000 + len(old)})
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
	m.picker = pickerState{cache: &pickerCache{}}
	if m.screen == "sequence" {
		m.picker.commitScoped = true
		m.picker.commitIndex = m.rows[m.pos].commitIndex
	}
	current := m.rows[m.pos].path
	for i, e := range m.pickerItems() {
		if !e.directory && e.path == current {
			m.picker.selected = i
		}
	}
	m.revealPickerSelection()
}

func (m model) pickerItems() []pickerItem {
	return pickerTree(filterFiles(m.fileEntries(), m.picker.query), m.picker.collapsed)
}

func (m *model) resetPickerSearch() {
	m.picker.collapsed = nil
	m.picker.selected = 0
	m.picker.offset = 0
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

func (m *model) handlePickerKey(key string, runes []rune) bool {
	if m.picker.searching {
		switch key {
		case "left", "right", "home", "end", "ctrl+a", "ctrl+e", "alt+b", "alt+f", "ctrl+w", "alt+backspace", "backspace", "ctrl+h", "delete":
			before := m.picker.query
			m.picker.query, m.picker.cursor, _ = editLine(before, m.picker.cursor, key, nil)
			if before != m.picker.query {
				m.resetPickerSearch()
			}
			return false
		}
	}
	items := m.pickerItems()
	selected := min(m.picker.selected, max(0, len(items)-1))
	if !m.picker.searching && m.picker.pending == "" && len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		m.picker.count = min(1000000, m.picker.count*10+int(key[0]-'0'))
		return false
	}
	count := max(1, m.picker.count)
	hadCount := m.picker.count > 0
	if m.picker.pending != "" {
		pending := m.picker.pending
		m.picker.pending = ""
		m.picker.count = 0
		if pending == "g" && key == "g" {
			m.picker.selected = min(count-1, max(0, len(items)-1))
			m.revealPickerSelection()
			return false
		}
		if key == "esc" {
			return false
		}
	}
	if key != "g" || m.picker.searching {
		m.picker.count = 0
	}
	_, visible := m.pickerViewport(len(items))
	opened := false
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
			opened = true
			if items[selected].file.row < 0 {
				m.picker.pendingPath = items[selected].path
				break
			}
			m.picker.pendingPath = ""
			before := m.location()
			m.pos = items[selected].file.row
			m.positionViewport("t")
			m.recordJump(before)
		}
	case "up", "ctrl+p", "ctrl+k", "shift+tab":
		m.picker.selected = max(0, selected-count)
	case "down", "ctrl+n", "ctrl+j", "tab":
		m.picker.selected = min(selected+count, max(0, len(items)-1))
	case "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b":
		amount := visible
		if key == "ctrl+d" || key == "ctrl+u" {
			amount = max(1, visible/2)
		}
		if key == "ctrl+u" || key == "ctrl+b" {
			amount = -amount
		}
		m.picker.selected = max(0, min(selected+amount*count, len(items)-1))
	case "home":
		m.picker.selected = 0
	case "end":
		m.picker.selected = max(0, len(items)-1)
	default:
		if m.picker.searching && len(runes) > 0 {
			m.picker.query, m.picker.cursor, _ = editLine(m.picker.query, m.picker.cursor, key, runes)
			m.resetPickerSearch()
		} else if !m.picker.searching {
			switch key {
			case "/":
				m.picker.searching = true
				m.picker.cursor = len([]rune(m.picker.query))
			case "j":
				m.picker.selected = min(selected+count, max(0, len(items)-1))
			case "k":
				m.picker.selected = max(0, selected-count)
			case "g":
				m.picker.pending = "g"
			case "G":
				m.picker.selected = max(0, len(items)-1)
				if hadCount {
					m.picker.selected = min(count-1, m.picker.selected)
				}
			case "{", "}":
				direction := 1
				if key == "{" {
					direction = -1
				}
				for i := selected + direction; i >= 0 && i < len(items); i += direction {
					if items[i].directory {
						m.picker.selected = i
						count--
						if count == 0 {
							break
						}
					}
				}
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
	if m.modal != "files" {
		// Let go of the rows; restorePicker starts a new cache.
		m.picker.cache = nil
	} else {
		m.revealPickerSelection()
	}
	return opened
}

func (m model) pickerViewport(count int) (int, int) {
	// Size the list by every file rather than the filtered or folded items so
	// the dialog keeps its shape while searching and folding.
	total := 0
	if entries := m.fileEntries(); m.picker.cache.cached(m.rows) {
		total = m.picker.cache.total
	} else {
		total = len(pickerTree(entries, nil))
	}
	visible := max(1, min(m.height-8, total))
	selected := min(m.picker.selected, max(0, count-1))
	offset := min(m.picker.offset, max(0, count-1))
	if selected < offset {
		offset = selected
	} else if selected >= offset+visible {
		offset = selected - visible + 1
	}
	return offset, visible
}

func (m *model) revealPickerSelection() {
	m.picker.offset, _ = m.pickerViewport(len(m.pickerItems()))
}

func (m model) showFilesDialog(lines []string) {
	entries := m.fileEntries()
	matches := filterFiles(entries, m.picker.query)
	items := pickerTree(matches, m.picker.collapsed)
	selected := min(m.picker.selected, max(0, len(items)-1))
	if m.width < 20 || len(lines) < 8 {
		status := "file: /" + clean(m.picker.query)
		if m.picker.searching {
			status = "file: /" + renderInput(m.picker.query, m.picker.cursor, max(1, m.width-7))
		}
		if len(items) > 0 {
			status += "  " + clean(items[selected].path)
		}
		lines[len(lines)-1] = bar(m.width, status, "")
		return
	}
	boxWidth := min(120, m.width-2)
	innerWidth := boxWidth - 4
	start, visible := m.pickerViewport(len(items))
	last := min(len(items), start+visible) - 1
	numberWidth := len(fmt.Sprint(max(selected-start, last-selected)))
	addedWidth, deletedWidth := 1, 1
	for _, item := range pickerTree(matches, nil) {
		addedWidth = max(addedWidth, len(fmt.Sprint(item.added)))
		deletedWidth = max(deletedWidth, len(fmt.Sprint(item.deleted)))
	}
	countWidth := max(len("Changes"), addedWidth+deletedWidth+3)
	nameWidth := max(0, innerWidth-numberWidth-1-3-countWidth)
	columns := func(number, name, counts string) string {
		name = fit(name, nameWidth)
		return number + " " + name + strings.Repeat(" ", max(0, nameWidth-stringWidth(name))) + " │ " + counts
	}
	query := sliceWidth(clean(m.picker.query), max(0, stringWidth(m.picker.query)-innerWidth+2), max(0, innerWidth-2))
	queryLine := styleDim + "/" + query + styleReset
	if query == "" {
		queryLine = styleDim + "/ to filter" + styleReset
	}
	hint := keyHints("j/k", "move", "h/l", "fold", "/", "filter", "↵", "open", "esc", "close")
	if m.picker.searching {
		queryLine = "/" + renderInput(m.picker.query, m.picker.cursor, max(1, innerWidth-1))
		hint = keyHints("↑/↓", "move", "↵", "open", "esc", "done")
	} else if ansi.StringWidth(hint) > innerWidth {
		hint = keyHints("/", "filter", "↵", "open", "esc", "close")
	}
	info := fmt.Sprintf("%d/%d", len(matches), len(entries))
	if m.contextCommit != "" && m.history.streamActive && len(m.history.streamPaths) > 0 {
		info = fmt.Sprintf("%d/%d loaded", m.history.streamLoaded, len(m.history.streamPaths))
	}
	if m.picker.commitScoped && m.picker.commitIndex >= 0 && m.picker.commitIndex < len(m.sequence.commits) {
		info = shortHash(m.sequence.commits[m.picker.commitIndex].hash) + " · " + info
	}
	var body []string
	body = append(body, queryLine, styleDim+columns(strings.Repeat(" ", numberWidth), "Files", "Changes")+styleReset)
	shown := 0
	if len(matches) == 0 {
		body = append(body, styleDim+"  No matching files"+styleReset)
		shown++
	}
	for i := start; i < min(len(items), start+visible); i++ {
		e := items[i]
		counts := ""
		name := e.branch
		if e.directory {
			arrow := "▾ "
			if e.collapsed {
				arrow = "▸ "
				counts = changeCounts(addedWidth, e.added, deletedWidth, e.deleted, i == selected)
			}
			name += arrow + clean(e.name) + "/"
		} else {
			if e.file.row < 0 {
				counts = "…"
			} else {
				counts = changeCounts(addedWidth, e.file.added, deletedWidth, e.file.deleted, i == selected)
			}
			name += "  " + clean(e.name)
			if e.file.submodule {
				name += " (submodule)"
			}
		}
		distance := max(i-selected, selected-i)
		number := styleDim + fmt.Sprintf("%*d", numberWidth, distance) + styleReset
		if i == selected {
			number = fmt.Sprintf("%*d", numberWidth, distance)
		}
		text := columns(number, name, counts)
		if i == selected {
			text = "\x1b[7m" + text + strings.Repeat(" ", max(0, innerWidth-ansi.StringWidth(text))) + styleReset
		}
		body = append(body, text)
		shown++
	}
	for shown < visible {
		body = append(body, "")
		shown++
	}
	placeDialog(lines, m.width, dialog{width: boxWidth, title: "Files", info: info, body: body, hint: hint}.render())
}

// changeCounts formats aligned addition and deletion counts, colored unless
// the row is selected and already highlighted.
func changeCounts(addedWidth, added, deletedWidth, deleted int, selected bool) string {
	if selected {
		return fmt.Sprintf("+%*d -%*d", addedWidth, added, deletedWidth, deleted)
	}
	return fmt.Sprintf("\x1b[32m+%*d\x1b[39m \x1b[31m-%*d\x1b[39m", addedWidth, added, deletedWidth, deleted)
}

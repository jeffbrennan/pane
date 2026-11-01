package main

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// noteRef anchors a note to a file and new-side line in one review.
type noteRef struct {
	view   string
	commit string
	path   string
	line   int
}

// noteView identifies the review notes belong to.
func (m model) noteView() string {
	switch m.screen {
	case "sequence":
		return m.sequenceIdentity()
	case "commit":
		return "commit:" + m.history.current.Hash
	case "base":
		return "base:" + m.base.hash + ":" + m.base.revision
	}
	if m.viewIdentity != "" {
		return m.viewIdentity
	}
	return "worktree"
}

func (m model) noteCommit(hash string, row row) string {
	if hash != "" {
		return hash
	}
	if m.screen == "sequence" && int(row.commitIndex) < len(m.sequence.commits) {
		return m.sequence.commits[row.commitIndex].Hash
	}
	if m.screen == "commit" {
		return m.history.current.Hash
	}
	return ""
}

// startNote opens the one-line note editor for the active diff line.
func (m *model) startNote() bool {
	if m.pos < 0 || m.pos >= len(m.rows) {
		return false
	}
	r := m.rows[m.pos]
	if r.path == "" || (r.kind != ' ' && r.kind != '+' && r.kind != '-' && r.kind != 'd') {
		m.flash = "Notes attach to diff lines"
		return false
	}
	line := r.sourceLine
	if r.kind == '-' || r.kind == 'd' {
		line = r.target
	}
	if line <= 0 {
		return false
	}
	ref := noteRef{view: m.noteView(), commit: m.noteCommit("", r), path: r.path, line: line}
	m.noteTarget = ref
	m.input = m.notes[ref]
	m.inputCursor = len([]rune(m.input))
	m.modal = "note"
	return true
}

// saveNote stores the editor text, or deletes the note when it is empty.
func (m *model) saveNote() {
	text := strings.TrimSpace(m.input)
	if text == "" {
		delete(m.notes, m.noteTarget)
	} else {
		if m.notes == nil {
			m.notes = make(map[noteRef]string)
		}
		m.notes[m.noteTarget] = text
	}
	m.modal = ""
	switch m.screen {
	case "commit", "base":
		m.rebuildCommit()
	case "sequence":
		m.sequence.rowCache = nil
		m.rebuildSequence()
	default:
		m.rebuild()
	}
}

// insertNoteRows adds a dim note row after each annotated line. It is safe to
// run on rows that already contain note rows.
func (m model) insertNoteRows(rows []row) []row {
	if len(m.notes) == 0 {
		return rows
	}
	view := m.noteView()
	if view == "" {
		return rows
	}
	byLine := make(map[string][]string)
	for ref, text := range m.notes {
		if ref.view != view {
			continue
		}
		key := fmt.Sprintf("%s\x00%d\x00%s", ref.path, ref.line, ref.commit)
		byLine[key] = append(byLine[key], text)
	}
	if len(byLine) == 0 {
		return rows
	}
	result := make([]row, 0, len(rows)+len(byLine))
	inserted := make(map[string]bool, len(byLine))
	for i, r := range rows {
		result = append(result, r)
		if r.path == "" {
			continue
		}
		line := r.sourceLine
		if r.kind == '-' || r.kind == 'd' {
			line = r.target
		}
		if line <= 0 || (r.kind != ' ' && r.kind != '+' && r.kind != '-' && r.kind != 'd') {
			continue
		}
		commit := ""
		if m.screen == "sequence" && int(r.commitIndex) < len(m.sequence.commits) {
			commit = m.sequence.commits[r.commitIndex].Hash
		}
		texts := byLine[fmt.Sprintf("%s\x00%d\x00%s", r.path, line, commit)]
		if len(texts) == 0 {
			continue
		}
		noteKey := fmt.Sprintf("%s\x00%d\x00%s", r.path, line, commit)
		if inserted[noteKey] {
			continue
		}
		inserted[noteKey] = true
		sort.Strings(texts)
		text := "  ✎ " + texts[0]
		if i+1 < len(rows) && rows[i+1].kind == 'n' && rows[i+1].path == r.path && rows[i+1].target == line && strings.TrimSpace(rows[i+1].text) == strings.TrimSpace(text) {
			continue
		}
		result = append(result, row{path: r.path, target: line, text: text, kind: 'n'})
	}
	return result
}

func noteLocation(m model, ref noteRef, remote string) string {
	if ref.commit != "" && remote != "" {
		if link := remotePermalink(remote, ref.commit, ref.path, ref.line); link != "" {
			return link
		}
	}
	return locationText(ref.path, ref.line)
}

// notesMarkdown exports every note in the current review as Markdown.
func (m model) notesMarkdown() (string, int) {
	if len(m.notes) == 0 {
		return "", 0
	}
	view := m
	if m.screen == "commit" || m.screen == "base" {
		view = m.commitModel()
	}
	identity := view.noteView()
	var refs []noteRef
	for ref := range m.notes {
		if ref.view == identity {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return "", 0
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].path != refs[j].path {
			return refs[i].path < refs[j].path
		}
		if refs[i].line != refs[j].line {
			return refs[i].line < refs[j].line
		}
		return refs[i].commit < refs[j].commit
	})
	remotes := make(map[string]string)
	remoteFor := func(commit string) string {
		if commit == "" {
			return ""
		}
		if remote, ok := remotes[commit]; ok {
			return remote
		}
		remote := ""
		if out, err := m.repo.Command("config", "--get", "remote.origin.url"); err == nil {
			remote = strings.TrimSpace(string(out))
		}
		remotes[commit] = remote
		return remote
	}
	var b strings.Builder
	for _, ref := range refs {
		fmt.Fprintf(&b, "### `%s`\n", noteLocation(m, ref, remoteFor(ref.commit)))
		if quote := view.quoteNoteLine(ref); quote != "" {
			fmt.Fprintf(&b, "> %s\n\n", quote)
		}
		fmt.Fprintf(&b, "%s\n\n", m.notes[ref])
	}
	return strings.TrimRight(b.String(), "\n") + "\n", len(refs)
}

// copyNotes puts the current review's notes on the clipboard.
func (m model) copyNotes() tea.Cmd {
	text, count := m.notesMarkdown()
	if count == 0 {
		return nil
	}
	return copyCmd(text, fmt.Sprintf("%d note(s)", count))
}

func (m model) quoteNoteLine(ref noteRef) string {
	best, bestKind := "", byte(0)
	for _, r := range m.rows {
		if r.path != ref.path || r.offset > 0 {
			continue
		}
		line := r.sourceLine
		if r.kind == '-' || r.kind == 'd' {
			line = r.target
		}
		if line != ref.line {
			continue
		}
		if m.screen == "sequence" && int(r.commitIndex) < len(m.sequence.commits) && m.sequence.commits[r.commitIndex].Hash != ref.commit {
			continue
		}
		text := strings.TrimSpace(r.text)
		if text == "" {
			continue
		}
		rank := 0
		switch r.kind {
		case '+':
			rank = 3
		case ' ':
			rank = 2
		case '-', 'd':
			rank = 1
		}
		if rank > int(bestKind) {
			best, bestKind = text, byte(rank)
		}
	}
	return best
}

func (m *model) handleNoteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		m.saveNote()
	case "esc", "ctrl+c":
		m.modal = ""
	default:
		var inserted []rune
		if key.Type == tea.KeyRunes {
			inserted = key.Runes
		} else if key.Type == tea.KeySpace {
			inserted = []rune{' '}
		}
		m.input, m.inputCursor, _ = editLine(m.input, m.inputCursor, key.String(), inserted)
	}
	return *m, nil
}

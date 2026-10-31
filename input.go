package main

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func renderInput(text string, cursor, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	pos := max(0, min(cursor, len(runes)))
	before := clean(string(runes[:pos]))
	after := ""
	cell := " "
	if pos < len(runes) {
		cell = clean(string(runes[pos]))
		after = clean(string(runes[pos+1:]))
	}
	cellWidth := max(1, runewidth.StringWidth(cell))
	if cellWidth > width {
		cell, cellWidth = " ", 1
	}
	start := max(0, runewidth.StringWidth(before)+cellWidth-width)
	visibleBefore := sliceWidth(before, start, width-cellWidth)
	remaining := max(0, width-runewidth.StringWidth(visibleBefore)-cellWidth)
	return visibleBefore + "\x1b[7m" + cell + styleReset + sliceWidth(after, 0, remaining)
}

func inputWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

func nextInputWord(runes []rune, pos int) int {
	if pos < len(runes) {
		class := inputWord(runes[pos])
		for pos < len(runes) && !unicode.IsSpace(runes[pos]) && inputWord(runes[pos]) == class {
			pos++
		}
	}
	for pos < len(runes) && unicode.IsSpace(runes[pos]) {
		pos++
	}
	return pos
}

func previousInputWord(runes []rune, pos int) int {
	for pos > 0 && unicode.IsSpace(runes[pos-1]) {
		pos--
	}
	if pos > 0 {
		class := inputWord(runes[pos-1])
		for pos > 0 && !unicode.IsSpace(runes[pos-1]) && inputWord(runes[pos-1]) == class {
			pos--
		}
	}
	return pos
}

func (m *model) handleCommitNormal(key tea.KeyMsg) {
	if key.Paste {
		return
	}
	runes := []rune(m.input)
	pos := max(0, min(m.inputCursor, max(0, len(runes)-1)))
	command := key.String()
	if m.inputPending != "" {
		command = m.inputPending + command
		m.inputPending = ""
	}
	switch command {
	case "i":
		m.inputNormal = false
	case "a":
		m.inputNormal = false
		pos = min(len(runes), pos+1)
	case "I":
		m.inputNormal = false
		pos = 0
	case "A":
		m.inputNormal = false
		pos = len(runes)
	case "h", "left":
		pos = max(0, pos-1)
	case "l", "right":
		pos = min(max(0, len(runes)-1), pos+1)
	case "0", "home":
		pos = 0
	case "$", "end":
		pos = max(0, len(runes)-1)
	case "w":
		pos = min(max(0, len(runes)-1), nextInputWord(runes, pos))
	case "b":
		pos = previousInputWord(runes, pos)
	case "d":
		m.inputPending = "d"
	case "x", "dw", "dd", "D":
		end := min(len(runes), pos+1)
		switch command {
		case "dw":
			end = nextInputWord(runes, pos)
		case "dd":
			pos, end = 0, len(runes)
		case "D":
			end = len(runes)
		}
		m.input = string(runes[:pos]) + string(runes[end:])
		pos = min(pos, max(0, len([]rune(m.input))-1))
		m.message = ""
	}
	m.inputCursor = pos
}

// editLine applies a single-line editing key using a rune-indexed cursor.
func editLine(text string, cursor int, key string, inserted []rune) (string, int, bool) {
	runes := []rune(text)
	cursor = max(0, min(cursor, len(runes)))
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }
	previousWord := func(pos int) int {
		for pos > 0 && !word(runes[pos-1]) {
			pos--
		}
		for pos > 0 && word(runes[pos-1]) {
			pos--
		}
		return pos
	}
	remove := func(start, end int) { runes = append(runes[:start], runes[end:]...); cursor = start }
	switch key {
	case "left", "ctrl+b":
		cursor = max(0, cursor-1)
	case "right", "ctrl+f":
		cursor = min(len(runes), cursor+1)
	case "alt+b":
		cursor = previousWord(cursor)
	case "alt+f":
		for cursor < len(runes) && !word(runes[cursor]) {
			cursor++
		}
		for cursor < len(runes) && word(runes[cursor]) {
			cursor++
		}
	case "home", "ctrl+a":
		cursor = 0
	case "end", "ctrl+e":
		cursor = len(runes)
	case "ctrl+w", "alt+backspace":
		remove(previousWord(cursor), cursor)
	case "ctrl+u":
		remove(0, cursor)
	case "ctrl+k":
		remove(cursor, len(runes))
	case "backspace", "ctrl+h":
		if cursor > 0 {
			remove(cursor-1, cursor)
		}
	case "delete", "ctrl+d":
		if cursor < len(runes) {
			remove(cursor, cursor+1)
		}
	default:
		if len(inserted) == 0 {
			return text, cursor, false
		}
		result := make([]rune, 0, len(runes)+len(inserted))
		result = append(result, runes[:cursor]...)
		result = append(result, inserted...)
		result = append(result, runes[cursor:]...)
		return string(result), cursor + len(inserted), true
	}
	return string(runes), cursor, true
}

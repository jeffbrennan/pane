package main

import "strings"

func actionVisibleRows(height int) int { return max(1, min(8, height-6)) }

func actionOutputLines(output string, width int) []string {
	if strings.TrimSpace(output) == "" {
		output = "Completed successfully."
	}
	innerWidth := max(1, min(64, width-2)-4)
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		lines = append(lines, wrapLine(clean(line), innerWidth)...)
	}
	return lines
}

func (m model) actionDialogView() string {
	base := m
	base.modal = ""
	base.message = ""
	lines := strings.Split(base.View(), "\n")
	if len(lines) == 0 {
		return ""
	}
	for i := 1; i < len(lines)-1; i++ {
		lines[i] = ""
	}
	title, output, hint := "", "", "y/↵ yes  n/esc no"
	if m.modal == "result" {
		title = "✓ Push complete"
		if m.resultAction == "pull" {
			title = "✓ Pull complete"
		}
		output = m.resultText
		hint = "j/k  ↵/esc close"
	} else if m.modal == "push" {
		title, output = "Push changes?", "Push to the configured upstream."
	} else {
		title, output = "Pull changes?", "Pull from the configured upstream."
	}
	if m.width < 24 || len(lines) < 8 {
		lines[len(lines)-1] = fit(title+"  "+hint, m.width)
		return strings.Join(lines, "\n")
	}
	boxWidth := min(64, m.width-2)
	innerWidth := boxWidth - 4
	body := actionOutputLines(output, m.width)
	visible := min(len(body), actionVisibleRows(len(lines)))
	start := min(m.resultPos, max(0, len(body)-visible))
	content := []string{"┌" + strings.Repeat("─", boxWidth-2) + "┐"}
	titleLine := dialogLine(title, innerWidth)
	if m.modal == "result" {
		titleLine = "\x1b[1;32m" + titleLine + "\x1b[0m"
	}
	content = append(content, titleLine, dialogLine("", innerWidth))
	for _, line := range body[start : start+visible] {
		content = append(content, dialogLine(line, innerWidth))
	}
	content = append(content, dialogLine("", innerWidth), dialogLine(hint, innerWidth), "└"+strings.Repeat("─", boxWidth-2)+"┘")
	left := strings.Repeat(" ", max(0, (m.width-boxWidth)/2))
	top := max(0, (len(lines)-len(content))/2)
	for i, line := range content {
		lines[top+i] = left + line
	}
	return strings.Join(lines, "\n")
}

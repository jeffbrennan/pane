package main

import "strings"

type commitReceipt struct {
	message, output string
	baseline        *snapshot
}

func (r commitReceipt) lines(width int) []string {
	lines := []string{"", fit("  Commit created", width), ""}
	appendText := func(text string) {
		for _, part := range wrapLine(clean(text), max(1, width-2)) {
			lines = append(lines, fit("  "+part, width))
		}
	}
	appendText(r.message)
	output := strings.Split(r.output, "\n")
	if len(output) > 0 && strings.HasPrefix(output[0], "[") {
		if end := strings.Index(output[0], "]"); end >= 0 {
			appendText(output[0][:end+1])
			output = output[1:]
		}
	}
	for _, line := range output {
		if strings.TrimSpace(line) != "" {
			appendText(strings.TrimSpace(line))
		}
	}
	return append(lines, "")
}

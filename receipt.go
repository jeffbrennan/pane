package main

import (
	"regexp"
	"strings"
)

var changeCount = regexp.MustCompile(`\d+ (?:insertions?\(\+\)|deletions?\(-\))`)

type commitReceipt struct {
	message, output string
	baseline        *snapshot
}

func (r commitReceipt) lines(width int) []string {
	lines := []string{"", "\x1b[1;32m" + fit("  ✓ Commit created", width) + "\x1b[0m", ""}
	appendText := func(text, style string) {
		for _, part := range wrapLine(clean(text), max(1, width-2)) {
			lines = append(lines, style+fit("  "+part, width)+"\x1b[0m")
		}
	}
	appendText(r.message, "\x1b[1m")
	output := strings.Split(r.output, "\n")
	if len(output) > 0 && strings.HasPrefix(output[0], "[") {
		if end := strings.Index(output[0], "]"); end >= 0 {
			appendText(output[0][:end+1], "\x1b[36m")
			output = output[1:]
		}
	}
	for _, line := range output {
		if strings.TrimSpace(line) != "" {
			for _, part := range wrapLine(clean(strings.TrimSpace(line)), max(1, width-2)) {
				text := fit("  "+part, width)
				text = changeCount.ReplaceAllStringFunc(text, func(count string) string {
					color := "\x1b[32m"
					if strings.Contains(count, "deletion") {
						color = "\x1b[31m"
					}
					return "\x1b[0m" + color + count + "\x1b[0;2m"
				})
				lines = append(lines, "\x1b[2m"+text+"\x1b[0m")
			}
		}
	}
	return append(lines, "")
}

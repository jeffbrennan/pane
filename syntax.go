package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/mattn/go-runewidth"
)

type syntaxSpan struct {
	start, end int
	color      string
}

type syntaxLine struct {
	text  string
	spans []syntaxSpan
}

func syntaxLexer(path string) chroma.Lexer {
	name := strings.ToLower(filepath.Base(path))
	if name == "dockerfile" || strings.HasPrefix(name, "dockerfile.") {
		return lexers.Get("docker")
	}
	if name == "makefile" || name == "gnumakefile" {
		return lexers.Get("makefile")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return lexers.Get("go")
	case ".py", ".pyi", ".pyw":
		return lexers.Get("python")
	case ".sql":
		return lexers.Get("sql")
	case ".rs":
		return lexers.Get("rust")
	case ".json", ".jsonl", ".ndjson", ".ipynb", ".avsc":
		return lexers.Get("json")
	case ".toml":
		return lexers.Get("toml")
	case ".hcl":
		return lexers.Get("hcl")
	case ".tf", ".tfvars":
		return lexers.Get("terraform")
	case ".yaml", ".yml":
		return lexers.Get("yaml")
	case ".ini", ".cfg":
		return lexers.Get("ini")
	case ".properties":
		return lexers.Get("properties")
	case ".sh", ".bash", ".zsh":
		return lexers.Get("bash")
	case ".scala", ".sc":
		return lexers.Get("scala")
	case ".java":
		return lexers.Get("java")
	case ".kt", ".kts":
		return lexers.Get("kotlin")
	case ".r":
		return lexers.Get("r")
	case ".jl":
		return lexers.Get("julia")
	case ".groovy", ".gradle":
		return lexers.Get("groovy")
	case ".js", ".mjs", ".cjs":
		return lexers.Get("javascript")
	case ".ts":
		return lexers.Get("typescript")
	case ".xml":
		return lexers.Get("xml")
	case ".proto":
		return lexers.Get("protobuf")
	case ".ps1", ".psm1", ".psd1":
		return lexers.Get("powershell")
	default:
		return nil
	}
}

func (r repository) loadSyntaxDiffs(changes []change, key contextKey) {
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i := range changes {
		if syntaxLexer(changes[i].path) == nil || changes[i].conflict {
			continue
		}
		wg.Add(1)
		limit <- struct{}{}
		go func(i int) {
			defer func() { <-limit; wg.Done() }()
			fileKey := key
			fileKey.path = changes[i].path
			diff, err := r.fullContext(fileKey, changes[i].diff)
			if err == nil && len(diff) <= 2*1024*1024 {
				changes[i].syntaxDiff = diff
			}
		}(i)
	}
	wg.Wait()
}

func syntaxLines(path, diff string) (map[int]syntaxLine, map[int]syntaxLine) {
	lexer := syntaxLexer(path)
	if lexer == nil || diff == "" {
		return nil, nil
	}
	var old, new []string
	inHunk := false
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if match := matchHunkHeader(line); match != nil {
			oldStart, _ := strconv.Atoi(match[1])
			newStart, _ := strconv.Atoi(match[3])
			if oldStart > len(old)+1 || newStart > len(new)+1 {
				return nil, nil
			}
			inHunk = true
			continue
		}
		if !inHunk || len(line) == 0 {
			continue
		}
		text := clean(strings.ReplaceAll(line[1:], "\t", "    "))
		switch line[0] {
		case ' ':
			old = append(old, text)
			new = append(new, text)
		case '-':
			old = append(old, text)
		case '+':
			new = append(new, text)
		}
	}
	return tokenizeLines(lexer, old), tokenizeLines(lexer, new)
}

func tokenizeLines(lexer chroma.Lexer, lines []string) map[int]syntaxLine {
	result := make(map[int]syntaxLine, len(lines))
	for i, text := range lines {
		result[i+1] = syntaxLine{text: text, spans: []syntaxSpan{}}
	}
	if len(lines) == 0 {
		return result
	}
	iterator, err := lexer.Tokenise(nil, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil
	}
	line, offset := 1, 0
	for token := iterator(); token != chroma.EOF; token = iterator() {
		color := tokenColor(token.Type)
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				line++
				offset = 0
			}
			if color != "" && len(part) > 0 && line <= len(lines) {
				entry := result[line]
				entry.spans = append(entry.spans, syntaxSpan{offset, offset + len(part), color})
				result[line] = entry
			}
			offset += len(part)
		}
	}
	return result
}

func tokenColor(t chroma.TokenType) string {
	switch {
	case t.InCategory(chroma.Comment):
		return "\x1b[90m"
	case t.InCategory(chroma.Keyword):
		return "\x1b[35m"
	case t.InSubCategory(chroma.LiteralString):
		return "\x1b[32m"
	case t.InSubCategory(chroma.LiteralNumber):
		return "\x1b[36m"
	case t == chroma.NameFunction:
		return "\x1b[34m"
	case t == chroma.NameTag || t == chroma.NameAttribute:
		return "\x1b[36m"
	default:
		return ""
	}
}

func clipSyntax(spans []syntaxSpan, start, end int) []syntaxSpan {
	result := []syntaxSpan{}
	for _, span := range spans {
		if span.end > start && span.start < end {
			result = append(result, syntaxSpan{max(span.start, start) - start, min(span.end, end) - start, span.color})
		}
	}
	return result
}

func sliceStart(text string, start, width int) int {
	pos := 0
	for i, r := range text {
		w := runewidth.RuneWidth(r)
		if pos >= start && pos+w <= start+width {
			return i
		}
		pos += w
		if pos >= start+width {
			break
		}
	}
	return len(text)
}

func renderSyntax(text string, spans, inline []syntaxSpan, start int, query, background string, light bool) string {
	colors := make([]string, len(text))
	for _, span := range spans {
		for i := max(0, span.start-start); i < min(len(text), span.end-start); i++ {
			colors[i] = span.color
			if light {
				colors[i] = lightSyntaxColor(span.color)
			}
		}
	}
	changed := make([]bool, len(text))
	for _, span := range inline {
		for i := max(0, span.start-start); i < min(len(text), span.end-start); i++ {
			changed[i] = true
		}
	}
	matches := make([]bool, len(text))
	folded, needle := searchFold(text, query), searchFold(query, query)
	if needle != "" && len(folded) == len(text) {
		for offset := 0; offset < len(folded); {
			i := strings.Index(folded[offset:], needle)
			if i < 0 {
				break
			}
			offset += i
			for j := offset; j < offset+len(needle); j++ {
				matches[j] = true
			}
			offset += len(needle)
		}
	}
	var b strings.Builder
	color := ""
	match := false
	change := false
	for i := 0; i < len(text); i++ {
		if changed[i] != change {
			change = changed[i]
			if change {
				if background == "\x1b[48;2;221;242;224m" {
					b.WriteString("\x1b[48;2;175;220;181m")
				} else if background == "\x1b[48;2;250;225;225m" {
					b.WriteString("\x1b[48;2;239;182;182m")
				} else if background == darkAddedBackground {
					b.WriteString("\x1b[48;2;42;83;52m")
				} else {
					b.WriteString("\x1b[48;2;100;38;43m")
				}
			} else {
				b.WriteString(background)
			}
		}
		if colors[i] != color {
			b.WriteString("\x1b[39m")
			color = colors[i]
			b.WriteString(color)
		}
		if matches[i] != match {
			match = matches[i]
			if match {
				b.WriteString("\x1b[7m")
			} else {
				b.WriteString("\x1b[27m")
			}
		}
		b.WriteByte(text[i])
	}
	if match {
		b.WriteString("\x1b[27m")
	}
	if color != "" {
		b.WriteString("\x1b[39m")
	}
	if change {
		b.WriteString(background)
	}
	return b.String()
}

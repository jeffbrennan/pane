package syntax

import (
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/mattn/go-runewidth"
)

// Added and removed line backgrounds for the dark theme.
const (
	DarkAddedBackground   = "\x1b[48;2;30;55;40m"
	DarkRemovedBackground = "\x1b[48;2;65;37;41m"
)

// Span marks a byte range of a rendered string with a color escape.
type Span struct {
	Start, End int
	Color      string
}

// Line is one tokenized source line.
type Line struct {
	Text  string
	Spans []Span
}

// Lexable reports whether path has a syntax lexer.
func Lexable(path string) bool { return Lexer(path) != nil }

// Lexer returns the lexer for path, or nil when the file type is unsupported.
func Lexer(path string) chroma.Lexer {
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

// syntaxKey identifies tokenized lines: a whole file when display is empty,
// otherwise only the lines that display shows.
type syntaxKey struct{ path, diff, display string }

type syntaxResult struct{ old, new map[int]Line }

// syntaxCache keeps tokenized diffs between rebuilds; tokenizing dominates
// the cost of switching views, toggling wrap and resizing. The results are
// shared and must not be modified.
var syntaxCache = struct {
	sync.Mutex
	entries map[syntaxKey]syntaxResult
}{entries: make(map[syntaxKey]syntaxResult)}

const syntaxCacheSize = 512

// syntaxWindowLead is how many lines around each displayed run are tokenized.
// Lines before let a run that starts inside a comment or string be lexed
// correctly; lines after let the lexer see where that comment or string
// closes, since an unterminated one is not recognized.
const syntaxWindowLead = 50

// Lines tokenizes the old and new sides of a full-context diff.
func Lines(path, diff string) (map[int]Line, map[int]Line) {
	return LinesFor(path, diff, "")
}

// LinesFor returns tokenized lines for the lines that display shows,
// using whole-file results once they exist. Tokenizing only displayed lines
// costs a fraction of a whole file, so the first paint can be highlighted.
func LinesFor(path, diff, display string) (map[int]Line, map[int]Line) {
	if diff == "" || Lexer(path) == nil {
		return nil, nil
	}
	if display == diff {
		display = ""
	}
	syntaxCache.Lock()
	cached, ok := syntaxCache.entries[syntaxKey{path, diff, ""}]
	if !ok && display != "" {
		cached, ok = syntaxCache.entries[syntaxKey{path, diff, display}]
	}
	syntaxCache.Unlock()
	if ok {
		return cached.old, cached.new
	}
	key := syntaxKey{path, diff, display}
	old, new := tokenizeDiff(path, diff, display)
	syntaxCache.Lock()
	if len(syntaxCache.entries) >= syntaxCacheSize {
		clear(syntaxCache.entries)
	}
	syntaxCache.entries[key] = syntaxResult{old, new}
	syntaxCache.Unlock()
	return old, new
}

// displayedLines returns the new-side and old-side line numbers that display
// shows: context and added lines are highlighted from the new side, removed
// lines from the old side.
func displayedLines(display string) (oldLines, newLines []int) {
	oldNumber, newNumber, inHunk := 0, 0, false
	for _, line := range strings.Split(display, "\n") {
		if match := MatchHunkHeader(line); match != nil {
			oldNumber, _ = strconv.Atoi(match[1])
			newNumber, _ = strconv.Atoi(match[3])
			inHunk = true
			continue
		}
		if !inHunk || len(line) == 0 {
			continue
		}
		switch line[0] {
		case ' ':
			newLines = append(newLines, newNumber)
			oldNumber++
			newNumber++
		case '-':
			oldLines = append(oldLines, oldNumber)
			oldNumber++
		case '+':
			newLines = append(newLines, newNumber)
			newNumber++
		}
	}
	return oldLines, newLines
}

// tokenizeWindows tokenizes the runs of lines containing wanted, each with
// syntaxWindowLead lines on either side, keyed like tokenizeLines.
func tokenizeWindows(lexer chroma.Lexer, lines []string, wanted []int) map[int]Line {
	result := make(map[int]Line, len(wanted))
	slices.Sort(wanted)
	for i := 0; i < len(wanted); {
		first, last := wanted[i], wanted[i]
		for i++; i < len(wanted) && wanted[i]-last <= syntaxWindowLead; i++ {
			last = wanted[i]
		}
		first, last = max(1, first), min(len(lines), last)
		if first > last {
			continue
		}
		start, end := max(1, first-syntaxWindowLead), min(len(lines), last+syntaxWindowLead)
		tokenized := tokenizeLines(lexer, lines[start-1:end])
		if tokenized == nil {
			return nil
		}
		for number := first; number <= last; number++ {
			result[number] = tokenized[number-start+1]
		}
	}
	return result
}

// tokenizeDiff tokenizes a full-context diff, every line when display is
// empty and otherwise only the lines that display shows.
func tokenizeDiff(path, diff, display string) (map[int]Line, map[int]Line) {
	lexer := Lexer(path)
	if lexer == nil || diff == "" {
		return nil, nil
	}
	var old, new []string
	inHunk := false
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if match := MatchHunkHeader(line); match != nil {
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
		text := Clean(strings.ReplaceAll(line[1:], "\t", "    "))
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
	if display == "" {
		return tokenizeLines(lexer, old), tokenizeLines(lexer, new)
	}
	oldLines, newLines := displayedLines(display)
	return tokenizeWindows(lexer, old, oldLines), tokenizeWindows(lexer, new, newLines)
}

func tokenizeLines(lexer chroma.Lexer, lines []string) map[int]Line {
	result := make(map[int]Line, len(lines))
	for i, text := range lines {
		result[i+1] = Line{Text: text, Spans: []Span{}}
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
		color := TokenColor(token.Type)
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				line++
				offset = 0
			}
			if color != "" && len(part) > 0 && line <= len(lines) {
				entry := result[line]
				entry.Spans = append(entry.Spans, Span{Start: offset, End: offset + len(part), Color: color})
				result[line] = entry
			}
			offset += len(part)
		}
	}
	return result
}

// TokenColor maps a token type to its terminal color.
func TokenColor(t chroma.TokenType) string {
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

func Clip(spans []Span, start, end int) []Span {
	result := []Span{}
	for _, span := range spans {
		if span.End > start && span.Start < end {
			result = append(result, Span{max(span.Start, start) - start, min(span.End, end) - start, span.Color})
		}
	}
	return result
}

func SliceStart(text string, start, width int) int {
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

func Render(text string, spans, inline []Span, start int, query, background string, light bool) string {
	return RenderWithHighlight(text, spans, inline, start, query, background, light, "")
}

func RenderWithHighlight(text string, spans, inline []Span, start int, query, background string, light bool, highlight string) string {
	// Index into a small palette instead of storing a string header per byte;
	// diffs have only a handful of distinct colors, and this drops the largest
	// per-row allocation in the render path.
	palette := []string{""}
	colorIDs := make([]uint8, len(text))
	for _, span := range spans {
		color := span.Color
		if light {
			color = lightSyntaxColor(span.Color)
		}
		id := uint8(0)
		if color != "" {
			for i, existing := range palette {
				if existing == color {
					id = uint8(i)
					break
				}
			}
			if id == 0 {
				palette = append(palette, color)
				id = uint8(len(palette) - 1)
			}
		}
		for i := max(0, span.Start-start); i < min(len(text), span.End-start); i++ {
			colorIDs[i] = id
		}
	}
	changed := make([]bool, len(text))
	for _, span := range inline {
		for i := max(0, span.Start-start); i < min(len(text), span.End-start); i++ {
			changed[i] = true
		}
	}
	matches := make([]bool, len(text))
	if query != "" {
		folded, needle := Fold(text, query), Fold(query, query)
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
	}
	var b strings.Builder
	b.Grow(len(text) + 32 + len(spans)*8 + len(inline)*16)
	color := uint8(0)
	match := false
	change := false
	for i := 0; i < len(text); i++ {
		if changed[i] != change {
			change = changed[i]
			if change {
				if highlight != "" {
					b.WriteString(highlight)
				} else if background == "\x1b[48;2;221;242;224m" {
					b.WriteString("\x1b[48;2;175;220;181m")
				} else if background == "\x1b[48;2;250;225;225m" {
					b.WriteString("\x1b[48;2;239;182;182m")
				} else if background == DarkAddedBackground {
					b.WriteString("\x1b[48;2;42;83;52m")
				} else {
					b.WriteString("\x1b[48;2;100;38;43m")
				}
			} else {
				b.WriteString(background)
			}
		}
		if colorIDs[i] != color {
			b.WriteString("\x1b[39m")
			color = colorIDs[i]
			b.WriteString(palette[color])
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
	if color != 0 {
		b.WriteString("\x1b[39m")
	}
	if change {
		b.WriteString(background)
	}
	return b.String()
}

// Clean replaces control characters that would corrupt terminal output.
func Clean(s string) string {
	if printableASCII(s) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// MatchHunkHeader parses a unified diff hunk header.
func MatchHunkHeader(line string) []string {
	if !strings.HasPrefix(line, "@@ ") {
		return nil
	}
	return hunkHeader.FindStringSubmatch(line)
}

// Fold lowercases s unless the query contains an uppercase letter (smartcase),
// so "foo" matches "Foo" but "Foo" does not match "foo".
func Fold(s, query string) string {
	for _, r := range query {
		if unicode.IsUpper(r) {
			return s
		}
	}
	return strings.ToLower(s)
}

// ResetCache clears tokenized diffs; tests use it to observe caching.
func ResetCache() {
	syntaxCache.Lock()
	syntaxCache.entries = make(map[syntaxKey]syntaxResult)
	syntaxCache.Unlock()
}

// CacheSize reports how many tokenized diffs are cached.
func CacheSize() int {
	syntaxCache.Lock()
	defer syntaxCache.Unlock()
	return len(syntaxCache.entries)
}

// CachesWholeFile reports whether path and diff have a whole-file cache entry.
func CachesWholeFile(path, diff string) bool {
	syntaxCache.Lock()
	defer syntaxCache.Unlock()
	_, ok := syntaxCache.entries[syntaxKey{path, diff, ""}]
	return ok
}

func lightSyntaxColor(color string) string {
	switch color {
	case "\x1b[90m":
		return "\x1b[38;2;100;100;100m"
	case "\x1b[35m":
		return "\x1b[38;2;128;35;108m"
	case "\x1b[32m":
		return "\x1b[38;2;30;105;55m"
	case "\x1b[36m":
		return "\x1b[38;2;0;105;110m"
	case "\x1b[34m":
		return "\x1b[38;2;30;75;150m"
	default:
		return color
	}
}

// File is one file's tokenizing work: the full-context diff and the display
// diff that says which lines are on screen.
type File struct {
	Path, Diff, Display string
}

// Warm tokenizes the displayed lines of files ahead of time, off the UI
// goroutine, one file per core.
func Warm(files []File) {
	var wg sync.WaitGroup
	limit := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, file := range files {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer func() { <-limit; wg.Done() }()
			LinesFor(file.Path, file.Diff, file.Display)
		}()
	}
	wg.Wait()
}

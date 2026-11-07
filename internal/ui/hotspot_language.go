package ui

import (
	"sort"
	"strings"

	"github.com/jeffbrennan/pane/internal/syntax"
)

// The two buckets that are not languages: formats people edit around the
// code. They are listed below the language bar instead of in it, since a
// lockfile bump or a README pass says little about what someone writes.
const (
	hotConfigLanguage = "config & data"
	hotDocsLanguage   = "docs"
)

// hotLanguageExtensions names the languages worth telling apart by
// extension. Anything else falls back to the syntax package's lexer match.
var hotLanguageExtensions = map[string]string{
	".go":  "Go",
	".zig": "Zig",
	".c":   "C",
	// A header could be either; splitting it by the repository's mix is not
	// worth a second pass.
	".h":      "C/C++",
	".cc":     "C++",
	".cpp":    "C++",
	".cxx":    "C++",
	".hpp":    "C++",
	".hh":     "C++",
	".hxx":    "C++",
	".m":      "Objective-C",
	".mm":     "Objective-C",
	".rs":     "Rust",
	".ts":     "TypeScript",
	".tsx":    "TypeScript",
	".mts":    "TypeScript",
	".cts":    "TypeScript",
	".js":     "JavaScript",
	".jsx":    "JavaScript",
	".mjs":    "JavaScript",
	".cjs":    "JavaScript",
	".py":     "Python",
	".pyi":    "Python",
	".java":   "Java",
	".kt":     "Kotlin",
	".kts":    "Kotlin",
	".swift":  "Swift",
	".rb":     "Ruby",
	".php":    "PHP",
	".cs":     "C#",
	".scala":  "Scala",
	".sc":     "Scala",
	".sh":     "Shell",
	".bash":   "Shell",
	".zsh":    "Shell",
	".fish":   "Shell",
	".ps1":    "PowerShell",
	".psm1":   "PowerShell",
	".sql":    "SQL",
	".lua":    "Lua",
	".ex":     "Elixir",
	".exs":    "Elixir",
	".hs":     "Haskell",
	".html":   "HTML",
	".htm":    "HTML",
	".css":    "CSS",
	".scss":   "CSS",
	".sass":   "CSS",
	".less":   "CSS",
	".vue":    "Vue",
	".svelte": "Svelte",
	".cmake":  "CMake",
	".nix":    "Nix",
	".tf":     "Terraform",
	".tfvars": "Terraform",
	".hcl":    "Terraform",
	".proto":  "Protobuf",
	".md":     hotDocsLanguage,
	".mdx":    hotDocsLanguage,
	".rst":    hotDocsLanguage,
	".txt":    hotDocsLanguage,
	".adoc":   hotDocsLanguage,
	".json":   hotConfigLanguage,
	".jsonc":  hotConfigLanguage,
	".yaml":   hotConfigLanguage,
	".yml":    hotConfigLanguage,
	".toml":   hotConfigLanguage,
	".ini":    hotConfigLanguage,
	".cfg":    hotConfigLanguage,
	".xml":    hotConfigLanguage,
	".lock":   hotConfigLanguage,
	".lockb":  hotConfigLanguage,
	".csv":    hotConfigLanguage,
	".env":    hotConfigLanguage,
}

// hotLanguageBasenames covers files known by name rather than extension.
var hotLanguageBasenames = map[string]string{
	"dockerfile":     "Dockerfile",
	"containerfile":  "Dockerfile",
	"makefile":       "Makefile",
	"gnumakefile":    "Makefile",
	"justfile":       "Makefile",
	"cmakelists.txt": "CMake",
	"build.zig":      "Zig",
	"build.zig.zon":  "Zig",
	"go.mod":         hotConfigLanguage,
	"go.sum":         hotConfigLanguage,
	".gitignore":     hotConfigLanguage,
	".gitattributes": hotConfigLanguage,
	"license":        hotDocsLanguage,
	"readme":         hotDocsLanguage,
	"changelog":      hotDocsLanguage,
}

// hotLanguage names the language a path is written in, or "" when it cannot
// tell.
func hotLanguage(path string) string {
	base := strings.ToLower(path[strings.LastIndex(path, "/")+1:])
	if language, ok := hotLanguageBasenames[base]; ok {
		return language
	}
	if strings.HasPrefix(base, "dockerfile.") {
		return "Dockerfile"
	}
	ext := ""
	if dot := strings.LastIndex(base, "."); dot > 0 {
		ext = base[dot:]
	}
	if language, ok := hotLanguageExtensions[ext]; ok {
		return language
	}
	return syntax.LanguageName(base)
}

// hotLanguageStat is one author's work in one language, or the whole
// window's. The embedded area counts each commit once however many of its
// files share the language.
type hotLanguageStat struct {
	hotArea
	churn int
	// attention is the damped weight files rank by, so one bulk dump does not
	// make someone a C++ person.
	attention float64
}

// hotLanguageShare is one row of a language breakdown.
type hotLanguageShare struct {
	name  string
	stat  *hotLanguageStat
	share float64
}

// hotHeadline reports whether a language belongs in the bar, as opposed to
// the config and docs buckets listed under it.
func hotHeadline(language string) bool {
	return language != hotConfigLanguage && language != hotDocsLanguage
}

// hotLanguageShares ranks languages by attention. Headline languages share
// the bar among themselves; the buckets are measured against everything, so
// "config & data 9%" reads as a slice of all the person's work.
func hotLanguageShares(languages map[string]*hotLanguageStat) (headline, other []hotLanguageShare) {
	var total, code float64
	for name, stat := range languages {
		total += stat.attention
		if hotHeadline(name) {
			code += stat.attention
		}
	}
	for name, stat := range languages {
		if hotHeadline(name) {
			headline = append(headline, hotLanguageShare{name: name, stat: stat, share: stat.attention / max(code, 1e-9)})
		} else {
			other = append(other, hotLanguageShare{name: name, stat: stat, share: stat.attention / max(total, 1e-9)})
		}
	}
	for _, shares := range [][]hotLanguageShare{headline, other} {
		sort.Slice(shares, func(i, j int) bool {
			if shares[i].stat.attention != shares[j].stat.attention {
				return shares[i].stat.attention > shares[j].stat.attention
			}
			return shares[i].name < shares[j].name
		})
	}
	return headline, other
}

// hotRepoLanguages adds every author's languages into the window's. Each
// commit has one author, so the sums count each commit once.
func hotRepoLanguages(authors map[string]*hotAuthor) map[string]*hotLanguageStat {
	total := make(map[string]*hotLanguageStat)
	for _, author := range authors {
		for name, stat := range author.languages {
			sum := total[name]
			if sum == nil {
				sum = &hotLanguageStat{}
				total[name] = sum
			}
			sum.commits += stat.commits
			sum.churn += stat.churn
			sum.attention += stat.attention
			sum.timeline.add(stat.timeline)
			if sum.first.IsZero() || (!stat.first.IsZero() && stat.first.Before(sum.first)) {
				sum.first = stat.first
			}
			if stat.last.After(sum.last) {
				sum.last = stat.last
			}
		}
	}
	return total
}

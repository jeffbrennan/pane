package ui

import (
	"sort"
	"strings"

	"github.com/jeffbrennan/pane/internal/git"
)

// hotPerson is everyone one person has committed as: the names and emails
// that were merged into the name shown.
type hotPerson struct {
	names  []string
	emails []string
}

// hotIdentities merges author names that belong to one person. Names join
// when they commit with the same email, when a name is itself an email
// another name commits with (a user.name set to an address), or when they
// differ only in case and spacing. Each group shows its most used name that is
// not an address. A .mailmap, which Git already applies, is the precise fix;
// this covers repositories without one.
func hotIdentities(commits []git.HotCacheCommit) (map[string]string, map[string]*hotPerson) {
	parent := make(map[string]string)
	var find func(string) string
	find = func(key string) string {
		if parent[key] == "" || parent[key] == key {
			parent[key] = key
			return key
		}
		root := find(parent[key])
		parent[key] = root
		return root
	}
	union := func(a, b string) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[rb] = ra
		}
	}
	counts := make(map[string]int)
	// emails keeps each address as written, since git log --author matches
	// case-sensitively.
	emails := make(map[string]map[string]string)
	for _, commit := range commits {
		counts[commit.Author]++
		name := "n:" + authorNameKey(commit.Author)
		find(name)
		if email := hotEmailKey(commit.Email); email != "" {
			union(name, "e:"+email)
			if emails[commit.Author] == nil {
				emails[commit.Author] = make(map[string]string)
			}
			emails[commit.Author][email] = strings.TrimSpace(commit.Email)
		}
		if address := hotEmailKey(commit.Author); address != "" {
			union(name, "e:"+address)
		}
	}
	groups := make(map[string][]string)
	for name := range counts {
		root := find("n:" + authorNameKey(name))
		groups[root] = append(groups[root], name)
	}
	canonical := make(map[string]string, len(counts))
	people := make(map[string]*hotPerson, len(groups))
	for _, names := range groups {
		sort.Slice(names, func(i, j int) bool {
			left, right := names[i], names[j]
			if leftAddress, rightAddress := strings.Contains(left, "@"), strings.Contains(right, "@"); leftAddress != rightAddress {
				return !leftAddress
			}
			if counts[left] != counts[right] {
				return counts[left] > counts[right]
			}
			return left < right
		})
		person := &hotPerson{names: names}
		seen := make(map[string]bool)
		for _, name := range names {
			canonical[name] = names[0]
			for key, email := range emails[name] {
				if !seen[key] {
					seen[key] = true
					person.emails = append(person.emails, email)
				}
			}
		}
		sort.Strings(person.emails)
		people[names[0]] = person
	}
	return canonical, people
}

// hotSharedMailboxes are local parts that many people commit with, such as a
// default config or a forge's placeholder, so they never merge names.
var hotSharedMailboxes = map[string]bool{
	"root": true, "noreply": true, "no-reply": true, "nobody": true, "none": true,
	"user": true, "you": true, "git": true, "admin": true, "unknown": true,
	"test": true, "dev": true, "developer": true,
}

// hotEmailKey normalizes an email for matching, or returns "" when it cannot
// tell people apart.
func hotEmailKey(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 || strings.ContainsAny(email, " <>") {
		return ""
	}
	local, domain := email[:at], email[at+1:]
	if hotSharedMailboxes[local] || domain == "localhost" || strings.HasSuffix(domain, ".local") ||
		domain == "example.com" || domain == "(none)" || !strings.Contains(domain, ".") {
		return ""
	}
	return email
}

// hotAuthorPatterns matches every name and email a hotspot author merged, for
// git log --author.
func (m model) hotAuthorPatterns(name string) []string {
	var person *hotPerson
	if author := m.hotspots.authors[name]; author != nil {
		person = author.person
	}
	for _, author := range m.hotspots.scopeAuthors {
		if author.name == name && author.person != nil {
			person = author.person
		}
	}
	if person == nil {
		return []string{git.AuthorPattern(name)}
	}
	var patterns []string
	for _, alias := range person.names {
		patterns = append(patterns, git.AuthorPattern(alias))
	}
	for _, email := range person.emails {
		patterns = append(patterns, git.AuthorEmailPattern(email))
	}
	return patterns
}

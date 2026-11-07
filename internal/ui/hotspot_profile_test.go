package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/jeffbrennan/pane/internal/syntax"
)

func TestHotLanguageNamesPathsAndBuckets(t *testing.T) {
	cases := map[string]string{
		"src/main.go":             "Go",
		"src/bundler/bundle.zig":  "Zig",
		"build.zig":               "Zig",
		"include/api.h":           "C/C++",
		"src/js.cpp":              "C++",
		"web/app.tsx":             "TypeScript",
		"scripts/run.mjs":         "JavaScript",
		"Dockerfile":              "Dockerfile",
		"docker/Dockerfile.dev":   "Dockerfile",
		"Makefile":                "Makefile",
		"README.md":               hotDocsLanguage,
		"package.json":            hotConfigLanguage,
		"bun.lock":                hotConfigLanguage,
		"config/app.YAML":         hotConfigLanguage,
		"lib/widget.dart":         "Dart",
		"test/__snapshots__/x.ab": "",
		"bin/run":                 "",
	}
	for path, want := range cases {
		if got := hotLanguage(path); got != want {
			t.Errorf("hotLanguage(%q) = %q, want %q", path, got, want)
		}
	}
	// The fallback answers the same way again from its index.
	if hotLanguage("lib/other.dart") != "Dart" {
		t.Fatal("fallback lookup changed between calls")
	}
}

func TestHotspotLanguagesCountCommitsOncePerLanguage(t *testing.T) {
	now := time.Now()
	day := int64(24 * 60 * 60)
	commits := []git.HotCacheCommit{
		git.NewHotCacheCommit("c1", now.Unix()-day, "Ana", []git.HotCacheChange{
			{Path: "src/a.zig", Added: 3}, {Path: "src/b.zig", Added: 2}, {Path: "web/app.ts", Added: 1},
		}),
		// Vendored and generated paths say nothing about what someone writes.
		git.NewHotCacheCommit("c2", now.Unix()-day, "Ana", []git.HotCacheChange{
			{Path: "vendor/lib.rs", Added: 900}, {Path: "web/app.min.js", Added: 900},
		}),
	}
	// Small edits made often outrank one bulk dump.
	for i := 0; i < 12; i++ {
		commits = append(commits, git.NewHotCacheCommit("g"+string(rune('a'+i)), now.Unix()-int64(i)*day, "Ben", []git.HotCacheChange{{Path: "cmd/main.go", Added: 4, Deleted: 2}}))
	}
	commits = append(commits, git.NewHotCacheCommit("dump", now.Unix()-day, "Ben", []git.HotCacheChange{{Path: "third/big.c", Added: 20000}}))
	_, authors, _, _ := aggregateHotspots(commits, builtinPathMarks(commits), 0, now)
	ana := authors["Ana"].languages
	if ana["Zig"] == nil || ana["Zig"].commits != 1 || ana["Zig"].churn != 5 || ana["TypeScript"].commits != 1 {
		t.Fatalf("one commit counts once per language: %#v", ana)
	}
	if ana["Rust"] != nil || ana["JavaScript"] != nil {
		t.Fatalf("vendored and generated paths were counted: %#v", ana)
	}
	headline, _ := hotLanguageShares(authors["Ben"].languages)
	if len(headline) != 2 || headline[0].name != "Go" {
		t.Fatalf("attention should rank Go above the C dump: %+v", headline)
	}
	if got := hotLanguageSummary(authors["Ben"].languages, 3); len(got) == 0 || !strings.HasPrefix(got[0], "Go ") {
		t.Fatalf("summary: %v", got)
	}
}

func TestHotSlotPlacesAuthorLocalTime(t *testing.T) {
	cases := map[string]uint8{
		"2026-11-02 14:03:00 +0100": 1 + 14,        // Monday, 2pm where they were
		"2026-11-08 23:59:59 -0800": 1 + 6*24 + 23, // Sunday, late
		"2026-11-04 00:10:00 +0530": 1 + 2*24 + 0,  // Wednesday, just after midnight
		"not a date":                0,
	}
	for in, want := range cases {
		if got := git.HotSlot(in); got != want {
			t.Errorf("HotSlot(%q) = %d, want %d", in, got, want)
		}
	}
	var punch hotPunch
	punch.record(git.HotSlot("2026-11-02 14:03:00 +0100"))
	punch.record(0)
	if punch[0][14] != 1 || punch.peak() != 1 {
		t.Fatalf("punch card: %v", punch)
	}
}

func TestHotspotStreamRecordsWhenAuthorsWork(t *testing.T) {
	r := hotFixture(t)
	_, authors, err := hotspots(r, "2.year", "")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, day := range authors["Other"].punch {
		for _, count := range day {
			total += int(count)
		}
	}
	if total != 1 {
		t.Fatalf("Git's author date should land on the punch card once, got %d", total)
	}
}

func TestHotspotSummaryKeepsLanguagesAndPunch(t *testing.T) {
	_, authors := hotProfileFixture()
	path := filepath.Join(t.TempDir(), "hot.summary")
	writeHotSummary(path, "head", "1.year", "", 1, time.Now().Unix(), nil, authors, nil, nil, hotHidden{})
	_, restored, _, _, _, _, ok := readHotSummary(path, "head", "1.year", "")
	if !ok {
		t.Fatal("summary did not round-trip")
	}
	want, got := authors["Ana Lima"], restored["Ana Lima"]
	if got == nil || got.punch != want.punch || got.punch.peak() == 0 {
		t.Fatalf("punch card lost: %v", got)
	}
	for name, stat := range want.languages {
		cached := got.languages[name]
		if cached == nil || cached.commits != stat.commits || cached.churn != stat.churn || cached.attention != stat.attention ||
			cached.timeline != stat.timeline || cached.last.Unix() != stat.last.Unix() {
			t.Fatalf("%s did not round-trip: %+v want %+v", name, cached, stat)
		}
	}
}

// hotProfileFixture is three people: Ana under two names, mostly Go with
// some TypeScript; Ben beside her in Go; and Cy, whose name carries an emoji,
// in the TypeScript.
func hotProfileFixture() ([]hotFile, map[string]*hotAuthor) {
	now := time.Now()
	hour := int64(60 * 60)
	commit := func(hash string, ago int64, name, email string, paths ...string) git.HotCacheCommit {
		var changes []git.HotCacheChange
		for _, path := range paths {
			changes = append(changes, git.HotCacheChange{Path: path, Added: 5, Deleted: 1})
		}
		c := git.NewHotCacheCommit(hash, now.Unix()-ago*hour, name, changes)
		c.Email = email
		c.Slot = git.HotSlot(time.Unix(c.Time, 0).Format("2006-01-02 15:04:05 -0700"))
		return c
	}
	commits := []git.HotCacheCommit{
		commit("a1", 1, "Ana Lima", "ana@lima.dev", "src/a.go", "src/b.go"),
		commit("a2", 30, "Ana Lima", "ana@lima.dev", "src/a.go"),
		commit("a3", 60, "ana", "ana@lima.dev", "src/c.go", "web/app.ts"),
		commit("a4", 90, "Ana Lima", "ana@lima.dev", "README.md"),
		commit("b1", 2, "Ben Ode", "ben@ode.dev", "src/a.go"),
		commit("b2", 40, "Ben Ode", "ben@ode.dev", "src/b.go"),
		commit("c1", 3, "Cy ❤️", "cy@example.org", "web/app.ts"),
	}
	files, authors, _, _ := aggregateHotspots(commits, builtinPathMarks(commits), 0, now)
	return files, authors
}

func hotProfileModel(width, height int) model {
	files, authors := hotProfileFixture()
	return model{width: width, height: height, screen: "hotspots", hotspots: hotspotState{
		mode: "authors", since: "1.year", files: files, authors: authors, items: &hotItemsCache{},
	}}
}

func pressHot(t *testing.T, m model, key string) model {
	t.Helper()
	next, _ := m.handleHotspotKey(keyMsg(key))
	return next.(model)
}

func TestHotspotProfileOpensStepsAndReturns(t *testing.T) {
	m := hotProfileModel(120, 40)
	entries := m.hotAuthorEntries()
	if len(entries) != 3 || entries[0].author.name != "Ana Lima" {
		t.Fatalf("fixture order: %v", entries)
	}
	m = pressHot(t, m, "p")
	if m.hotspots.mode != "profile" || m.hotspots.profile.author != "Ana Lima" {
		t.Fatalf("p should open Ana's profile: mode %q author %q", m.hotspots.mode, m.hotspots.profile.author)
	}
	// ( stops at the first person instead of wrapping.
	if m = pressHot(t, m, "("); m.hotspots.profile.author != "Ana Lima" {
		t.Fatalf("( at the start moved to %q", m.hotspots.profile.author)
	}
	m = pressHot(t, m, ")")
	if m.hotspots.profile.author != "Ben Ode" {
		t.Fatalf(") should step to Ben, got %q", m.hotspots.profile.author)
	}
	m = pressHot(t, m, ")")
	if m = pressHot(t, m, ")"); m.hotspots.profile.author != "Cy ❤️" {
		t.Fatalf(") should stop at the last person, got %q", m.hotspots.profile.author)
	}
	m = pressHot(t, m, "esc")
	if m.hotspots.mode != "authors" || m.hotAuthorEntries()[m.hotspots.list.selected].author.name != "Cy ❤️" {
		t.Fatalf("esc should land on the person last shown: mode %q selected %d", m.hotspots.mode, m.hotspots.list.selected)
	}
	m.hotspots.expanded = map[string]bool{"Ana Lima": true}
	m.hotspots.list.selected = 0
	m = pressHot(t, pressHot(t, m, "p"), "p")
	if m.hotspots.mode != "authors" || !m.hotspots.expanded["Ana Lima"] || m.hotspots.list.selected != 0 {
		t.Fatalf("p should close the profile and keep the list as it was: %q %v %d", m.hotspots.mode, m.hotspots.expanded, m.hotspots.list.selected)
	}
}

func TestHotspotProfileWindowKeepsThePersonOrReturns(t *testing.T) {
	m := pressHot(t, hotProfileModel(120, 40), "p")
	ben := m.hotspots.authors["Ben Ode"]
	m.hotspots.windows = map[string]*hotWindow{
		"": {authors: map[string]*hotAuthor{"Ben Ode": ben}, at: time.Now()},
	}
	// 1.year cycles to all time, where only Ben committed.
	next, _ := m.switchHotWindow(nextHotWindow(m.hotspots.since))
	gone := next.(model)
	if gone.hotspots.mode != "authors" || !strings.Contains(gone.flash, "Ana Lima") {
		t.Fatalf("a window without Ana should return to the list: mode %q flash %q", gone.hotspots.mode, gone.flash)
	}
	m = pressHot(t, m, ")")
	next, _ = m.switchHotWindow(nextHotWindow(m.hotspots.since))
	kept := next.(model)
	if kept.hotspots.mode != "profile" || kept.hotspots.profile.author != "Ben Ode" {
		t.Fatalf("Ben's profile should survive the window switch: mode %q author %q", kept.hotspots.mode, kept.hotspots.profile.author)
	}
}

func TestHotspotProfileShowsLanguagesAreasFilesAndPeople(t *testing.T) {
	m := pressHot(t, hotProfileModel(120, 40), "p")
	view := ansi.Strip(m.hotView())
	for _, want := range []string{"Ana Lima", "also ana, ana@lima.dev", "languages", "Go", "TypeScript", "also docs",
		"areas", "src/", "src/a.go", "works alongside", "Ben Ode", syntax.Clean("Cy ❤️"), "when they work", "Mon"} {
		if !strings.Contains(view, want) {
			t.Fatalf("profile is missing %q:\n%s", want, view)
		}
	}
	// Her badge appears once, in the header, not again among her
	// collaborators.
	if strings.Count(view, "AL Ana Lima") != 1 {
		t.Fatalf("Ana should not be listed beside herself:\n%s", view)
	}
}

func TestHotspotProfileFitsEveryWidthAndDropsSectionsInOrder(t *testing.T) {
	for _, width := range []int{80, 99, 100, 160} {
		m := pressHot(t, hotProfileModel(width, 40), "p")
		for i, line := range strings.Split(m.hotView(), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width %d: line %d is %d cells: %q", width, i, w, ansi.Strip(line))
			}
		}
	}
	// A short single-column screen keeps languages and areas and drops the
	// week first.
	m := pressHot(t, hotProfileModel(80, 16), "p")
	view := ansi.Strip(m.hotView())
	if !strings.Contains(view, "languages") || !strings.Contains(view, "areas") || strings.Contains(view, "when they work") {
		t.Fatalf("short screen should keep the top sections only:\n%s", view)
	}
	if lines := strings.Split(m.hotView(), "\n"); len(lines) != 16 {
		t.Fatalf("profile should fill exactly the screen, got %d lines", len(lines))
	}
}

func TestHotspotProfileEmojiNamesKeepColumnsAligned(t *testing.T) {
	m := pressHot(t, hotProfileModel(80, 40), "p")
	var rows []string
	for _, line := range strings.Split(ansi.Strip(m.hotView()), "\n") {
		if strings.Contains(line, "Ben Ode") || strings.Contains(line, syntax.Clean("Cy ❤️")) {
			if strings.Contains(line, "%") {
				rows = append(rows, line)
			}
		}
	}
	if len(rows) != 2 {
		t.Fatalf("alongside rows: %q", rows)
	}
	end := func(line string) int { return ansi.StringWidth(strings.TrimRight(line, " ")) }
	if end(rows[0]) != end(rows[1]) {
		t.Fatalf("counts should end in one column:\n%s\n%s", rows[0], rows[1])
	}
}

func TestHotspotProfileEnterOpensTheSelection(t *testing.T) {
	m := pressHot(t, hotProfileModel(120, 40), "p")
	rows := hotProfileRows(m.hotProfileLayout(m.hotProfileSubject("Ana Lima"), time.Now()))
	index := func(kind, key string) int {
		for i, row := range rows {
			if row.kind == kind && (key == "" || row.key == key) {
				return i
			}
		}
		t.Fatalf("no %s row %q in %v", kind, key, rows)
		return 0
	}
	// A language opens the files view narrowed to Ana and that language.
	m.hotspots.profile.list.selected = index("language", "TypeScript")
	files := pressHot(t, m, "enter")
	if files.hotspots.mode != "files" || files.hotspots.authorFilter != "Ana Lima" || files.hotspots.languageFilter != "TypeScript" {
		t.Fatalf("language enter: mode %q filter %q/%q", files.hotspots.mode, files.hotspots.authorFilter, files.hotspots.languageFilter)
	}
	if items := files.hotFileItems(); len(items) != 1 || items[0].path != "web/app.ts" {
		t.Fatalf("files view should list Ana's TypeScript only: %v", hotItemPaths(items))
	}
	if !strings.Contains(ansi.Strip(files.hotView()), "by Ana Lima in TypeScript") {
		t.Fatalf("title should name both filters:\n%s", ansi.Strip(files.hotView()))
	}
	if cleared := pressHot(t, files, "esc"); cleared.hotspots.authorFilter != "" || cleared.hotspots.languageFilter != "" {
		t.Fatalf("esc should clear both filters")
	}
	// A file opens Ana's history there under every name she used.
	m.hotspots.profile.list.selected = index("file", "src/a.go")
	next, _ := m.handleHotspotKey(tea.KeyMsg{Type: tea.KeyEnter})
	history := next.(model)
	patterns := strings.Join(history.history.authorPatterns, "\n")
	if history.screen != "history" || history.history.path != "src/a.go" || !strings.Contains(patterns, "^ana <") || !strings.Contains(patterns, "ana@lima") {
		t.Fatalf("file enter: screen %q path %q patterns %q", history.screen, history.history.path, patterns)
	}
	// A person opens their profile.
	m.hotspots.profile.list.selected = index("person", "Ben Ode")
	if ben := pressHot(t, m, "enter"); ben.hotspots.mode != "profile" || ben.hotspots.profile.author != "Ben Ode" {
		t.Fatalf("person enter: %q", ben.hotspots.profile.author)
	}
}

func TestHotspotAuthorDetailNamesLanguages(t *testing.T) {
	m := hotProfileModel(120, 30)
	view := ansi.Strip(m.hotView())
	if !strings.Contains(view, "Ana Lima · Go ") || !strings.Contains(view, "TypeScript") || !strings.Contains(view, "4 commits") {
		t.Fatalf("detail line should sum up the author:\n%s", view)
	}
}

func TestHotspotBotProfileListsPeople(t *testing.T) {
	now := time.Now().Unix()
	commits := []git.HotCacheCommit{
		git.NewHotCacheCommit("b1", now, "release-bot", []git.HotCacheChange{{Path: "src/a.go", Added: 1}}),
		git.NewHotCacheCommit("h1", now, "Hana", []git.HotCacheChange{{Path: "src/a.go", Added: 1}}),
	}
	files, authors, _, _ := aggregateHotspots(commits, builtinPathMarks(commits), 0, time.Now())
	m := model{width: 120, height: 40, hotspots: hotspotState{mode: "authors", files: files, authors: authors, items: &hotItemsCache{}}}
	m.hotspots.list.selected = 1
	m = pressHot(t, m, "p")
	if m.hotspots.profile.author != "release-bot" || !strings.Contains(ansi.Strip(m.hotView()), "Hana") {
		t.Fatalf("a bot's profile shows who works alongside it:\n%s", ansi.Strip(m.hotView()))
	}
}

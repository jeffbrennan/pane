package ui

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jeffbrennan/pane/internal/syntax"
)

// hotPunch counts an author's commits by weekday, Monday first, and hour in
// their own time zone.
type hotPunch [7][24]uint16

func (p *hotPunch) record(slot uint8) {
	if slot == 0 || slot > 7*24 {
		return
	}
	day, hour := (slot-1)/24, (slot-1)%24
	if p[day][hour] < math.MaxUint16 {
		p[day][hour]++
	}
}

func (p hotPunch) peak() float64 {
	peak := 0.0
	for _, day := range p {
		for _, count := range day {
			peak = max(peak, float64(count))
		}
	}
	return peak
}

// hotProfile is the profile screen opened from the authors view: whose it
// is, the selected row, and what was derived for them. The authors list keeps
// its own state underneath, so leaving returns to it.
type hotProfile struct {
	author string
	list   listState
	cache  *hotProfileCache
}

func newHotProfile(author string) hotProfile {
	return hotProfile{author: author, cache: &hotProfileCache{}}
}

type hotProfileKey struct {
	author      string
	dataVersion int
	hiddenShown bool
}

// hotProfileCache holds what a profile derives from every file in the
// window, built once per person and data version rather than per frame.
type hotProfileCache struct {
	key       hotProfileKey
	ready     bool
	files     []hotProfileFile
	alongside []hotAlongside
	repo      map[string]*hotLanguageStat
}

// hotProfileFile is one file the person changed, with their share of its
// churn.
type hotProfileFile struct {
	file  *hotFile
	share float64
}

// hotAlongside is someone who changed the same files. overlap is how many of
// the person's changed lines were in files the other also changed, counted
// up to the other's own lines there.
type hotAlongside struct {
	name    string
	files   int
	overlap int
}

// The most rows each list section offers before the screen height trims it.
const (
	hotProfileLanguages = 6
	hotProfileAreas     = 6
	hotProfileFiles     = 8
	hotProfileAlongside = 5
)

// hotProfileSubject finds the window's author for a name, which may be one of
// the aliases merged into them: a path's authors are merged over fewer
// commits, so they can show another of the person's names.
func (m model) hotProfileSubject(name string) *hotAuthor {
	if author := m.hotspots.authors[name]; author != nil {
		return author
	}
	for _, author := range m.hotspots.authors {
		if author.person != nil && slices.Contains(author.person.names, name) {
			return author
		}
	}
	return nil
}

// hotProfileData derives the person's files and collaborators from the
// window's files, which a cached summary restores without commits.
func (m model) hotProfileData(author *hotAuthor) *hotProfileCache {
	cache := m.hotspots.profile.cache
	if cache == nil {
		cache = &hotProfileCache{}
	}
	key := hotProfileKey{author: author.name, dataVersion: m.hotspots.dataVersion, hiddenShown: m.hotspots.hiddenShown}
	if cache.ready && cache.key == key {
		return cache
	}
	var files []hotProfileFile
	shared := make(map[string]*hotAlongside)
	for i := range m.hotspots.files {
		file := &m.hotspots.files[i]
		mine := file.authors[author.name]
		if mine == 0 {
			continue
		}
		hidden := file.class == hotGenerated || file.class == hotVendored
		if hidden && !m.hotspots.hiddenShown {
			continue
		}
		files = append(files, hotProfileFile{file: file, share: float64(mine) / float64(max(1, file.added+file.deleted))})
		if hidden {
			// Regenerating the same lockfile is not working together.
			continue
		}
		for name, theirs := range file.authors {
			if name == author.name || theirs == 0 || isBotAuthor(name) {
				continue
			}
			other := shared[name]
			if other == nil {
				other = &hotAlongside{name: name}
				shared[name] = other
			}
			other.files++
			other.overlap += min(mine, theirs)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		left, right := files[i].share*files[i].file.score, files[j].share*files[j].file.score
		if left != right {
			return left > right
		}
		return files[i].file.path < files[j].file.path
	})
	alongside := make([]hotAlongside, 0, len(shared))
	for _, other := range shared {
		alongside = append(alongside, *other)
	}
	sort.Slice(alongside, func(i, j int) bool {
		if alongside[i].overlap != alongside[j].overlap {
			return alongside[i].overlap > alongside[j].overlap
		}
		return alongside[i].name < alongside[j].name
	})
	*cache = hotProfileCache{
		key: key, ready: true,
		files:     files[:min(hotProfileFiles, len(files))],
		alongside: alongside[:min(hotProfileAlongside, len(alongside))],
		repo:      hotRepoLanguages(m.hotspots.authors),
	}
	return cache
}

// hotProfileRow identifies a row the selection can rest on.
type hotProfileRow struct{ kind, key string }

type hotProfileItem struct {
	row hotProfileRow
	// text renders the row in width cells, without the selection marker.
	text func(width int) string
}

// hotProfileSection is one titled block. Items are selectable; notes are the
// dim lines that follow them.
type hotProfileSection struct {
	title string
	items []hotProfileItem
	notes []string
}

func (s hotProfileSection) height() int {
	if len(s.items) == 0 && len(s.notes) == 0 {
		return 0
	}
	return 1 + len(s.items) + len(s.notes)
}

// fit trims the section's items to rows lines. A section that cannot keep its
// title, notes and one item does not fit at all.
func (s hotProfileSection) fit(rows int) (hotProfileSection, bool) {
	if s.height() == 0 {
		return s, false
	}
	if s.height() <= rows {
		return s, true
	}
	keep := rows - 1 - len(s.notes)
	if len(s.items) == 0 || keep < 1 {
		return s, false
	}
	s.items = s.items[:keep]
	return s, true
}

// hotProfileBlock is one band of the screen: a section across the width, or
// two side by side on a wide terminal.
type hotProfileBlock struct{ left, right *hotProfileSection }

func (b hotProfileBlock) sections() []*hotProfileSection {
	var sections []*hotProfileSection
	for _, section := range []*hotProfileSection{b.left, b.right} {
		if section != nil {
			sections = append(sections, section)
		}
	}
	return sections
}

func (b hotProfileBlock) height() int {
	height := 0
	for _, section := range b.sections() {
		height = max(height, section.height())
	}
	return height
}

// hotProfileWide is the width from which sections pair up in two columns.
const hotProfileWide = 100

// hotProfileHeader is the lines above the sections: who, how much, and when.
const hotProfileHeader = 2

func (m model) hotProfileColumns() bool { return m.width >= hotProfileWide }

// hotProfileColumnWidth is the width a section renders in, after the screen's
// one-cell margin and, in two columns, the gutter between them.
func (m model) hotProfileColumnWidth(full bool) int {
	if full || !m.hotProfileColumns() {
		return max(1, m.width-1)
	}
	return max(1, (m.width-1-3)/2)
}

// hotProfileLayout picks the sections that fit, in priority order: languages,
// areas, files, collaborators, then the week. The first band that does not fit
// whole is trimmed, and everything after it is dropped.
func (m model) hotProfileLayout(author *hotAuthor, now time.Time) []hotProfileBlock {
	sections := m.hotProfileSections(author, now)
	var bands [][]*hotProfileSection
	if m.hotProfileColumns() {
		bands = [][]*hotProfileSection{{sections[0], sections[1]}, {sections[2]}, {sections[3], sections[4]}}
	} else {
		for _, section := range sections {
			bands = append(bands, []*hotProfileSection{section})
		}
	}
	rows := max(0, m.height-2-hotProfileHeader)
	var blocks []hotProfileBlock
	for _, band := range bands {
		// A blank line sets each band off from the one above.
		room := rows - 1
		var fitted []*hotProfileSection
		trimmed := false
		for _, section := range band {
			fit, ok := section.fit(room)
			if !ok {
				fitted = append(fitted, nil)
				trimmed = trimmed || section.height() > 0
				continue
			}
			trimmed = trimmed || fit.height() < section.height()
			fitted = append(fitted, &fit)
		}
		block := hotProfileBlock{left: fitted[0]}
		if len(fitted) > 1 {
			block.right = fitted[1]
		}
		if block.height() > 0 {
			blocks = append(blocks, block)
			rows -= 1 + block.height()
		}
		if trimmed {
			break
		}
	}
	return blocks
}

// hotProfileRows lists the selectable rows the layout shows, in reading
// order.
func hotProfileRows(blocks []hotProfileBlock) []hotProfileRow {
	var rows []hotProfileRow
	for _, block := range blocks {
		for _, section := range block.sections() {
			for _, item := range section.items {
				rows = append(rows, item.row)
			}
		}
	}
	return rows
}

// hotProfileSelected is the row under the selection, clamped to what shows.
func (m model) hotProfileSelected(rows []hotProfileRow) hotProfileRow {
	if len(rows) == 0 {
		return hotProfileRow{}
	}
	return rows[max(0, min(m.hotspots.profile.list.selected, len(rows)-1))]
}

func (m model) hotProfileColor(name string) string {
	if isBotAuthor(name) {
		// Bots stay dim, as in the authors view.
		return ""
	}
	return m.authorNameColor(name)
}

// hotProfileSections builds every section in priority order, empty ones
// included, so the layout can pair them by position.
func (m model) hotProfileSections(author *hotAuthor, now time.Time) []*hotProfileSection {
	data := m.hotProfileData(author)
	color := m.hotProfileColor(author.name)
	peak := author.timeline.peak()

	languages := &hotProfileSection{title: "languages"}
	headline, other := hotLanguageShares(author.languages)
	headline = headline[:min(hotProfileLanguages, len(headline))]
	nameWidth := 0
	for _, language := range headline {
		nameWidth = max(nameWidth, ansi.StringWidth(language.name))
	}
	nameWidth = min(nameWidth, 14)
	for _, language := range headline {
		languages.items = append(languages.items, hotProfileItem{
			row: hotProfileRow{kind: "language", key: language.name},
			text: func(width int) string {
				barWidth := max(4, min(16, width-nameWidth-2-6-2-hotBuckets))
				filled := int(math.Round(language.share * float64(barWidth)))
				if language.share > 0 {
					filled = max(1, filled)
				}
				bar := styleNormal + color + strings.Repeat("█", filled) + styleReset + strings.Repeat("░", barWidth-filled)
				text := padCell(language.name, nameWidth) + "  " + bar + fmt.Sprintf("  %3d%%", int(math.Round(language.share*100))) +
					"  " + hotAreaStrip(language.stat.timeline, peak, color)
				if repo := data.repo[language.name]; repo != nil && repo.attention > language.stat.attention*1.001 {
					tail := fmt.Sprintf("  %d%% of the repo's", int(math.Round(language.stat.attention/repo.attention*100)))
					if ansi.StringWidth(text)+ansi.StringWidth(tail) <= width {
						text += tail
					}
				}
				return text
			},
		})
	}
	var buckets []string
	for _, language := range other {
		if percent := int(math.Round(language.share * 100)); percent >= 1 {
			buckets = append(buckets, fmt.Sprintf("%s %d%%", language.name, percent))
		}
	}
	if len(buckets) > 0 {
		languages.notes = append(languages.notes, "also "+strings.Join(buckets, " · "))
	}

	areas := &hotProfileSection{title: "areas"}
	dirs := hotAuthorAreas(author)
	dirs = dirs[:min(hotProfileAreas, len(dirs))]
	areaWidth := 0
	for _, dir := range dirs {
		areaWidth = max(areaWidth, ansi.StringWidth(syntax.Clean(hotProfileArea(dir))))
	}
	areaWidth = min(areaWidth, 24)
	for _, dir := range dirs {
		area := author.areas[dir]
		areas.items = append(areas.items, hotProfileItem{
			row: hotProfileRow{kind: "area", key: dir},
			text: func(width int) string {
				name := padCell(syntax.Clean(hotProfileArea(dir)), min(areaWidth, max(4, width-2-5-2-hotBuckets)))
				text := name + fmt.Sprintf("  %3d%%", area.commits*100/max(1, author.commits)) + "  " + hotAreaStrip(area.timeline, peak, color)
				if span := "  " + hotSpan(area.first, area.last, now); ansi.StringWidth(text)+ansi.StringWidth(span) <= width {
					text += span
				}
				return text
			},
		})
	}

	files := &hotProfileSection{title: "most changed files · their share of the churn"}
	filePeak, pathWidth := 0.0, 0
	for _, file := range data.files {
		filePeak = max(filePeak, file.file.timeline.peak())
		pathWidth = max(pathWidth, ansi.StringWidth(syntax.Clean(file.file.path)))
	}
	for _, file := range data.files {
		files.items = append(files.items, hotProfileItem{
			row: hotProfileRow{kind: "file", key: file.file.path},
			text: func(width int) string {
				columns := hotStrip(file.file.timeline, filePeak, "") + fmt.Sprintf("  %12s  %3d%%", hotCommits(file.file.commits), int(math.Round(file.share*100)))
				return hotPathCell(syntax.Clean(file.file.path), max(8, min(pathWidth, width-2-ansi.StringWidth(columns)))) + "  " + columns
			},
		})
	}

	alongside := &hotProfileSection{title: "works alongside · share of their lines · files"}
	alongsideWidth := 0
	for _, other := range data.alongside {
		alongsideWidth = max(alongsideWidth, ansi.StringWidth(syntax.Clean(other.name)))
	}
	alongsideWidth = min(alongsideWidth, 24)
	for _, other := range data.alongside {
		alongside.items = append(alongside.items, hotProfileItem{
			row: hotProfileRow{kind: "person", key: other.name},
			text: func(width int) string {
				badge := styleNormal + m.hotProfileColor(other.name) + authorInitials(other.name) + styleReset
				count := fmt.Sprintf("  %3d%%  %6s", other.overlap*100/max(1, author.churn), formatCount(other.files))
				name := padCell(syntax.Clean(other.name), max(4, min(alongsideWidth, width-hotBadgeWidth-len(count))))
				return badge + " " + name + count
			},
		})
	}

	week := &hotProfileSection{title: "when they work · their local time"}
	if punchPeak := author.punch.peak(); punchPeak > 0 {
		hours := []rune(strings.Repeat(" ", 24))
		for _, hour := range []int{0, 6, 12, 18} {
			copy(hours[hour:], []rune(fmt.Sprint(hour)))
		}
		week.notes = append(week.notes, "    "+string(hours))
		for day, name := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
			values := make([]float64, 24)
			for hour, count := range author.punch[day] {
				values[hour] = float64(count)
			}
			week.notes = append(week.notes, name+" "+hotDrawStrip(values, punchPeak, color, func(v float64) float64 { return math.Sqrt(v / punchPeak) }))
		}
	}
	return []*hotProfileSection{languages, areas, files, alongside, week}
}

func hotProfileArea(dir string) string {
	if dir == "." {
		return dirLabel(dir)
	}
	return dir + "/"
}

// hotPathCell fits a path to width cells, cutting from the left so the file
// name survives.
func hotPathCell(path string, width int) string {
	if ansi.StringWidth(path) <= width {
		return padCell(path, width)
	}
	runes := []rune(path)
	for len(runes) > 0 && ansi.StringWidth(string(runes))+1 > width {
		runes = runes[1:]
	}
	return padCell("…"+string(runes), width)
}

// hotProfileSectionLines renders a section at width, marking the selection.
func (m model) hotProfileSectionLines(section *hotProfileSection, width int, selected hotProfileRow) []string {
	if section == nil {
		return nil
	}
	lines := []string{truncateCell(section.title, width)}
	for _, item := range section.items {
		text := ansi.Truncate(item.text(max(1, width-2)), max(1, width-2), "")
		if item.row == selected {
			lines = append(lines, m.accent()+"▸ "+styleReset+text+styleReset)
			continue
		}
		lines = append(lines, styleDim+"  "+strings.ReplaceAll(text, styleReset, styleReset+styleDim)+styleReset)
	}
	for _, note := range section.notes {
		lines = append(lines, styleDim+"  "+strings.ReplaceAll(ansi.Truncate(note, max(1, width-2), ""), styleReset, styleReset+styleDim)+styleReset)
	}
	return lines
}

// hotProfileHeaderLines are the name, totals, activity and merged aliases.
func (m model) hotProfileHeaderLines(author *hotAuthor, now time.Time) []string {
	color := m.hotProfileColor(author.name)
	badge := authorInitials(author.name)
	if isBotAuthor(author.name) {
		badge = "⚙ "
	}
	summary := hotCommits(author.commits)
	if active := hotActive(author, now); active != "" {
		summary += " · " + active
	}
	first := " " + styleNormal + color + badge + styleReset + " " + syntax.Clean(author.name) + styleDim + "  " + summary + styleReset
	second := styleDim + " activity " + hotAreaStrip(author.timeline, author.timeline.peak(), color) + styleDim
	if aliases := hotProfileAliases(author); aliases != "" {
		second += "  also " + syntax.Clean(aliases)
	}
	second += styleReset
	return []string{ansi.Truncate(first, m.width, "") + styleReset, ansi.Truncate(second, m.width, "") + styleReset}
}

// hotProfileAliases lists the other names and the emails merged into the
// author, so an identity merge can be checked.
func hotProfileAliases(author *hotAuthor) string {
	if author.person == nil {
		return ""
	}
	var aliases []string
	for _, name := range author.person.names {
		if name != author.name {
			aliases = append(aliases, name)
		}
	}
	aliases = append(aliases, author.person.emails...)
	return strings.Join(aliases, ", ")
}

// hotProfilePeople is who ( and ) step through: the authors list the
// profile was opened from, in its order and filter.
func (m model) hotProfilePeople() []string {
	var people []string
	seen := make(map[string]bool)
	for _, author := range m.hotFilteredAuthors() {
		if subject := m.hotProfileSubject(author.name); subject != nil && !seen[subject.name] {
			seen[subject.name] = true
			people = append(people, subject.name)
		}
	}
	return people
}

func (m model) hotProfileView() string {
	now := time.Now()
	name := m.hotspots.profile.author
	author := m.hotProfileSubject(name)
	title := fmt.Sprintf(" hotspots · %s · %s", m.hotWindowLabel(), name)
	position := ""
	if people := m.hotProfilePeople(); len(people) > 1 {
		if i := slices.Index(people, name); i >= 0 {
			position = fmt.Sprintf("%d of %d", i+1, len(people))
		}
	}
	lines := []string{m.titleBar(" "+m.accent()+syntax.Clean(title)+styleReset, stateBadges(position))}
	if author == nil {
		text := " No commits by " + syntax.Clean(name) + " in the window"
		if m.hotspots.loading || m.hotspots.stream != nil {
			text = " Loading hotspots…"
		}
		lines = append(lines, fit(text, m.width))
	} else {
		lines = append(lines, m.hotProfileHeaderLines(author, now)...)
		blocks := m.hotProfileLayout(author, now)
		selected := m.hotProfileSelected(hotProfileRows(blocks))
		for _, block := range blocks {
			lines = append(lines, "")
			if block.right == nil || !m.hotProfileColumns() {
				for _, line := range m.hotProfileSectionLines(block.left, m.hotProfileColumnWidth(block.right == nil), selected) {
					lines = append(lines, " "+line)
				}
				continue
			}
			width := m.hotProfileColumnWidth(false)
			left := m.hotProfileSectionLines(block.left, width, selected)
			right := m.hotProfileSectionLines(block.right, width, selected)
			for i := 0; i < max(len(left), len(right)); i++ {
				line := strings.Repeat(" ", width)
				if i < len(left) {
					line = padCell(left[i], width)
				}
				if i < len(right) {
					line += "   " + right[i]
				}
				lines = append(lines, " "+line)
			}
		}
	}
	lines = lines[:min(len(lines), max(1, m.height-1))]
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	status := ""
	if m.flash != "" {
		status = syntax.Clean(m.flash)
	}
	hints := footer(true, hint("open", "open"), hint("people", "previous-commit", "next-commit"), hint("window", "hotspots.window"))
	lines = append(lines, m.statusBar(status, truncateCell(hints, max(1, m.width-ansi.StringWidth(status)-2))))
	if m.modal == "help" {
		clearBody(lines)
		m.showHelpDialog(lines)
	}
	return strings.Join(lines[:m.height], "\n")
}

// openHotProfile shows the selected author's profile.
func (m *model) openHotProfile() {
	entries := m.hotAuthorEntries()
	if len(entries) == 0 {
		return
	}
	subject := m.hotProfileSubject(entries[min(m.hotspots.list.selected, len(entries)-1)].author.name)
	if subject == nil {
		return
	}
	m.hotspots.profile = newHotProfile(subject.name)
	m.hotspots.mode = "profile"
}

// closeHotProfile returns to the authors list on the row of the person the
// profile last showed, which stepping may have changed.
func (m *model) closeHotProfile() {
	name := m.hotspots.profile.author
	m.hotspots.mode, m.hotspots.profile = "authors", hotProfile{}
	for i, entry := range m.hotAuthorEntries() {
		if entry.area != "" {
			continue
		}
		if subject := m.hotProfileSubject(entry.author.name); subject != nil && subject.name == name {
			m.hotspots.list.selected = i
			break
		}
	}
}

// stepHotProfile moves to the previous or next person in the list, stopping
// at either end as lists do.
func (m *model) stepHotProfile(delta int) {
	people := m.hotProfilePeople()
	if len(people) == 0 {
		return
	}
	i := slices.Index(people, m.hotspots.profile.author)
	next := max(0, min(len(people)-1, i+delta))
	if i < 0 {
		next = 0
	}
	if people[next] != m.hotspots.profile.author {
		m.hotspots.profile = newHotProfile(people[next])
	}
}

func (m model) handleHotProfileKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	var rows []hotProfileRow
	if author := m.hotProfileSubject(m.hotspots.profile.author); author != nil {
		rows = hotProfileRows(m.hotProfileLayout(author, time.Now()))
	}
	p := m.listPress(&m.hotspots.profile.list, key, scopeHotspots)
	if p.action == actSearch {
		// Nothing on a profile to filter.
		return m, nil
	}
	if m.handleListKey(&m.hotspots.profile.list, p, len(rows), max(1, len(rows)), nil) {
		return m, nil
	}
	if next, cmd, ok := m.handleSharedAction(p); ok {
		return next, cmd
	}
	switch p.action {
	case actBack, actQuit, actProfile:
		m.closeHotProfile()
	case actInterrupt:
		m.closeHotspotStream()
		return m, tea.Quit
	case actPrevCommit:
		m.stepHotProfile(-1)
	case actNextCommit:
		m.stepHotProfile(1)
	case actWindow:
		return m.switchHotWindow(nextHotWindow(m.hotspots.since))
	case actShowHidden:
		m.toggleHotHidden()
	case actOpen:
		row := m.hotProfileSelected(rows)
		name := m.hotspots.profile.author
		switch row.kind {
		case "language":
			m.hotspots.authorFilter, m.hotspots.languageFilter = name, row.key
			m.hotspots.mode, m.hotspots.profile = "files", hotProfile{}
			m.hotspots.scope, m.hotspots.scopeAuthors = "", nil
			m.hotspots.list.reset()
			if m.hotspots.items != nil {
				m.hotspots.items.filled = false
			}
		case "area", "file":
			m.closeHotspotStream()
			return m.openAuthorPathHistory(row.key, name)
		case "person":
			m.hotspots.profile = newHotProfile(row.key)
		}
	}
	return m, nil
}

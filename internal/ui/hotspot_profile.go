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

// hotPunchHours is how many hours each column of the week chart covers.
const hotPunchHours = 2

// hotPunchColumns is how many columns a day has in the week chart.
const hotPunchColumns = 24 / hotPunchHours

// columns sums each day's hours into the chart's columns.
func (p hotPunch) columns() [7][hotPunchColumns]float64 {
	var columns [7][hotPunchColumns]float64
	for day, hours := range p {
		for hour, count := range hours {
			columns[day][hour/hotPunchHours] += float64(count)
		}
	}
	return columns
}

func (p hotPunch) peak() float64 {
	peak := 0.0
	for _, day := range p.columns() {
		for _, count := range day {
			peak = max(peak, count)
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

// hotProfileSection is one boxed panel. Items are selectable; notes are the
// dim lines that follow them, drawn at the width the box gives them. label heads the columns that rows align flush
// right beneath it.
type hotProfileSection struct {
	title, label string
	items        []hotProfileItem
	notes        []func(width int) string
	// width is the widest row or note at its natural size, without the
	// selection marker, so boxes can be sized to what they hold.
	width int
	// least is the narrowest the content can be drawn at once its
	// adjustable columns shrink; 0 means it cannot shrink.
	least int
	// stretch boxes the section across the whole screen.
	stretch bool
	// tall redraws the notes at a height, tallest first, so a chart can use
	// the room the screen has left; nil keeps the notes as they are.
	tall func(level int) []func(width int) string
}

// hotProfileTallest is the most levels a tall section is drawn at.
const hotProfileTallest = 3

// natural is the box width that fits the section without cutting anything:
// "│ ", the marker, the content, "  │", and room for the border's title and
// label.
func (s hotProfileSection) natural() int {
	return s.boxWidth(s.width)
}

// narrowest is the box width at the content's least width.
func (s hotProfileSection) narrowest() int {
	if s.least == 0 {
		return s.natural()
	}
	return s.boxWidth(s.least)
}

func (s hotProfileSection) boxWidth(content int) int {
	border := 8 + ansi.StringWidth(s.title)
	if s.label != "" {
		border += 1 + ansi.StringWidth(s.label)
	}
	return max(border, 5+2+content)
}

// height counts the box's borders around its rows.
func (s hotProfileSection) height() int {
	if len(s.items) == 0 && len(s.notes) == 0 {
		return 0
	}
	return 2 + len(s.items) + len(s.notes)
}

// fit trims the section's items to rows lines. A section that cannot keep its
// borders, notes and one item does not fit at all.
func (s hotProfileSection) fit(rows int) (hotProfileSection, bool) {
	if s.height() == 0 {
		return s, false
	}
	if s.tall != nil {
		for level := hotProfileTallest; level >= 1; level-- {
			if s.notes = s.tall(level); s.height() <= rows {
				return s, true
			}
		}
		return s, false
	}
	if s.height() <= rows {
		return s, true
	}
	keep := rows - 2 - len(s.notes)
	if len(s.items) == 0 || keep < 1 {
		return s, false
	}
	s.items = s.items[:keep]
	return s, true
}

// hotProfileBlock is one band of the screen: a box across the width, or two
// side by side on a wide terminal.
type hotProfileBlock struct {
	left, right *hotProfileSection
	// full spans the screen; otherwise a lone box keeps a column's width.
	full bool
}

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

// hotProfileColumns reports whether languages and areas fit side by side at
// their narrowest, the test for laying the screen out in two columns.
func (m model) hotProfileColumns(sections []*hotProfileSection) bool {
	return sections[0].narrowest()+1+sections[1].narrowest() <= m.width
}

// hotProfileLayout picks the sections that fit under the header, in priority
// order: languages, areas, files, collaborators, then the week. The first
// band that does not fit whole is trimmed, and everything after it is
// dropped.
func (m model) hotProfileLayout(author *hotAuthor, now time.Time) []hotProfileBlock {
	sections := m.hotProfileSections(author, now)
	var bands [][]*hotProfileSection
	columns := m.hotProfileColumns(sections)
	if columns {
		// Collaborators keep a column's width beside nothing, so the week can
		// take the whole screen below them.
		bands = [][]*hotProfileSection{{sections[0], sections[1]}, {sections[2]}, {sections[3], {}}, {sections[4]}}
	} else {
		for _, section := range sections {
			bands = append(bands, []*hotProfileSection{section})
		}
	}
	rows := max(0, m.height-2-len(m.hotProfileHeaderLines(author, now)))
	var blocks []hotProfileBlock
	for _, band := range bands {
		var fitted []*hotProfileSection
		trimmed := false
		for _, section := range band {
			fit, ok := section.fit(rows)
			if !ok {
				trimmed = trimmed || section.height() > 0
				continue
			}
			trimmed = trimmed || fit.height() < section.height()
			fitted = append(fitted, &fit)
		}
		// A pair that lost one box keeps the other on the left.
		if len(fitted) > 0 {
			block := hotProfileBlock{left: fitted[0], full: len(band) == 1 || !columns}
			if len(fitted) > 1 {
				block.right = fitted[1]
			}
			blocks = append(blocks, block)
			rows -= block.height()
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
	// The repo column says how much of everyone's work in a language is this
	// person's; alone in a repository it is always 100%, so it is left out.
	repoShare := func(language hotLanguageShare) float64 {
		if repo := data.repo[language.name]; repo != nil && repo.attention > 0 {
			return language.stat.attention / repo.attention
		}
		return 1
	}
	for _, language := range headline {
		if repoShare(language) < 0.999 {
			languages.label = "repo"
		}
	}
	// The share bar gives up cells, down to a few, before the names are cut.
	const barWidest, barNarrowest = 16, 6
	nameWidth := 0
	for _, language := range headline {
		nameWidth = max(nameWidth, ansi.StringWidth(language.name))
	}
	rest := 2 + 4 + 2 + hotBuckets
	if languages.label != "" {
		rest += 2 + 4
	}
	for _, language := range headline {
		languages.items = append(languages.items, hotProfileItem{
			row: hotProfileRow{kind: "language", key: language.name},
			text: func(width int) string {
				barWidth := max(barNarrowest, min(barWidest, width-nameWidth-2-rest))
				filled := int(math.Round(language.share * float64(barWidth)))
				if language.share > 0 {
					filled = max(1, filled)
				}
				columns := styleNormal + color + strings.Repeat("█", filled) + styleReset + strings.Repeat("░", barWidth-filled) +
					fmt.Sprintf("  %4s  ", hotPercent(language.share)) + hotAreaStrip(language.stat.timeline, peak, color)
				if languages.label != "" {
					columns += fmt.Sprintf("  %4s", hotPercent(repoShare(language)))
				}
				return hotProfileCells(func(room int) string { return padCell(language.name, room) }, columns, width)
			},
		})
	}
	if len(headline) > 0 {
		languages.width = nameWidth + 2 + barWidest + rest
		languages.least = languages.width - (barWidest - barNarrowest)
	}
	var buckets []string
	for _, language := range other {
		if percent := int(math.Round(language.share * 100)); percent >= 1 {
			buckets = append(buckets, fmt.Sprintf("%s %d%%", language.name, percent))
		}
	}
	if len(buckets) > 0 {
		note := strings.Join(buckets, " · ")
		languages.notes = append(languages.notes, func(int) string { return note })
		languages.width = max(languages.width, ansi.StringWidth(note))
		languages.least = max(languages.least, ansi.StringWidth(note))
	}

	areas := &hotProfileSection{title: "areas"}
	dirs := hotAuthorAreas(author)
	dirs = dirs[:min(hotProfileAreas, len(dirs))]
	spanWidth, areaWidth := 0, 0
	for _, dir := range dirs {
		spanWidth = max(spanWidth, ansi.StringWidth(hotSpan(author.areas[dir].first, author.areas[dir].last, now)))
		areaWidth = max(areaWidth, ansi.StringWidth(syntax.Clean(hotProfileArea(dir))))
	}
	// Without room for every column, the span of dates goes first.
	areas.least = areaWidth + 2 + 4 + 2 + hotBuckets
	areas.width = areas.least + 2 + spanWidth
	for _, dir := range dirs {
		area := author.areas[dir]
		areas.items = append(areas.items, hotProfileItem{
			row: hotProfileRow{kind: "area", key: dir},
			text: func(width int) string {
				columns := fmt.Sprintf("%4s  ", hotPercent(float64(area.commits)/float64(max(1, author.commits)))) +
					hotAreaStrip(area.timeline, peak, color)
				if width >= areas.width {
					columns += "  " + padCell(hotSpan(area.first, area.last, now), spanWidth)
				}
				return hotProfileCells(func(room int) string { return padCell(syntax.Clean(hotProfileArea(dir)), room) }, columns, width)
			},
		})
	}

	files := &hotProfileSection{title: "files", label: "commits  share"}
	filePeak := 0.0
	for _, file := range data.files {
		filePeak = max(filePeak, file.file.timeline.peak())
	}
	for _, file := range data.files {
		files.items = append(files.items, hotProfileItem{
			row: hotProfileRow{kind: "file", key: file.file.path},
			text: func(width int) string {
				columns := hotStrip(file.file.timeline, filePeak, "") + fmt.Sprintf("  %7s  %5s", formatCount(file.file.commits), hotPercent(file.share))
				return hotProfileCells(func(room int) string { return hotPathCell(syntax.Clean(file.file.path), room) }, columns, width)
			},
		})
		files.width = max(files.width, ansi.StringWidth(syntax.Clean(file.file.path))+2+hotBuckets+2+7+2+5)
	}

	// Overlap is drawn against the closest collaborator, since lines shared
	// with any one person are a small slice of a busy author's work.
	alongside := &hotProfileSection{title: "alongside", label: "overlap     files"}
	const overlapWidth = 10
	closest := 1
	for _, other := range data.alongside {
		closest = max(closest, other.overlap)
	}
	for _, other := range data.alongside {
		alongside.items = append(alongside.items, hotProfileItem{
			row: hotProfileRow{kind: "person", key: other.name},
			text: func(width int) string {
				otherColor := m.hotProfileColor(other.name)
				badge := styleNormal + otherColor + authorInitials(other.name) + styleReset
				filled := max(1, int(math.Round(float64(other.overlap)/float64(closest)*overlapWidth)))
				columns := styleNormal + otherColor + strings.Repeat("█", filled) + styleReset + strings.Repeat("░", overlapWidth-filled) +
					fmt.Sprintf("  %5s", formatCount(other.files))
				return hotProfileCells(func(room int) string {
					return badge + " " + padCell(syntax.Clean(other.name), max(0, room-hotBadgeWidth))
				}, columns, width)
			},
		})
		alongside.width = max(alongside.width, hotBadgeWidth+ansi.StringWidth(syntax.Clean(other.name))+2+overlapWidth+2+5)
	}

	week := &hotProfileSection{title: "week", stretch: true}
	if punchPeak := author.punch.peak(); punchPeak > 0 {
		var swatch strings.Builder
		for step := range 5 {
			swatch.WriteString(styleNormal + m.hotHeat(float64(step)/4) + "█" + styleReset + styleDim)
		}
		week.label = "less " + swatch.String() + " more"
		columns := author.punch.columns()
		week.width = 4 + 2*hotPunchColumns
		week.tall = func(dayRows int) []func(width int) string {
			notes := []func(width int) string{func(width int) string {
				grid := max(2*hotPunchColumns, width-4)
				// Every column is labelled with the hour it starts at once the
				// labels fit, every other one before that.
				step := 1
				if grid/hotPunchColumns < 3 {
					step = 2
				}
				hours := []rune(strings.Repeat(" ", grid))
				for column := 0; column < hotPunchColumns; column += step {
					copy(hours[hotPunchStart(column, grid):], []rune(fmt.Sprint(column*hotPunchHours)))
				}
				return "    " + string(hours)
			}}
			for day, name := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
				for row := range dayRows {
					notes = append(notes, func(width int) string {
						label := "   "
						if row == 0 {
							label = name
						}
						return label + " " + hotPunchRow(columns[day], punchPeak, m.hotHeat, max(2*hotPunchColumns, width-4), row, dayRows)
					})
				}
			}
			return notes
		}
		week.notes = week.tall(hotProfileTallest)
	}
	return []*hotProfileSection{languages, areas, files, alongside, week}
}

// hotPunchStart is the cell a column begins at when the day spans grid
// cells; columns differ by at most one cell so the day fills its row.
func hotPunchStart(column, grid int) int { return column * grid / hotPunchColumns }

// hotPunchRow draws one line of a day that spans dayRows lines, row 0 at the
// top, across grid cells: a heat map, each column a solid cell colored by how
// busy it is against the busiest column, on a square-root scale so quiet hours
// still separate from empty ones. A day taller than one line ends in a half
// block, leaving a sliver between days. Every column's cell is the same width;
// wide columns keep a gap after it, which absorbs the cells left over. An
// empty column is one dot on the day's first line.
func hotPunchRow(day [hotPunchColumns]float64, peak float64, heat func(float64) string, grid, row, dayRows int) string {
	block := grid / hotPunchColumns
	if block >= 3 {
		block--
	}
	glyph := "█"
	if dayRows > 1 && row == dayRows-1 {
		glyph = "▀"
	}
	var b strings.Builder
	for column, count := range day {
		cell := hotPunchStart(column+1, grid) - hotPunchStart(column, grid)
		if count == 0 {
			text := ""
			if row == 0 {
				text = strings.Repeat(" ", (block-1)/2) + "·"
			}
			b.WriteString(padCell(text, cell))
			continue
		}
		b.WriteString(styleNormal + heat(math.Sqrt(count/peak)) + strings.Repeat(glyph, block) + styleReset + strings.Repeat(" ", cell-block))
	}
	return b.String()
}

// hotHeatRamps run from barely there to hottest. The dark ramp rises from a
// dim plum through red to yellow; the light one deepens from pale gold to
// plum, so the hottest cell is always the one that stands out from the
// background.
var hotHeatRamps = struct{ dark, light [][3]float64 }{
	dark:  [][3]float64{{72, 48, 92}, {135, 45, 112}, {205, 62, 72}, {242, 135, 45}, {252, 225, 100}},
	light: [][3]float64{{250, 232, 168}, {246, 178, 92}, {224, 104, 58}, {176, 46, 72}, {92, 24, 84}},
}

// hotHeat colors heat t in [0, 1] along the theme's ramp.
func (m model) hotHeat(t float64) string {
	ramp := hotHeatRamps.dark
	if m.lightTheme {
		ramp = hotHeatRamps.light
	}
	t = max(0, min(1, t)) * float64(len(ramp)-1)
	i := min(int(t), len(ramp)-2)
	f := t - float64(i)
	var rgb [3]int
	for c := range rgb {
		rgb[c] = int(math.Round(ramp[i][c] + (ramp[i+1][c]-ramp[i][c])*f))
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
}

func hotPercent(share float64) string {
	return fmt.Sprintf("%d%%", int(math.Round(share*100)))
}

// hotProfileCells lays a row out as a table: the name fills what the columns
// leave, and the columns sit flush right under the box's label.
func hotProfileCells(name func(room int) string, columns string, width int) string {
	room := width - 2 - ansi.StringWidth(columns)
	if room < 4 {
		return ansi.Truncate(columns, width, "")
	}
	return name(room) + "  " + columns
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

// hotProfileWidths sizes the boxes as a grid fitted to their content: one
// width per column, and boxes across the screen as wide as both columns or
// their own content, whichever is wider. Nothing exceeds the screen; when the
// columns do not fit side by side, each gives up room down to half.
func (m model) hotProfileWidths(blocks []hotProfileBlock) (left, right, full int) {
	paired := false
	leftLeast, rightLeast := 0, 0
	for _, block := range blocks {
		if block.full {
			full = max(full, block.left.natural())
			continue
		}
		paired = true
		left, leftLeast = max(left, block.left.natural()), max(leftLeast, block.left.narrowest())
		if block.right != nil {
			right, rightLeast = max(right, block.right.natural()), max(rightLeast, block.right.narrowest())
		}
	}
	// Too wide side by side, each column gives up cells in proportion to how
	// many it can spare.
	if over := left + 1 + right - m.width; paired && over > 0 {
		leftSpare, rightSpare := left-leftLeast, right-rightLeast
		if spare := leftSpare + rightSpare; spare > 0 {
			give := min(over, spare) * leftSpare / spare
			left -= give
			right -= min(over, spare) - give
		}
		left = min(left, m.width-1-right)
	}
	if paired && right > 0 {
		full = max(full, left+1+right)
	} else if paired {
		full = max(full, left)
	}
	for _, block := range blocks {
		if block.left.stretch {
			full = m.width
		}
	}
	// Columns widen with the boxes across the screen so the grid's edges
	// line up.
	if paired && right > 0 && full > left+1+right {
		extra := full - (left + 1 + right)
		left, right = left+extra/2, right+extra-extra/2
	}
	return min(left, m.width), min(right, m.width), min(full, m.width)
}

// hotProfileBox draws a section in a rounded box of width cells and height
// lines: the title, and any column label, in the top border, rows marked with
// the selection. The title takes the accent while the selection is inside.
func (m model) hotProfileBox(section *hotProfileSection, width, height int, selected hotProfileRow) []string {
	// "│ " before the rows and "  │" after, so they end under the label,
	// which the border closes with " ─╮".
	inner := max(1, width-5)
	active := false
	var body []string
	for _, item := range section.items {
		text := ansi.Truncate(item.text(max(1, inner-2)), max(1, inner-2), "")
		if item.row == selected {
			active = true
			body = append(body, m.accent()+"▸ "+styleReset+text+styleReset)
			continue
		}
		body = append(body, styleDim+"  "+strings.ReplaceAll(text, styleReset, styleReset+styleDim)+styleReset)
	}
	for _, note := range section.notes {
		text := ansi.Truncate(note(max(1, inner-2)), max(1, inner-2), "")
		body = append(body, styleDim+"  "+strings.ReplaceAll(text, styleReset, styleReset+styleDim)+styleReset)
	}
	for len(body) < height-2 {
		body = append(body, "")
	}
	// The top border is "╭─ title " then a rule, closed by the label as
	// " label ─╮" or by "─╮" alone.
	title, tail := syntax.Clean(section.title), "─╮"
	if section.label != "" {
		tail = " " + section.label + " ─╮"
	}
	fill := width - 4 - ansi.StringWidth(title) - ansi.StringWidth(tail)
	if fill < 1 {
		tail = "─╮"
		fill = width - 4 - ansi.StringWidth(title) - 2
	}
	if fill < 0 {
		title = truncateCell(title, max(1, width-6))
		fill = max(0, width-6-ansi.StringWidth(title))
	}
	if active {
		title = m.accent() + title + styleReset
	}
	lines := []string{styleDim + "╭─ " + styleReset + title + styleDim + " " + strings.Repeat("─", fill) + tail + styleReset}
	for _, line := range body {
		lines = append(lines, styleDim+"│ "+styleReset+padCell(line, inner)+styleDim+"  │"+styleReset)
	}
	return append(lines, styleDim+"╰"+strings.Repeat("─", max(0, width-2))+"╯"+styleReset)
}

// hotProfileHeaderLines is the name, totals and activity.
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
	first := " " + styleNormal + color + badge + styleReset + " " + syntax.Clean(author.name) + styleDim + "  " + summary + "  " +
		hotAreaStrip(author.timeline, author.timeline.peak(), color) + styleReset
	return []string{ansi.Truncate(first, m.width, "") + styleReset}
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
	title := " hotspots · " + m.hotWindowLabel()
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
		// Paired boxes share a height so their borders line up.
		leftWidth, rightWidth, fullWidth := m.hotProfileWidths(blocks)
		for _, block := range blocks {
			height := block.height()
			if block.full {
				lines = append(lines, m.hotProfileBox(block.left, fullWidth, height, selected)...)
				continue
			}
			left := m.hotProfileBox(block.left, leftWidth, height, selected)
			var right []string
			if block.right != nil {
				right = m.hotProfileBox(block.right, rightWidth, height, selected)
			}
			for i, line := range left {
				if i < len(right) {
					line += " " + right[i]
				}
				lines = append(lines, line)
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

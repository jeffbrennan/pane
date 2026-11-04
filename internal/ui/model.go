package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

type loaded struct {
	snapshot   git.Snapshot
	err        error
	generation int
	cache      *git.RefreshCache
}
type loadedUnchanged struct {
	generation int
	cache      *git.RefreshCache
}
type tick struct{}
type resizeRebuild struct{ request int }
type completed struct {
	message       string
	commitMessage string
	action        string
	err           error
	stageCursor   *stagingCursor
}

type recentCommitsLoaded struct {
	commits []git.CommitInfo
	err     error
	request int
}

type model struct {
	repo                  git.Repo
	refreshCache          *git.RefreshCache
	snapshot              git.Snapshot
	rows                  []row
	width, height         int
	gutterFloor           int
	gutterWidth           int
	sequenceGutters       bool
	previousChanges       []git.Change
	syntaxWarmPath        string
	syntaxWarmCommit      string
	tickStopped           bool
	resizeContentWidth    int
	resizeRequest         int
	pos, horizontal       int
	viewTop               int
	pending               string
	count                 int
	jumps                 []jumpLocation
	jumpIndex             int
	staged, wrap          bool
	shortPaths            bool
	relativeLines         bool
	startsInHistory       bool
	lightTheme            bool
	themeAuto             bool
	themeNotify           bool
	themeQuery            func()
	ticks                 int
	blurred               bool
	blurredInterval       int
	whitespaceRefreshing  bool
	loading, busy         bool
	activeAction          string
	startup               bool
	generation            int
	modal, input, message string
	inputCursor           int
	inputBody             string
	amendHash             string
	commitEditorPath      string
	errorText             string
	errorPos              int
	help                  listState
	helpContext           string
	screen, viewTitle     string
	viewBadge, viewHints  string
	history               historyState
	worktreeHeads         map[string]string
	worktreeHeadRequest   int
	picker                pickerState
	pickerReturn          bool
	pickerBackToHistory   bool
	search                searchState
	searchProgress        string
	flash                 string
	expandedDeleted       map[string]bool
	contextFiles          map[git.Context]contextFile
	collapsedFiles        map[git.Context]bool
	generatedHandled      map[git.Context]bool
	viewed                *viewedStore
	viewIdentity          string
	contextRequest        int
	contextCommit         string
	syntaxDemand          *syntaxDemand
	recentCommits         []git.CommitInfo
	recentCommitRequest   int
	recentCommitLoading   bool
	recentCommitError     bool
	discard               git.DiscardRequest
	stageCursor           *stagingCursor
	sequence              sequenceState
	stash                 stashState
	branch                branchState
	push                  pushState
	pull                  git.PullIssue
	pushIssue             git.PushIssue
	worktrees             worktreeState
	hotspots              hotspotState
	base                  baseState
	contextBase           bool
	fileHistoryReturn     *model
	prefetch              *startupPrefetch
}

func (m model) closeHistoryStreams() {
	m.cancelCommitLoad()
	m.prefetch.close()
	m.history.stream.Close()
	if m.fileHistoryReturn != nil {
		m.fileHistoryReturn.history.stream.Close()
	}
}

func initialModel(repo git.Repo, args []string) (model, error) {
	if len(args) == 2 && args[0] == "--" {
		return initialFileHistory(repo, args[1])
	}
	if len(args) == 3 && args[0] == "log" && args[1] == "--" {
		return initialFileHistory(repo, args[2])
	}
	if len(args) > 0 && args[0] == "hot" {
		since, author := hotSinceDefault(), ""
		for i := 1; i < len(args); i++ {
			arg := args[i]
			switch {
			case arg == "--since" && i+1 < len(args):
				i++
				since = args[i]
			case strings.HasPrefix(arg, "--since="):
				since = strings.TrimPrefix(arg, "--since=")
			default:
				return model{}, fmt.Errorf("usage: pane hot [--since <window>]")
			}
		}
		m := model{repo: repo, wrap: true, loading: true, startup: true, viewed: newViewedStore(repo.Root)}
		next, _ := m.openHotspots(since, author)
		return next.(model), nil
	}
	if len(args) > 2 || (len(args) == 2 && args[0] != "--base" && args[0] != "-b") {
		return model{}, fmt.Errorf("%s", fileHistoryUsage())
	}
	m := model{repo: repo, wrap: true, loading: true, startup: true, viewed: newViewedStore(repo.Root)}
	if len(args) == 0 {
		return m, nil
	}
	base := ""
	if len(args) == 2 {
		base = args[1]
		if base == "" || strings.HasPrefix(base, "-") {
			return model{}, fmt.Errorf("invalid base %q", base)
		}
	} else if args[0] == "--base" || args[0] == "-b" {
		upstream, err := repo.DefaultBase()
		if err != nil {
			return model{}, err
		}
		base = upstream
	} else if strings.HasSuffix(args[0], "...") && !strings.Contains(strings.TrimSuffix(args[0], "..."), "..") {
		base = strings.TrimSuffix(args[0], "...")
		if base == "" {
			return model{}, fmt.Errorf("base revision cannot be empty")
		}
	}
	if base != "" {
		hash, err := repo.MergeBase(base)
		if err != nil {
			return model{}, err
		}
		m.screen, m.base = "base", baseState{revision: base, hash: hash}
		m.history.directCommit = true
		return m, nil
	}
	if strings.Contains(args[0], "..") {
		out, err := repo.Command("rev-list", fmt.Sprintf("--max-count=%d", maxSequenceCommits+1), "--reverse", "--end-of-options", args[0], "--")
		if err != nil {
			return model{}, fmt.Errorf("invalid commit range %q: %w", args[0], git.CommandError(out, err))
		}
		if len(out) == 0 {
			return model{}, fmt.Errorf("commit range %q contains no commits", args[0])
		}
		return initialSequence(repo, strings.NewReader(string(out)))
	}
	commit, err := repo.ResolveCommit(args[0])
	if err != nil {
		return model{}, err
	}
	m.screen = "commit"
	m.history.current = commit
	m.history.meta = commit
	m.history.loading = true
	m.history.request++
	m.history.directCommit = true
	m.history.streamActive = true
	m.startCommitLoad()
	return m, nil
}

func (m model) Init() tea.Cmd {
	initial := load(m.repo, m.generation, m.refreshCache)
	if m.prefetch != nil {
		initial = m.prefetch.loadStatus()
	}
	commands := []tea.Cmd{initial, nextTick(), m.queryTheme()}
	if m.screen == "history" && m.history.path != "" && m.history.loading {
		commands = append(commands, m.fetchHistory(0, git.HistoryPage))
	}
	if m.screen == "commit" && m.history.loading && m.history.directCommit {
		repo, hash, request := m.commitRepository(), m.history.current.Hash, m.history.request
		commands = append(commands, startCommitStream(repo, hash, request, m.height))
		commands = append(commands, commitLoadingTimer(request))
	}
	if m.screen == "sequence" && m.sequence.loading {
		for i, state := range m.sequence.states {
			if state == sequenceLoading {
				commands = append(commands, m.startSequenceStream(i))
			}
		}
	}
	if m.screen == "hotspots" && m.hotspots.loading {
		commands = append(commands, m.loadHotspots())
	}
	return tea.Batch(commands...)
}

// queryTheme asks the terminal for its background color; the answer arrives
// as terminalBackgroundChanged.
func (m model) queryTheme() tea.Cmd {
	if !m.themeAuto || m.themeNotify || m.themeQuery == nil || editorRunning.Load() {
		return nil
	}
	query := m.themeQuery
	return func() tea.Msg {
		if !editorRunning.Load() {
			query()
		}
		return nil
	}
}

func (m model) readOnlyDiff() bool {
	return m.screen == "commit" || m.screen == "sequence" || m.screen == "stashes" || m.screen == "branches" || m.screen == "worktrees" || m.screen == "hotspots" || (m.screen == "history" && m.history.path != "")
}

func (m model) queryThemeOnTick() tea.Cmd {
	if m.blurred || (!m.readOnlyDiff() && m.ticks%10 != 0) {
		return nil
	}
	return m.queryTheme()
}

func load(repo git.Repo, generation int, cache *git.RefreshCache) tea.Cmd {
	return loadWith(repo, generation, cache, false)
}

func loadWith(repo git.Repo, generation int, cache *git.RefreshCache, deferOffscreen bool) tea.Cmd {
	return func() tea.Msg {
		next, unchanged, err := repo.LoadChanges(cache, deferOffscreen)
		if err != nil {
			return loaded{err: err, generation: generation}
		}
		if unchanged {
			return loadedUnchanged{generation: generation, cache: next}
		}
		return loaded{snapshot: next.Snapshot, generation: generation, cache: next}
	}
}

// Refreshes run every refreshTicks ticks while the terminal is focused, and
// every blurredRefreshTicks while it is not, so a pane left in a background
// window polls Git less often. Regaining focus refreshes immediately.
const (
	refreshTicks        = 2
	blurredRefreshTicks = 6
)

func nextTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tick{} })
}

func (m model) refresh() (model, tea.Cmd) {
	if m.loading || m.busy {
		return m, nil
	}
	m.loading = true
	return m, load(m.repo, m.generation, m.refreshCache)
}

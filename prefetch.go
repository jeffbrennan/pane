package main

import tea "github.com/charmbracelet/bubbletea"

// startupStatsCount is how many of the newest commits get line counts before
// history first appears: enough to fill a tall terminal.
const startupStatsCount = 25

// startupPrefetch loads the working tree status and the first history page
// as soon as pane starts, in parallel with each other and with terminal setup.
// A clean tree opens history, so loading it only after status reports clean
// would serialize every Git command behind the status check. The history's
// stash count and line counts wait for status to report clean, so a dirty
// tree's first screen does not compete with them; if the tree is dirty, the
// history is discarded.
type startupPrefetch struct {
	status  chan tea.Msg
	clean   chan bool
	history chan historyLoaded
	stream  *historyStream
}

func startPrefetch(repo repository) *startupPrefetch {
	p := &startupPrefetch{status: make(chan tea.Msg, 1), clean: make(chan bool, 1), history: make(chan historyLoaded, 1), stream: newHistoryStream(repo, "")}
	go func() {
		msg := loadWith(repo, 0, nil, true)()
		loadedMsg, ok := msg.(loaded)
		p.clean <- ok && loadedMsg.err == nil && len(loadedMsg.snapshot.unstaged) == 0 && len(loadedMsg.snapshot.staged) == 0
		p.status <- msg
	}()
	go func() {
		commits, err := p.stream.nextPage(historyPage)
		repo.stats.fill(commits)
		if !<-p.clean || err != nil {
			p.history <- historyLoaded{commits: commits, err: err, limit: historyPage}
			return
		}
		count := make(chan int, 1)
		go func() {
			entries, _ := repo.stashes()
			count <- len(entries)
		}()
		var missing []string
		for _, c := range commits[:min(len(commits), startupStatsCount)] {
			if !c.statsLoaded {
				missing = append(missing, c.hash)
			}
		}
		if len(missing) > 0 {
			// Counts are a nicety; history still opens if they fail.
			if stats, statsErr := repo.historyStats(missing); statsErr == nil {
				applyStats(commits, stats)
			}
		}
		p.history <- historyLoaded{commits: commits, err: err, limit: historyPage, stashCount: <-count}
	}()
	return p
}

func applyStats(commits, stats []commitInfo) {
	byHash := make(map[string]commitInfo, len(stats))
	for _, s := range stats {
		byHash[s.hash] = s
	}
	for i, c := range commits {
		if s, ok := byHash[c.hash]; ok {
			commits[i].added, commits[i].deleted, commits[i].binary = s.added, s.deleted, s.binary
			commits[i].statsLoaded = true
		}
	}
}

// loadStatus waits for the prefetched status in place of an initial load.
func (p *startupPrefetch) loadStatus() tea.Cmd {
	return func() tea.Msg { return <-p.status }
}

// loadHistory waits for the prefetched history page, tagged with request.
func (p *startupPrefetch) loadHistory(request int) tea.Cmd {
	return func() tea.Msg {
		msg := <-p.history
		msg.request = request
		return msg
	}
}

func (p *startupPrefetch) close() {
	if p != nil {
		p.stream.close()
	}
}

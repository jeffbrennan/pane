package git

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// statsStoreLimit bounds the on-disk cache; past it the file starts over.
const statsStoreLimit = 50000

type commitStats struct{ added, deleted, binary int }

// StatsStore keeps history line counts across launches, so commits seen
// before show their counts with their messages. A commit's counts never
// change, so entries are keyed by hash alone and never expire. A nil store
// caches nothing.
type StatsStore struct {
	Path  string
	once  sync.Once
	mu    sync.Mutex
	stats map[string]commitStats
}

func newStatsStore(root string) *StatsStore {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	sum := sha256.Sum256([]byte(root))
	return &StatsStore{Path: filepath.Join(dir, "pane", "stats-"+hex.EncodeToString(sum[:8]))}
}

func (s *StatsStore) load() {
	s.once.Do(func() {
		s.stats = make(map[string]commitStats)
		file, err := os.Open(s.Path)
		if err != nil {
			return
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var hash string
			var c commitStats
			if n, _ := fmt.Sscanf(scanner.Text(), "%s %d %d %d", &hash, &c.added, &c.deleted, &c.binary); n == 4 {
				s.stats[hash] = c
			}
		}
		if len(s.stats) > statsStoreLimit {
			s.stats = make(map[string]commitStats)
			os.Remove(s.Path)
		}
	})
}

// fill copies stored counts into commits that do not have them yet.
func (s *StatsStore) Fill(commits []CommitInfo) {
	if s == nil {
		return
	}
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range commits {
		if stats, ok := s.stats[c.Hash]; ok && !c.StatsLoaded {
			commits[i].Added, commits[i].Deleted, commits[i].Binary = stats.added, stats.deleted, stats.binary
			commits[i].StatsLoaded = true
		}
	}
}

// put records counts for commits and appends new ones to the cache file.
// Write failures only cost a recomputation on the next launch.
func (s *StatsStore) put(commits []CommitInfo) {
	if s == nil {
		return
	}
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines []byte
	for _, c := range commits {
		if _, ok := s.stats[c.Hash]; ok || c.Hash == "" {
			continue
		}
		s.stats[c.Hash] = commitStats{c.Added, c.Deleted, c.Binary}
		lines = fmt.Appendf(lines, "%s %d %d %d\n", c.Hash, c.Added, c.Deleted, c.Binary)
	}
	if len(lines) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	file.Write(lines)
}

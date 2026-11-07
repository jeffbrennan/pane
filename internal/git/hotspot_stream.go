package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// hotSpotReader reads one git log --numstat process.
type hotSpotReader struct {
	repo Repo
	args []string

	mu     sync.Mutex
	cmd    *exec.Cmd
	reader *bufio.Reader
	stderr bytes.Buffer
	done   bool
	closed bool
	header bool
}

func newHotSpotReader(repo Repo, args []string) *hotSpotReader {
	return &hotSpotReader{repo: repo, args: args}
}

func (s *hotSpotReader) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	if s.cmd != nil || s.done {
		return nil
	}
	cmd := s.repo.gitCommand(s.args...)
	cmd.Stderr = &s.stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd, s.reader = cmd, bufio.NewReader(stdout)
	return nil
}

func (s *hotSpotReader) readField() (string, error) {
	field, err := s.reader.ReadString(0)
	if err == io.EOF {
		return strings.TrimSuffix(field, "\x00"), io.EOF
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(field, "\x00"), nil
}

func (s *hotSpotReader) finish(kill bool) error {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return nil
	}
	s.done = true
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if kill {
		_ = cmd.Process.Kill()
	}
	err := cmd.Wait()
	if err != nil && !kill {
		return CommandError(s.stderr.Bytes(), err)
	}
	return nil
}

func (s *hotSpotReader) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.finish(true)
}

func (s *hotSpotReader) nextPage(limit int) ([]HotCacheCommit, bool, error) {
	if s == nil {
		return nil, true, nil
	}
	if err := s.start(); err != nil {
		return nil, true, err
	}
	page := make([]HotCacheCommit, 0, min(limit, 128))
	for len(page) < limit {
		if !s.header {
			field, err := s.readField()
			if err == io.EOF {
				if err := s.finish(false); err != nil {
					return page, true, err
				}
				return page, true, nil
			}
			if err != nil {
				return page, true, err
			}
			if field != "" {
				continue
			}
			s.header = true
		}
		hash, err := s.readField()
		if err == io.EOF {
			_ = s.finish(false)
			return page, true, nil
		}
		if err != nil {
			return page, true, err
		}
		timestamp, err := s.readField()
		if err != nil {
			return page, true, err
		}
		author, err := s.readField()
		if err != nil {
			return page, true, err
		}
		email, err := s.readField()
		if err != nil {
			return page, true, err
		}
		s.header = false
		var fields []string
		eof := false
		for {
			field, err := s.readField()
			if err == io.EOF {
				eof = true
				break
			}
			if err != nil {
				return page, true, err
			}
			if field == "" {
				s.header = true
				break
			}
			fields = append(fields, field)
		}
		commit, err := parseHotCommitFields(hash, timestamp, author, fields)
		if err != nil {
			return page, true, err
		}
		commit.Email = email
		page = append(page, commit)
		if eof {
			if err := s.finish(false); err != nil {
				return page, true, err
			}
			return page, true, nil
		}
	}
	return page, false, nil
}

type hotSpotPage struct {
	commits []HotCacheCommit
	done    bool
	err     error
}

// HotSpotStream merges one or more git log readers so a large window can be
// walked in parallel. It still yields commits in pages.
type HotSpotStream struct {
	repo          Repo
	since, author string
	revisionRange string

	mu         sync.Mutex
	readers    []*hotSpotReader
	results    chan hotSpotPage
	closed     chan struct{}
	started    bool
	closedFlag bool
	remaining  int
	err        error
}

func NewHotSpotStream(repo Repo, since, author, revisionRange string) *HotSpotStream {
	return &HotSpotStream{repo: repo, since: since, author: author, revisionRange: revisionRange, closed: make(chan struct{})}
}

func (s *HotSpotStream) start() error {
	s.mu.Lock()
	if s.closedFlag {
		s.mu.Unlock()
		return io.ErrClosedPipe
	}
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()
	readers, err := s.buildReaders()
	if err != nil {
		return err
	}
	s.readers = readers
	s.results = make(chan hotSpotPage, max(2, len(readers)*2))
	s.remaining = len(readers)
	startReader := func(reader *hotSpotReader) {
		go func() {
			batch := 1
			for {
				commits, done, err := reader.nextPage(batch)
				batch = 20
				select {
				case s.results <- hotSpotPage{commits: commits, done: done, err: err}:
				case <-s.closed:
					return
				}
				if done || err != nil {
					return
				}
			}
		}()
	}
	// Start a few readers first so the first page is not delayed by process
	// startup contention, then fan out to the rest.
	initial := min(6, len(readers))
	for _, reader := range readers[:initial] {
		startReader(reader)
	}
	if initial < len(readers) {
		go func() {
			select {
			case <-time.After(120 * time.Millisecond):
			case <-s.closed:
				return
			}
			for _, reader := range readers[initial:] {
				startReader(reader)
			}
		}()
	}
	return nil
}

var (
	HotspotParallelThreshold = 1000
	HotspotWorkers           = 0
)

func (s *HotSpotStream) buildReaders() ([]*hotSpotReader, error) {
	single := func() []*hotSpotReader {
		args := []string{"log", "--numstat", "-M", "-z", "--format=%x00%H%x00%ct%x00%aN%x00%aE"}
		if s.since != "" {
			args = append(args, "--since="+s.since)
		}
		if s.author != "" {
			args = append(args, "--author="+s.author)
		}
		if s.revisionRange != "" {
			args = append(args, s.revisionRange)
		}
		args = append(args, "--")
		return []*hotSpotReader{newHotSpotReader(s.repo, args)}
	}
	if s.revisionRange != "" {
		return single(), nil
	}
	var args []string
	if s.since != "" {
		args = append(args, "rev-list", "--since="+s.since)
	} else {
		args = append(args, "rev-list")
	}
	if s.author != "" {
		args = append(args, "--author="+s.author)
	}
	args = append(args, "HEAD")
	out, err := s.repo.CommandStdout(args...)
	if err != nil {
		return single(), nil
	}
	var hashes []string
	for _, field := range bytes.Split(out, []byte{'\n'}) {
		if hash := strings.TrimSpace(string(field)); hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) < HotspotParallelThreshold {
		return single(), nil
	}
	workers := HotspotWorkers
	if workers <= 0 {
		workers = min(24, runtime.GOMAXPROCS(0)*3)
	}
	group := (len(hashes) + workers - 1) / workers
	if group > 2000 {
		group = 2000
	}
	var readers []*hotSpotReader
	for start := 0; start < len(hashes); start += group {
		end := min(start+group, len(hashes))
		logArgs := []string{"log", "--no-walk=unsorted", "--numstat", "-M", "-z", "--format=%x00%H%x00%ct%x00%aN%x00%aE"}
		logArgs = append(logArgs, hashes[start:end]...)
		logArgs = append(logArgs, "--")
		readers = append(readers, newHotSpotReader(s.repo, logArgs))
	}
	return readers, nil
}

func (s *HotSpotStream) NextPage(limit int) ([]HotCacheCommit, bool, error) {
	if s == nil {
		return nil, true, nil
	}
	if err := s.start(); err != nil {
		return nil, true, err
	}
	page := make([]HotCacheCommit, 0, min(limit, 128))
	for len(page) < limit {
		select {
		case result := <-s.results:
			if result.err != nil {
				s.Close()
				return page, true, result.err
			}
			page = append(page, result.commits...)
			if result.done {
				s.remaining--
				if s.remaining == 0 {
					return page, true, nil
				}
			}
		case <-s.closed:
			// A closed stream is cut short, not finished: reporting done
			// would let a partial walk be cached as the whole window.
			return page, true, io.ErrClosedPipe
		}
	}
	return page, false, nil
}

func (s *HotSpotStream) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closedFlag {
		s.mu.Unlock()
		return
	}
	s.closedFlag = true
	close(s.closed)
	readers := s.readers
	s.mu.Unlock()
	for _, reader := range readers {
		reader.close()
	}
}

func parseHotCommitFields(hash, timestamp, author string, fields []string) (HotCacheCommit, error) {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return HotCacheCommit{}, fmt.Errorf("unexpected Git hotspot timestamp")
	}
	commit := HotCacheCommit{Hash: hash, Time: seconds, Author: author}
	byPath := make(map[string]*HotCacheChange)
	for i := 0; i < len(fields); i++ {
		line := strings.TrimPrefix(fields[i], "\n")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			i += 2
		}
		if path == "" {
			continue
		}
		added, addErr := strconv.Atoi(parts[0])
		deleted, delErr := strconv.Atoi(parts[1])
		if addErr != nil || delErr != nil {
			continue
		}
		change := byPath[path]
		if change == nil {
			change = &HotCacheChange{Path: path}
			byPath[path] = change
		}
		change.Added += added
		change.Deleted += deleted
	}
	for _, change := range byPath {
		commit.Changes = append(commit.Changes, *change)
	}
	sort.Slice(commit.Changes, func(i, j int) bool { return commit.Changes[i].Path < commit.Changes[j].Path })
	return commit, nil
}

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// hotSpotStream reads git log --numstat output one commit at a time so the
// hotspot tree can fill in while the window is still being walked.
type hotSpotStream struct {
	repo repository
	args []string

	mu          sync.Mutex
	cmd         *exec.Cmd
	reader      *bufio.Reader
	stderr      bytes.Buffer
	done        bool
	closed      bool
	headerReady bool
}

func newHotSpotStream(repo repository, since, author, revisionRange string) *hotSpotStream {
	args := []string{"log", "--numstat", "-M", "-z", "--format=%x00%H%x00%ct%x00%aN"}
	if since != "" {
		args = append(args, "--since="+since)
	}
	if author != "" {
		args = append(args, "--author="+author)
	}
	if revisionRange != "" {
		args = append(args, revisionRange)
	}
	args = append(args, "--")
	return &hotSpotStream{repo: repo, args: args}
}

func (s *hotSpotStream) start() error {
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

// readField returns one NUL-terminated field without the terminator. io.EOF
// means the process finished.
func (s *hotSpotStream) readField() (string, error) {
	field, err := s.reader.ReadString(0)
	if err == io.EOF {
		return strings.TrimSuffix(field, "\x00"), io.EOF
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(field, "\x00"), nil
}

func (s *hotSpotStream) finish(kill bool) error {
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
		return gitError(s.stderr.Bytes(), err)
	}
	return nil
}

func (s *hotSpotStream) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.finish(true)
}

// nextPage returns up to limit commits and reports whether the stream is done.
func (s *hotSpotStream) nextPage(limit int) ([]hotCacheCommit, bool, error) {
	if s == nil {
		return nil, true, nil
	}
	if err := s.start(); err != nil {
		return nil, true, err
	}
	page := make([]hotCacheCommit, 0, min(limit, 128))
	for len(page) < limit {
		if !s.headerReady {
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
			s.headerReady = true
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
		s.headerReady = false
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
				s.headerReady = true
				break
			}
			fields = append(fields, field)
		}
		commit, err := parseHotCommitFields(hash, timestamp, author, fields)
		if err != nil {
			return page, true, err
		}
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

func parseHotCommitFields(hash, timestamp, author string, fields []string) (hotCacheCommit, error) {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return hotCacheCommit{}, fmt.Errorf("unexpected Git hotspot timestamp")
	}
	commit := hotCacheCommit{Hash: hash, Time: seconds, Author: author}
	byPath := make(map[string]*hotCacheChange)
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
			// The next two fields are the original and new paths.
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
			change = &hotCacheChange{Path: path}
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

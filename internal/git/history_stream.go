package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type HistoryStream struct {
	repo  Repo
	path  string
	since string

	mu      sync.Mutex
	readMu  sync.Mutex
	closed  bool
	cmd     *exec.Cmd
	reader  *bufio.Reader
	stderr  bytes.Buffer
	done    bool
	pending string
}

func NewHistoryStream(repo Repo, path string, since ...string) *HistoryStream {
	stream := &HistoryStream{repo: repo, path: path}
	if len(since) > 0 {
		stream.since = since[0]
	}
	return stream
}

func (s *HistoryStream) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	if s.cmd != nil || s.done {
		return nil
	}
	out, err := s.repo.Command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			s.done = true
			return nil
		}
		return CommandError(out, err)
	}
	args := []string{"log", "-z", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00%P%x00"}
	if s.repo.FirstParent {
		args = append(args, "--first-parent")
	}
	if s.since != "" {
		args = append(args, "--since="+s.since)
	}
	if s.path != "" {
		args = append(args, "--follow", "--name-status", "-M", "--", s.path)
	} else {
		args = append(args, "--")
	}
	cmd := s.repo.gitCommand(args...)
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

func (s *HistoryStream) finish(kill bool) error {
	if s.done {
		return nil
	}
	s.done = true
	if s.cmd == nil {
		return nil
	}
	if kill {
		_ = s.cmd.Process.Kill()
	}
	err := s.cmd.Wait()
	if err != nil && !kill {
		return CommandError(s.stderr.Bytes(), err)
	}
	return nil
}

func (s *HistoryStream) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.mu.Unlock()
	s.readMu.Lock()
	defer s.readMu.Unlock()
	_ = s.finish(true)
}

func (s *HistoryStream) field() (string, error) {
	field, err := s.reader.ReadBytes(0)
	if err != nil {
		if err == io.EOF && len(field) != 0 {
			return "", fmt.Errorf("unexpected Git history format")
		}
		return "", err
	}
	return string(field[:len(field)-1]), nil
}

func (s *HistoryStream) nextCommit() (CommitInfo, error) {
	hash := s.pending
	s.pending = ""
	for hash == "" {
		var err error
		hash, err = s.field()
		if err != nil {
			return CommitInfo{}, err
		}
	}
	fields := make([]string, 6)
	for i := range fields {
		var err error
		fields[i], err = s.field()
		if err != nil {
			return CommitInfo{}, fmt.Errorf("unexpected Git history format: %w", err)
		}
	}
	c := CommitInfo{Hash: hash, Author: fields[0], Email: fields[1], Date: fields[2], Subject: fields[3], Message: fields[4], FilePath: s.path}
	if parents := strings.Fields(fields[5]); len(parents) > 0 {
		c.Parents = parents
	}
	c.Search = strings.ToLower(c.Hash + "\n" + c.Author + "\n" + c.Email + "\n" + c.Message + "\n" + c.Subject)
	if s.path == "" {
		return c, nil
	}
	for {
		token, err := s.field()
		if err == io.EOF {
			return c, nil
		}
		if err != nil {
			return CommitInfo{}, err
		}
		if token == "" {
			continue
		}
		status := strings.TrimPrefix(token, "\n")
		if len(status) > 0 && strings.ContainsRune("AMDRCTUXB", rune(status[0])) && (len(status) == 1 || ((status[0] == 'R' || status[0] == 'C') && len(status) <= 4)) {
			path, err := s.field()
			if err != nil || path == "" {
				return CommitInfo{}, fmt.Errorf("unexpected Git file history path")
			}
			c.FilePath = path
			if status[0] == 'R' || status[0] == 'C' {
				newPath, err := s.field()
				if err != nil || newPath == "" {
					return CommitInfo{}, fmt.Errorf("unexpected Git file history rename")
				}
				c.FilePath = newPath
				s.path = path
			}
			continue
		}
		s.pending = token
		return c, nil
	}
}

func (s *HistoryStream) NextPage(limit int) ([]CommitInfo, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if err := s.start(); err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for len(commits) < limit && !s.done {
		commit, err := s.nextCommit()
		if err == io.EOF {
			return commits, s.finish(false)
		}
		if err != nil {
			_ = s.finish(true)
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

// Reaped reports whether the stream's Git process has exited and been waited
// on.
func (s *HistoryStream) reaped() bool {
	return s.cmd == nil || s.cmd.ProcessState != nil
}

// Running reports whether the stream has started a Git process.
func (s *HistoryStream) running() bool { return s.cmd != nil }

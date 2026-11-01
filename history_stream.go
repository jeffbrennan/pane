package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type historyStream struct {
	repo  repository
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

func newHistoryStream(repo repository, path string, since ...string) *historyStream {
	stream := &historyStream{repo: repo, path: path}
	if len(since) > 0 {
		stream.since = since[0]
	}
	return stream
}

func (s *historyStream) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	if s.cmd != nil || s.done {
		return nil
	}
	out, err := s.repo.command("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			s.done = true
			return nil
		}
		return gitError(out, err)
	}
	args := []string{"log", "-z", "--format=%x00%H%x00%an%x00%ae%x00%aI%x00%s%x00%B%x00"}
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

func (s *historyStream) finish(kill bool) error {
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
		return gitError(s.stderr.Bytes(), err)
	}
	return nil
}

func (s *historyStream) close() {
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

func (s *historyStream) field() (string, error) {
	field, err := s.reader.ReadBytes(0)
	if err != nil {
		if err == io.EOF && len(field) != 0 {
			return "", fmt.Errorf("unexpected Git history format")
		}
		return "", err
	}
	return string(field[:len(field)-1]), nil
}

func (s *historyStream) nextCommit() (commitInfo, error) {
	hash := s.pending
	s.pending = ""
	for hash == "" {
		var err error
		hash, err = s.field()
		if err != nil {
			return commitInfo{}, err
		}
	}
	fields := make([]string, 5)
	for i := range fields {
		var err error
		fields[i], err = s.field()
		if err != nil {
			return commitInfo{}, fmt.Errorf("unexpected Git history format: %w", err)
		}
	}
	c := commitInfo{hash: hash, author: fields[0], email: fields[1], date: fields[2], subject: fields[3], message: fields[4], filePath: s.path}
	c.search = strings.ToLower(c.hash + "\n" + c.author + "\n" + c.email + "\n" + c.message + "\n" + c.subject)
	if s.path == "" {
		return c, nil
	}
	for {
		token, err := s.field()
		if err == io.EOF {
			return c, nil
		}
		if err != nil {
			return commitInfo{}, err
		}
		if token == "" {
			continue
		}
		status := strings.TrimPrefix(token, "\n")
		if len(status) > 0 && strings.ContainsRune("AMDRCTUXB", rune(status[0])) && (len(status) == 1 || ((status[0] == 'R' || status[0] == 'C') && len(status) <= 4)) {
			path, err := s.field()
			if err != nil || path == "" {
				return commitInfo{}, fmt.Errorf("unexpected Git file history path")
			}
			c.filePath = path
			if status[0] == 'R' || status[0] == 'C' {
				newPath, err := s.field()
				if err != nil || newPath == "" {
					return commitInfo{}, fmt.Errorf("unexpected Git file history rename")
				}
				c.filePath = newPath
				s.path = path
			}
			continue
		}
		s.pending = token
		return c, nil
	}
}

func (s *historyStream) nextPage(limit int) ([]commitInfo, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if err := s.start(); err != nil {
		return nil, err
	}
	var commits []commitInfo
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

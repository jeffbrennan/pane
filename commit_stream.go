package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"time"
)

type patchReader struct {
	reader  *bufio.Reader
	pending string
	path    string
	err     error
}

type commitChunk struct {
	paths   []string
	changes []change
	loaded  int
	done    bool
	err     error
}

// streamCommit sends complete file sections as soon as a screenful is ready.
// The caller must cancel ctx when it stops receiving.
func (r repository) streamCommit(ctx context.Context, hash string, firstLines int) <-chan commitChunk {
	out := make(chan commitChunk, 1)
	go func() {
		defer close(out)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		r.context = ctx
		type statusResult struct {
			data []byte
			err  error
		}
		status := make(chan statusResult, 1)
		go func() {
			data, err := r.commandStdout("show", "--first-parent", "--format=", "--name-status", "-M", "-z", hash, "--")
			status <- statusResult{data, err}
		}()
		args := append([]string{"show", "--first-parent", "--format="}, r.diffOptions()...)
		args = append(args, "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
		cmd := r.gitCommand(args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		pipe, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			select {
			case out <- commitChunk{done: true, err: err}:
			case <-ctx.Done():
			}
			return
		}
		finished := false
		defer func() {
			if !finished {
				cancel()
				_ = cmd.Wait()
			}
		}()
		result := <-status
		if result.err != nil {
			select {
			case out <- commitChunk{done: true, err: result.err}:
			case <-ctx.Done():
			}
			return
		}
		paths, oldPaths, err := parseCommitPaths(result.data)
		if err != nil {
			select {
			case out <- commitChunk{done: true, err: err}:
			case <-ctx.Done():
			}
			return
		}
		known := make(map[string]bool, len(paths))
		for _, path := range paths {
			known[path] = true
		}
		seen := make(map[string]bool, len(paths))
		var changes []change
		loaded, lines := 0, 0
		first := true
		last := time.Now()
		send := func(done bool, err error) bool {
			chunk := commitChunk{paths: paths, changes: changes, loaded: loaded, done: done, err: err}
			select {
			case out <- chunk:
				changes, lines, first, last = nil, 0, false, time.Now()
				return true
			case <-ctx.Done():
				return false
			}
		}
		add := func(path, diff string, found bool) error {
			diff, err := r.commitFileDiff(hash, path, oldPaths[path], diff, found)
			if err != nil {
				return err
			}
			c := change{path: path, oldPath: oldPaths[path], diff: diff}
			c.syntaxSkipped = highlightable(c)
			changes = append(changes, c)
			seen[path] = true
			loaded++
			lines += strings.Count(diff, "\n")
			if first && lines >= max(1, firstLines) || !first && (len(changes) >= 2000 || time.Since(last) >= 100*time.Millisecond) {
				if !send(false, nil) {
					return ctx.Err()
				}
			}
			return nil
		}
		reader := newPatchReader(pipe)
		for {
			path, section, readErr := reader.next()
			if readErr != nil {
				if readErr != io.EOF {
					err = readErr
				}
				break
			}
			if known[path] && !seen[path] {
				if err = add(path, section, true); err != nil {
					break
				}
			}
		}
		if err != nil {
			_ = cmd.Process.Kill()
		}
		waitErr := cmd.Wait()
		finished = true
		if err == nil && waitErr != nil {
			err = gitError(stderr.Bytes(), waitErr)
		}
		if err == nil {
			for _, path := range paths {
				if !seen[path] {
					if err = add(path, "", false); err != nil {
						break
					}
				}
			}
		}
		send(true, err)
	}()
	return out
}

func newPatchReader(reader io.Reader) *patchReader {
	return &patchReader{reader: bufio.NewReader(reader)}
}

// next returns one complete section. Unrecognized paths are returned empty so
// the caller can fall back to a per-path Git query after the stream ends.
func (p *patchReader) next() (string, string, error) {
	var section strings.Builder
	for {
		line := p.pending
		p.pending = ""
		if line == "" && p.err == nil {
			line, p.err = p.reader.ReadString('\n')
		}
		if line == "" {
			if section.Len() == 0 {
				return "", "", p.err
			}
			text := section.String()
			return patchSectionPath(p.path, text), text, nil
		}
		header := strings.TrimSuffix(line, "\n")
		path, boundary := "", false
		if strings.HasPrefix(header, "diff ") {
			path, boundary = patchPath(header), true
		} else if submodulePath, _, ok := submoduleLine(header); ok && p.path != submodulePath {
			path, boundary = submodulePath, true
		}
		if boundary && section.Len() > 0 {
			p.pending = line
			text := section.String()
			return patchSectionPath(p.path, text), text, nil
		}
		if boundary {
			p.path = path
		}
		section.WriteString(line)
	}
}

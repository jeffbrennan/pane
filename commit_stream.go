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
	meta    commitInfo
	loaded  int
	done    bool
	err     error
}

// streamCommit sends complete file sections as soon as a screenful is ready.
// The caller must cancel ctx when it stops receiving.
func (r repository) streamCommit(ctx context.Context, hash string, firstLines int) <-chan commitChunk {
	patch := append([]string{"show", "--first-parent", "--format="}, r.diffOptions()...)
	patch = append(patch, "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
	status := []string{"show", "--first-parent", "--format=", "--name-status", "-M", "-z", hash, "--"}
	source := r.streamPatch(ctx, status, patch, firstLines, func(path, oldPath, diff string, ok bool) (string, error) {
		return r.commitFileDiff(hash, path, oldPath, diff, ok)
	})
	out := make(chan commitChunk, 1)
	go func() {
		defer close(out)
		meta := make(chan commitInfo, 1)
		go func() {
			commit, err := r.commitDetail(hash)
			if err != nil {
				commit = commitInfo{}
			}
			meta <- commit
		}()
		var detail commitInfo
		metaReady := false
		for chunk := range source {
			if !metaReady {
				select {
				case detail = <-meta:
					metaReady = true
				case <-ctx.Done():
					return
				}
			}
			chunk.meta = detail
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// streamBase is streamCommit for a merge base, which diffs the working tree
// against the base instead of showing a commit. Untracked files are not part of
// that diff, so they join the final chunk.
func (r repository) streamBase(ctx context.Context, hash string, firstLines int) <-chan commitChunk {
	patch := append([]string{"diff"}, r.diffOptions()...)
	patch = append(patch, "--src-prefix=a/", "--dst-prefix=b/", hash, "--")
	status := []string{"diff", "--name-status", "-M", "-z", hash, "--"}
	source := r.streamPatch(ctx, status, patch, firstLines, func(path, oldPath, diff string, ok bool) (string, error) {
		return r.baseFileDiff(hash, path, oldPath, diff, ok)
	})
	out := make(chan commitChunk, 1)
	go func() {
		defer close(out)
		for chunk := range source {
			if !chunk.done {
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
				continue
			}
			untracked, err := r.untrackedChanges()
			if chunk.err == nil {
				chunk.err = err
			}
			chunk.changes = append(chunk.changes, untracked...)
			select {
			case out <- chunk:
			case <-ctx.Done():
			}
			return
		}
	}()
	return out
}

// streamPatch reads statusArgs to learn every path, then sends the patch from
// patchArgs one file at a time. Fallback resolves files the patch reader did
// not recognise.
func (r repository) streamPatch(ctx context.Context, statusArgs, patchArgs []string, firstLines int, fallback func(path, oldPath, diff string, ok bool) (string, error)) <-chan commitChunk {
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
			data, err := r.commandStdout(statusArgs...)
			status <- statusResult{data, err}
		}()
		cmd := r.gitCommand(patchArgs...)
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
			diff, err := fallback(path, oldPaths[path], diff, found)
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
		var line []byte
		if p.pending != "" {
			line = []byte(p.pending)
		}
		p.pending = ""
		if len(line) == 0 && p.err == nil {
			var err error
			line, err = p.reader.ReadSlice('\n')
			if err == bufio.ErrBufferFull {
				large := append([]byte(nil), line...)
				for err == bufio.ErrBufferFull {
					line, err = p.reader.ReadSlice('\n')
					large = append(large, line...)
				}
				line = large
			}
			p.err = err
		}
		if len(line) == 0 {
			if section.Len() == 0 {
				return "", "", p.err
			}
			text := section.String()
			return patchSectionPath(p.path, text), text, nil
		}
		header := bytes.TrimSuffix(line, []byte("\n"))
		path, boundary := "", false
		if bytes.HasPrefix(header, []byte("diff ")) {
			path, boundary = patchPath(string(header)), true
		} else if bytes.HasPrefix(header, []byte("Submodule ")) {
			if submodulePath, _, ok := submoduleLine(string(header)); ok && p.path != submodulePath {
				path, boundary = submodulePath, true
			}
		}
		if boundary && section.Len() > 0 {
			p.pending = string(line)
			text := section.String()
			return patchSectionPath(p.path, text), text, nil
		}
		if boundary {
			p.path = path
		}
		section.Write(line)
	}
}

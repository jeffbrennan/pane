package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type viewedKey struct{ view, path string }

// viewedStore persists which files a reviewer has marked viewed. A mark is
// tied to the file's displayed diff, so editing the file invalidates it.
type viewedStore struct {
	path  string
	once  sync.Once
	mu    sync.Mutex
	marks map[viewedKey]string
}

func newViewedStore(root string) *viewedStore {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	sum := sha256.Sum256([]byte(root))
	return &viewedStore{path: filepath.Join(dir, "pane", "viewed-"+hex.EncodeToString(sum[:8]))}
}

func (s *viewedStore) load() {
	s.once.Do(func() {
		s.marks = make(map[viewedKey]string)
		file, err := os.Open(s.path)
		if err != nil {
			return
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), "\t")
			if len(fields) != 3 {
				continue
			}
			view, err := base64.StdEncoding.DecodeString(fields[0])
			if err != nil {
				continue
			}
			path, err := base64.StdEncoding.DecodeString(fields[1])
			if err != nil {
				continue
			}
			s.marks[viewedKey{string(view), string(path)}] = fields[2]
		}
	})
}

func (s *viewedStore) marked(view, path, blob string) bool {
	if s == nil || view == "" || path == "" || blob == "" {
		return false
	}
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	marked, ok := s.marks[viewedKey{view, path}]
	return ok && marked == blob
}

// set records or clears a mark. Write failures only cost a mark next launch.
func (s *viewedStore) set(view, path, blob string, marked bool) {
	if s == nil || view == "" || path == "" || blob == "" {
		return
	}
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	key := viewedKey{view, path}
	if marked {
		s.marks[key] = blob
	} else {
		delete(s.marks, key)
	}
	file, err := os.Create(s.path)
	if err != nil {
		if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
			return
		}
		file, err = os.Create(s.path)
		if err != nil {
			return
		}
	}
	defer file.Close()
	for key, blob := range s.marks {
		fmt.Fprintf(file, "%s\t%s\t%s\n", base64.StdEncoding.EncodeToString([]byte(key.view)), base64.StdEncoding.EncodeToString([]byte(key.path)), blob)
	}
}

// count returns how many files in a view are marked.
func (s *viewedStore) count(view string) int {
	if s == nil || view == "" {
		return 0
	}
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for key := range s.marks {
		if key.view == view {
			total++
		}
	}
	return total
}

func diffBlob(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:8])
}

// applyViewed marks file headers and collapses files already marked viewed.
func (m *model) applyViewed(rows []row, changes []change) {
	if m.viewed == nil || m.viewIdentity == "" {
		return
	}
	blobs := make(map[string]string, len(changes))
	for _, c := range changes {
		blobs[c.path] = diffBlob(c.diff)
	}
	for i := range rows {
		if rows[i].kind != 'f' {
			continue
		}
		path := rows[i].path
		if !m.viewed.marked(m.viewIdentity, path, blobs[path]) {
			continue
		}
		rows[i].viewed = true
		m.collapseViewed(path)
	}
}

func (m *model) collapseViewed(path string) {
	key := contextKey{path: path, commit: m.contextCommit, staged: m.staged, base: m.contextBase}
	if m.generatedHandled == nil {
		m.generatedHandled = make(map[contextKey]bool)
	}
	if m.generatedHandled[key] {
		return
	}
	m.generatedHandled[key] = true
	if m.collapsedFiles == nil {
		m.collapsedFiles = make(map[contextKey]bool)
	}
	m.collapsedFiles[key] = true
}

// seedViewedCollapse collapses newly seen viewed files before rows are built,
// so streaming chunks do not append their contents first.
func (m *model) seedViewedCollapse(changes []change) {
	if m.viewed == nil || m.viewIdentity == "" {
		return
	}
	for _, c := range changes {
		if m.viewed.marked(m.viewIdentity, c.path, diffBlob(c.diff)) {
			m.collapseViewed(c.path)
		}
	}
}

// viewedCount reports marked files and total files in the rows on screen.
func viewedCountRows(rows []row) (int, int) {
	seen := make(map[string]bool)
	viewed := make(map[string]bool)
	for _, r := range rows {
		if r.kind != 'f' || r.path == "" || seen[r.path] {
			continue
		}
		seen[r.path] = true
		if r.viewed {
			viewed[r.path] = true
		}
	}
	return len(viewed), len(seen)
}

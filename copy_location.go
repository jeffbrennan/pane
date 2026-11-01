package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
)

func locationText(path string, line int) string {
	if line > 0 {
		return fmt.Sprintf("%s:%d", path, line)
	}
	return path
}

func remotePermalink(remote, hash, path string, line int) string {
	if hash == "" || path == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@") {
		host, repo, ok := strings.Cut(strings.TrimPrefix(remote, "git@"), ":")
		if !ok {
			return ""
		}
		remote = "https://" + host + "/" + repo
	}
	u, err := url.Parse(remote)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" && !strings.HasPrefix(host, "github.") && !strings.HasPrefix(host, "gitlab.") {
		return ""
	}
	repo := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if parts := strings.Split(repo, "/"); len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
		return ""
	}
	u.Path = "/" + repo + "/blob/" + hash + "/" + path
	u.RawPath = ""
	if line > 0 {
		u.Fragment = "L" + strconv.Itoa(line)
	}
	return u.String()
}

func copyLocationCmd(repo git.Repo, hash, path string, line int) tea.Cmd {
	return func() tea.Msg {
		text := locationText(path, line)
		if hash != "" {
			if out, err := repo.Command("config", "--get", "remote.origin.url"); err == nil {
				if link := remotePermalink(strings.TrimSpace(string(out)), hash, path, line); link != "" {
					text = link
				}
			}
		}
		return copyCmd(text, text)()
	}
}

func (m model) copyDiffLocation(hash string) tea.Cmd {
	if m.pos < 0 || m.pos >= len(m.rows) {
		return nil
	}
	r := m.rows[m.pos]
	if r.path == "" {
		return nil
	}
	line := r.target
	if r.kind == '-' || r.kind == 'd' {
		closest := 0
		for _, candidate := range m.rows {
			if candidate.path != r.path || candidate.commitIndex != r.commitIndex || (candidate.kind != '+' && candidate.kind != ' ') || candidate.sourceLine <= 0 {
				continue
			}
			distance := candidate.sourceLine - line
			if distance < 0 {
				distance = -distance
			}
			bestDistance := closest - line
			if bestDistance < 0 {
				bestDistance = -bestDistance
			}
			if closest == 0 || distance < bestDistance {
				closest = candidate.sourceLine
			}
		}
		if closest > 0 {
			line = closest
		}
	}
	return copyLocationCmd(m.repo, hash, r.path, line)
}

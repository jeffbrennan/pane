//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"bytes"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

type themeInput struct {
	*os.File
	handler func(bool)
}

func startThemeTracking() themeTracking {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return themeTracking{}
	}
	input := &themeInput{File: tty}
	return themeTracking{
		input:      input,
		query:      func() { _, _ = tty.WriteString("\x1b]11;?\x1b\\") },
		setHandler: func(handler func(bool)) { input.handler = handler },
		close:      func() { _ = tty.Close() },
	}
}

func (input *themeInput) Read(p []byte) (int, error) {
	n, err := input.File.Read(p)
	if n == 0 || err != nil {
		return n, err
	}
	data := append([]byte(nil), p[:n]...)
	prefix := []byte("\x1b]11;rgb:")
	for {
		start := bytes.Index(data, prefix)
		if start < 0 {
			for i := 1; i < len(prefix) && i <= len(data); i++ {
				if bytes.Equal(data[len(data)-i:], prefix[:i]) {
					start = len(data) - i
					break
				}
			}
		}
		if start < 0 {
			break
		}
		end := -1
		if bytes.HasPrefix(data[start:], prefix) {
			for i := start + len(prefix); i < len(data); i++ {
				if data[i] == '\a' {
					end = i + 1
					break
				}
				if data[i] == '\\' && i > start && data[i-1] == '\x1b' {
					end = i + 1
					break
				}
			}
		}
		if end >= 0 {
			if light, ok := lightBackgroundResponse(string(data[start:end])); ok {
				if input.handler != nil {
					input.handler(light)
				}
				data = append(data[:start], data[end:]...)
				continue
			}
			break
		}
		if len(data)-start >= 128 || len(data) >= len(p) {
			break
		}
		var fds unix.FdSet
		fds.Set(int(input.Fd()))
		timeout := unix.NsecToTimeval(int64(20 * time.Millisecond))
		ready, selectErr := unix.Select(int(input.Fd())+1, &fds, nil, nil, &timeout)
		if selectErr == unix.EINTR {
			continue
		}
		if selectErr != nil || ready == 0 {
			break
		}
		buffer := make([]byte, len(p)-len(data))
		count, readErr := input.File.Read(buffer)
		data = append(data, buffer[:count]...)
		if readErr != nil || count == 0 {
			break
		}
	}
	return copy(p, data), nil
}

func terminalLightBackground() (bool, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, false
	}
	defer tty.Close()
	state, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return false, false
	}
	defer term.Restore(tty.Fd(), state)
	if _, err := tty.WriteString("\x1b]11;?\x1b\\"); err != nil {
		return false, false
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	var response []byte
	for len(response) < 128 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		var fds unix.FdSet
		fds.Set(int(tty.Fd()))
		timeout := unix.NsecToTimeval(int64(remaining))
		ready, err := unix.Select(int(tty.Fd())+1, &fds, nil, nil, &timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil || ready == 0 {
			break
		}
		var buf [1]byte
		if n, err := tty.Read(buf[:]); err != nil || n == 0 {
			break
		}
		response = append(response, buf[0])
		if buf[0] == '\a' || len(response) >= 2 && response[len(response)-2] == '\x1b' && buf[0] == '\\' {
			return lightBackgroundResponse(string(response))
		}
	}
	return false, false
}

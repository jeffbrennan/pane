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
	handler       func(bool)
	notifyHandler func()
	notified      bool
	pending       []byte
}

func startThemeTracking() themeTracking {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return themeTracking{}
	}
	input := &themeInput{File: tty}
	return themeTracking{
		input: input,
		query: func() {
			_, _ = tty.WriteString("\x1b]11;?\x1b\\")
			// DECRQM: if mode 2031 is supported, enabling it replaces polling.
			_, _ = tty.WriteString("\x1b[?2031$p")
		},
		setHandler:       func(handler func(bool)) { input.handler = handler },
		setNotifyHandler: func(handler func()) { input.notifyHandler = handler },
		close:            func() { _ = tty.Close() },
	}
}

func (input *themeInput) Read(p []byte) (int, error) {
	n, err := input.File.Read(p)
	if n == 0 || err != nil {
		return n, err
	}
	data := append(input.pending, p[:n]...)
	input.pending = nil
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
	data = input.stripNotifications(data)
	return copy(p, data), nil
}

func (input *themeInput) stripNotifications(data []byte) []byte {
	sequences := [][]byte{[]byte("\x1b[?997;"), []byte("\x1b[?2031;")}
	for {
		start, prefix := -1, []byte(nil)
		for _, candidate := range sequences {
			if index := bytes.Index(data, candidate); index >= 0 && (start < 0 || index < start) {
				start, prefix = index, candidate
			}
		}
		if start < 0 {
			break
		}
		end := -1
		if bytes.Equal(prefix, sequences[0]) {
			if index := bytes.IndexByte(data[start+len(prefix):], 'n'); index >= 0 {
				end = start + len(prefix) + index + 1
				light := data[start+len(prefix)] == '2'
				if input.handler != nil {
					input.handler(light)
				}
			}
		} else {
			if index := bytes.Index(data[start+len(prefix):], []byte("$y")); index >= 0 {
				end = start + len(prefix) + index + 2
				if data[start+len(prefix)] != '0' && !input.notified {
					input.notified = true
					_, _ = input.WriteString("\x1b[?2031h")
					if input.notifyHandler != nil {
						input.notifyHandler()
					}
				}
			}
		}
		if end < 0 {
			break
		}
		data = append(data[:start], data[end:]...)
	}
	for _, prefix := range sequences {
		for length := 1; length < len(prefix) && length <= len(data); length++ {
			if bytes.HasSuffix(data, prefix[:length]) {
				input.pending = append(input.pending[:0], data[len(data)-length:]...)
				data = data[:len(data)-length]
				return data
			}
		}
	}
	return data
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

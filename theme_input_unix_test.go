//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestThemeInputPreservesKeys(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	var detected []bool
	input := themeInput{File: reader, handler: func(light bool) { detected = append(detected, light) }}
	_, err = writer.WriteString("j\x1b]11;rgb:ffff/ffff/ffff\x1b\\k")
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := input.Read(buf)
	if err != nil || string(buf[:n]) != "jk" || len(detected) != 1 || !detected[0] {
		t.Fatalf("input=%q themes=%v error=%v", buf[:n], detected, err)
	}
}

func TestThemePollStopsWhenNotificationsArrive(t *testing.T) {
	var queries atomic.Int64
	input := themeInput{query: func() { queries.Add(1) }}
	stop := input.startPoll(2 * time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	if queries.Load() == 0 {
		t.Fatal("theme poll did not query")
	}
	input.notified.Store(true)
	time.Sleep(5 * time.Millisecond)
	stopped := queries.Load()
	time.Sleep(10 * time.Millisecond)
	if queries.Load() != stopped {
		t.Fatal("theme poll queried after notifications were enabled")
	}
	stop()
}

func TestThemeInputDeduplicatesUnchangedBackground(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	calls := 0
	input := themeInput{File: reader, handler: func(bool) { calls++ }}
	if _, err := writer.WriteString("\x1b]11;rgb:0000/0000/0000\x1b\\\x1b]11;rgb:0000/0000/0000\x1b\\"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	if _, err := input.Read(buf); err != nil || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
	if _, err := writer.WriteString("\x1b]11;rgb:ffff/ffff/ffff\x1b\\"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Read(buf); err != nil || calls != 2 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestThemeInputHandlesThemeNotifications(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	var lights []bool
	notified := 0
	input := themeInput{File: reader, handler: func(light bool) { lights = append(lights, light) }, notifyHandler: func() { notified++ }}
	if _, err := writer.WriteString("a\x1b[?2031;1$y\x1b[?997;2nb\x1b[?997;1nc"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := input.Read(buf)
	if err != nil || string(buf[:n]) != "abc" || notified != 1 || len(lights) != 2 || !lights[0] || lights[1] {
		t.Fatalf("input=%q notified=%d lights=%v error=%v", buf[:n], notified, lights, err)
	}
}

func TestThemeInputBuffersSplitNotification(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	notified := false
	input := themeInput{File: reader, notifyHandler: func() { notified = true }}
	if _, err := writer.WriteString("a\x1b[?203"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(time.Millisecond)
		_, _ = writer.WriteString("1;1$yb")
	}()
	buf := make([]byte, 256)
	n, err := input.Read(buf)
	if err != nil || string(buf[:n]) != "a" || notified {
		t.Fatalf("first input=%q notified=%t error=%v", buf[:n], notified, err)
	}
	n, err = input.Read(buf)
	if err != nil || string(buf[:n]) != "b" || !notified {
		t.Fatalf("second input=%q notified=%t error=%v", buf[:n], notified, err)
	}
}

func TestThemeInputHandlesSplitResponse(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	detected := false
	input := themeInput{File: reader, handler: func(light bool) { detected = light }}
	if _, err := writer.WriteString("\x1b]11;"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(time.Millisecond)
		_, _ = writer.WriteString("rgb:ffff/ffff/ffff\a")
	}()
	buf := make([]byte, 256)
	n, err := input.Read(buf)
	if err != nil || n != 0 || !detected {
		t.Fatalf("split response: input=%q detected=%t error=%v", buf[:n], detected, err)
	}
}

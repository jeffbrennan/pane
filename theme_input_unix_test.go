//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"os"
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

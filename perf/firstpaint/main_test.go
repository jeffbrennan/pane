package main

import (
	"bytes"
	"testing"
)

func TestTerminalOutputHandlesSplitSequences(t *testing.T) {
	output := terminalOutput{content: "needle", after: "ready"}
	var replies bytes.Buffer
	data := []byte("\x1b[6n\x1b]11;?\x1b\\read-only read-only needle needle ready ready")
	paints, contents, ready := 0, 0, 0
	for _, b := range data {
		painted, found, isReady := output.feed([]byte{b}, &replies)
		if painted {
			paints++
		}
		if found {
			contents++
		}
		if isReady {
			ready++
		}
	}
	if paints != 1 || contents != 1 || ready != 1 || replies.String() != "\x1b[1;1R\x1b]11;rgb:0000/0000/0000\x1b\\" {
		t.Fatalf("paints=%d contents=%d ready=%d replies=%q", paints, contents, ready, replies.String())
	}
}

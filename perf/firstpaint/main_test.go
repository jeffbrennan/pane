package main

import (
	"bytes"
	"testing"
)

func TestTerminalOutputHandlesSplitSequences(t *testing.T) {
	var output terminalOutput
	var replies bytes.Buffer
	data := []byte("\x1b[6n\x1b]11;?\x1b\\read-only read-only")
	paints := 0
	for _, b := range data {
		if output.feed([]byte{b}, &replies) {
			paints++
		}
	}
	if paints != 1 || replies.String() != "\x1b[1;1R\x1b]11;rgb:0000/0000/0000\x1b\\" {
		t.Fatalf("paints=%d replies=%q", paints, replies.String())
	}
}

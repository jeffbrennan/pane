package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

func main() {
	binary := flag.String("binary", ".scratch/perf-pane", "pane binary")
	repo := flag.String("repo", ".", "repository to open")
	revision := flag.String("revision", "HEAD", "commit to open")
	idle := flag.Duration("idle", time.Minute, "time to observe after first paint")
	logPath := flag.String("log", "", "append metrics to this TSV file")
	flag.Parse()
	if err := run(*binary, *repo, *revision, *idle, *logPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type terminalOutput struct {
	tail    []byte
	painted bool
}

func (output *terminalOutput) feed(data []byte, terminal io.Writer) bool {
	painted := false
	for _, b := range data {
		output.tail = append(output.tail, b)
		switch {
		case bytes.HasSuffix(output.tail, []byte("\x1b[6n")):
			_, _ = io.WriteString(terminal, "\x1b[1;1R")
		case bytes.HasSuffix(output.tail, []byte("\x1b]11;?\x1b\\")):
			_, _ = io.WriteString(terminal, "\x1b]11;rgb:0000/0000/0000\x1b\\")
		case !output.painted && bytes.HasSuffix(output.tail, []byte("read-only")):
			output.painted, painted = true, true
		}
		if len(output.tail) > 64 {
			copy(output.tail, output.tail[len(output.tail)-64:])
			output.tail = output.tail[:64]
		}
	}
	return painted
}

func run(binary, repo, revision string, idle time.Duration, logPath string) error {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	cmd := exec.Command(binary, revision)
	cmd.Dir = repo
	start := time.Now()
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 160, Rows: 50})
	if err != nil {
		return err
	}
	defer terminal.Close()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	var outputBytes atomic.Int64
	painted := make(chan time.Time, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		var output terminalOutput
		buffer := make([]byte, 8192)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				outputBytes.Add(int64(n))
				if output.feed(buffer[:n], terminal) {
					painted <- time.Now()
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var first time.Time
	select {
	case first = <-painted:
	case <-readDone:
		return fmt.Errorf("pane exited before the title appeared")
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timed out waiting for the title")
	}
	bytesAtPaint := outputBytes.Load()
	time.Sleep(idle)
	idleBytes := outputBytes.Load() - bytesAtPaint
	if _, err := io.WriteString(terminal, "\x03"); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			return fmt.Errorf("pane exit: %w", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-wait
		return fmt.Errorf("pane did not exit")
	}
	metrics := map[string]int64{"first_paint_ns": first.Sub(start).Nanoseconds(), "idle_terminal_bytes": idleBytes}
	if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		metrics["process_cpu_ns"] = usage.Utime.Nano() + usage.Stime.Nano()
		rss := usage.Maxrss
		if runtime.GOOS != "darwin" {
			rss *= 1024
		}
		metrics["peak_rss_bytes"] = rss
	}
	for name, value := range metrics {
		fmt.Printf("commit %s %d\n", name, value)
		if logPath != "" {
			if err := appendMetric(logPath, name, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendMetric(path, metric string, value int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	revision, err := exec.Command("git", "describe", "--always", "--dirty").Output()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(file, "%s\t%s\tcommit\t%s\t%d\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(revision)), metric, value)
	return err
}

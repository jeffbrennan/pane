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
	"strconv"
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
	scenario := flag.String("scenario", "commit", "scenario name for the results log")
	args := flag.String("args", "", "alternate pane arguments, for example '-b main'")
	idle := flag.Duration("idle", time.Minute, "time to observe after first paint")
	settle := flag.Duration("settle", 2*time.Second, "wait for background loading before the idle window")
	logPath := flag.String("log", "", "append metrics to this TSV file")
	content := flag.String("content", os.Getenv("PANE_PERF_CONTENT"), "first diff text to wait for")
	key := flag.String("key", "", "key to send after the title appears")
	after := flag.String("after", "", "wait for this text before sending the key")
	flag.Parse()
	if err := run(*binary, *repo, *revision, *scenario, *args, *idle, *settle, *logPath, *content, *key, *after); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type terminalOutput struct {
	tail    []byte
	painted bool
	content string
	found   bool
	after   string
	ready   bool
}

func (output *terminalOutput) feed(data []byte, terminal io.Writer) (bool, bool, bool) {
	painted, found, ready := false, false, false
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
		if output.content != "" && !output.found && bytes.HasSuffix(output.tail, []byte(output.content)) {
			output.found, found = true, true
		}
		if output.after != "" && !output.ready && bytes.HasSuffix(output.tail, []byte(output.after)) {
			output.ready, ready = true, true
		}
		limit := max(64, len(output.content), len(output.after))
		if len(output.tail) > limit {
			copy(output.tail, output.tail[len(output.tail)-limit:])
			output.tail = output.tail[:limit]
		}
	}
	return painted, found, ready
}

func run(binary, repo, revision, scenario, paneArgs string, idle, settle time.Duration, logPath, content, key, after string) error {
	if idle < 0 || settle < 0 {
		return fmt.Errorf("idle and settle durations cannot be negative")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	arguments := []string{revision}
	if paneArgs != "" {
		arguments = strings.Fields(paneArgs)
	}
	cmd := exec.Command(binary, arguments...)
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
	contentPainted := make(chan time.Time, 1)
	ready := make(chan struct{}, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		output := terminalOutput{content: content, after: after}
		buffer := make([]byte, 8192)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				outputBytes.Add(int64(n))
				first, found, isReady := output.feed(buffer[:n], terminal)
				if first {
					painted <- time.Now()
				}
				if found {
					contentPainted <- time.Now()
				}
				if isReady {
					ready <- struct{}{}
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
	var firstContent time.Time
	if content != "" {
		select {
		case firstContent = <-contentPainted:
		case <-readDone:
			return fmt.Errorf("pane exited before the requested content appeared")
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timed out waiting for content %q", content)
		}
	}
	if key != "" {
		if _, err := io.WriteString(terminal, key); err != nil {
			return err
		}
	}
	if after != "" {
		select {
		case <-ready:
		case <-readDone:
			return fmt.Errorf("pane exited before the requested key point")
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timed out waiting for %q", after)
		}
	}
	time.Sleep(settle)
	bytesAtPaint := outputBytes.Load()
	cpuAtPaint := processCPU(cmd.Process.Pid)
	time.Sleep(idle)
	idleBytes := outputBytes.Load() - bytesAtPaint
	idleCPU := processCPU(cmd.Process.Pid) - cpuAtPaint
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
	metrics := map[string]int64{"first_paint_ns": first.Sub(start).Nanoseconds(), "idle_terminal_bytes": idleBytes, "idle_duration_ns": idle.Nanoseconds(), "idle_cpu_ns": idleCPU}
	if !firstContent.IsZero() {
		metrics["first_content_ns"] = firstContent.Sub(start).Nanoseconds()
	}
	if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		metrics["process_cpu_ns"] = usage.Utime.Nano() + usage.Stime.Nano()
		rss := usage.Maxrss
		if runtime.GOOS != "darwin" {
			rss *= 1024
		}
		metrics["peak_rss_bytes"] = rss
	}
	for name, value := range metrics {
		fmt.Printf("%s %s %d\n", scenario, name, value)
		if logPath != "" {
			if err := appendMetric(logPath, scenario, name, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func processCPU(pid int) int64 {
	out, err := exec.Command("ps", "-o", "time=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	value := strings.TrimSpace(string(out))
	fields := strings.Split(value, ":")
	var seconds float64
	for _, field := range fields {
		part, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return 0
		}
		seconds = seconds*60 + part
	}
	return int64(seconds * float64(time.Second))
}

func appendMetric(path, scenario, metric string, value int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	revision, err := exec.Command("git", "describe", "--always", "--dirty").Output()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(file, "%s\t%s\t%s\t%s\t%d\t%s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(string(revision)), scenario, metric, value, os.Getenv("PANE_PERF_RUN"))
	return err
}

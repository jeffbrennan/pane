package main

import (
	"github.com/jeffbrennan/pane/internal/git"

	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

func main() {
	if debug.SetMemoryLimit(-1) > 1<<40 {
		// Large diffs otherwise let the runtime grow the heap well past what
		// the rows need; 1 GiB keeps collection responsive without thrashing.
		debug.SetMemoryLimit(1 << 30)
	}
	repo, err := git.OpenRepository()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	piped := !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd())
	var prefetch *startupPrefetch
	if !piped && len(os.Args) == 1 {
		prefetch = startPrefetch(repo)
	}
	var m model
	if piped && len(os.Args) == 1 {
		m, err = initialSequence(repo, os.Stdin)
	} else {
		m, err = initialModel(repo, os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m.prefetch = prefetch
	theme, colorFGBG := strings.ToLower(strings.TrimSpace(os.Getenv("PANE_THEME"))), os.Getenv("COLORFGBG")
	m.lightTheme = lightThemeFromEnv(theme, colorFGBG)
	options := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()}
	var tracking themeTracking
	if theme != "light" && theme != "dark" {
		tracking = startThemeTracking()
		if tracking.input != nil {
			// Init queries the background without blocking startup; terminals
			// that never answer would otherwise delay every launch.
			m.themeAuto, m.themeQuery = true, tracking.query
			options = append(options, tea.WithInput(tracking.input))
			defer tracking.close()
		} else if light, ok := terminalLightBackground(); ok {
			m.lightTheme = light
		}
	}
	if piped && tracking.input == nil {
		options = append(options, tea.WithInputTTY())
	}
	program := tea.NewProgram(m, options...)
	if tracking.setHandler != nil {
		tracking.setHandler(func(light bool) { go program.Send(terminalBackgroundChanged(light)) })
	}
	if tracking.setNotifyHandler != nil {
		tracking.setNotifyHandler(func() { go program.Send(terminalThemeNotifications(true)) })
	}
	if tracking.poll != nil {
		defer tracking.poll()()
	}
	stopProfile := startCPUProfile(os.Getenv("PANE_CPUPROFILE"))
	stopTrace := startTrace(os.Getenv("PANE_TRACE"))
	final, err := program.Run()
	stopTrace()
	stopProfile()
	if path := os.Getenv("PANE_MEMPROFILE"); path != "" {
		if profileErr := writeHeapProfile(path); profileErr != nil {
			fmt.Fprintln(os.Stderr, profileErr)
		}
	}
	if result, ok := final.(model); ok {
		result.closeHistoryStreams()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func startTrace(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := trace.Start(file); err != nil {
		file.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return func() {
		trace.Stop()
		file.Close()
	}
}

func writeHeapProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	runtime.GC()
	return pprof.WriteHeapProfile(file)
}

// startCPUProfile records a CPU profile to path, for building default.pgo
// from real sessions. It returns a function that stops recording.
func startCPUProfile(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return func() {
		pprof.StopCPUProfile()
		file.Close()
	}
}

package ui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jeffbrennan/pane/internal/git"
	"github.com/mattn/go-isatty"
)

// Run opens the repository and runs the terminal UI with the given arguments.
func Run(args []string) error {
	repo, err := git.OpenRepository()
	if err != nil {
		return err
	}
	piped := !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd())
	var prefetch *startupPrefetch
	if !piped && len(args) == 0 {
		prefetch = startPrefetch(repo)
	}
	var m model
	if piped && len(args) == 0 {
		m, err = initialSequence(repo, os.Stdin)
	} else {
		m, err = initialModel(repo, args)
	}
	if err != nil {
		return err
	}
	m.prefetch = prefetch
	keys, keysErr := loadKeys(keysPath())
	activeKeys = keys
	if keysErr != nil {
		m.flash = keysErr.Error()
	}
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
	final, err := program.Run()
	if result, ok := final.(model); ok {
		result.closeHistoryStreams()
	}
	return err
}

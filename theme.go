package main

import (
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/jeffbrennan/pane/internal/syntax"
)

type themeTracking struct {
	input            io.Reader
	query            func()
	poll             func() func()
	setHandler       func(func(bool))
	setNotifyHandler func(func())
	close            func()
}

type terminalBackgroundChanged bool
type terminalThemeNotifications bool

const (
	darkAddedBackground   = syntax.DarkAddedBackground
	darkRemovedBackground = syntax.DarkRemovedBackground
)

func lightBackgroundResponse(response string) (bool, bool) {
	start := strings.Index(response, "\x1b]11;rgb:")
	if start < 0 {
		return false, false
	}
	value := response[start+len("\x1b]11;rgb:"):]
	value = strings.TrimRight(value, "\a\x1b\\")
	parts := strings.Split(value, "/")
	if len(parts) != 3 {
		return false, false
	}
	var rgb [3]float64
	for i, part := range parts {
		if len(part) == 0 || len(part) > 4 {
			return false, false
		}
		n, err := strconv.ParseUint(part, 16, 16)
		if err != nil {
			return false, false
		}
		rgb[i] = float64(n) / float64((uint64(1)<<(4*len(part)))-1)
	}
	return (math.Max(rgb[0], math.Max(rgb[1], rgb[2]))+math.Min(rgb[0], math.Min(rgb[1], rgb[2])))/2 >= 0.5, true
}

func lightThemeFromEnv(theme, colorFGBG string) bool {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "light":
		return true
	case "dark":
		return false
	}
	parts := strings.Split(colorFGBG, ";")
	if len(parts) < 2 {
		return false
	}
	color, err := strconv.Atoi(parts[len(parts)-1])
	return err == nil && (color == 7 || color >= 9 && color <= 15)
}

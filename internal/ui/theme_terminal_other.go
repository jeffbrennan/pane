//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package ui

func terminalLightBackground() (bool, bool) { return false, false }

func startThemeTracking() themeTracking { return themeTracking{} }

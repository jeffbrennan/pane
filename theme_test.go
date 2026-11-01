package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLightThemeFromEnv(t *testing.T) {
	for _, tc := range []struct {
		theme, background string
		light             bool
	}{
		{"", "", false}, {"", "15;0", false}, {"", "0;15", true},
		{"", "0;7", true}, {"", "0;8", false}, {"", "0;invalid", false},
		{"light", "15;0", true}, {"dark", "0;15", false},
	} {
		if got := lightThemeFromEnv(tc.theme, tc.background); got != tc.light {
			t.Errorf("theme=%q COLORFGBG=%q: got %t", tc.theme, tc.background, got)
		}
	}
}

func TestTerminalBackgroundResponse(t *testing.T) {
	for _, tc := range []struct {
		response  string
		light, ok bool
	}{
		{"\x1b]11;rgb:ffff/eeee/dddd\x1b\\", true, true},
		{"\x1b]11;rgb:0000/1111/2222\a", false, true},
		{"\x1b]11;rgb:ff/ff/ff\a", true, true},
		{"\x1b]11;rgb:f/0/0\a", true, true},
		{"\x1b]11;rgb:zz/00/00\a", false, false},
		{"\x1b]11;rgb:ff/ff\a", false, false},
		{"", false, false},
	} {
		light, ok := lightBackgroundResponse(tc.response)
		if light != tc.light || ok != tc.ok {
			t.Errorf("response %q: got (%t, %t)", tc.response, light, ok)
		}
	}
}

func TestLightThemeDiffRendering(t *testing.T) {
	m := model{width: 80, height: 12, lightTheme: true}
	rows := buildRows([]change{{path: "file.go", diff: "@@ -1 +1 @@\n-old\n+new\n"}}, 78, true)
	for _, tc := range []struct {
		index int
		want  string
	}{{0, "\x1b[1;38;2;45;45;45;48;2;222;222;222m"}, {1, "\x1b[48;2;250;225;225m"}, {2, "\x1b[48;2;221;242;224m"}} {
		if got := m.renderDiffRow(rows[tc.index], false); !strings.Contains(got, tc.want) {
			t.Errorf("row %d missing light color: %q", tc.index, got)
		}
	}
	for _, tc := range []struct{ background, want string }{
		{"\x1b[48;2;221;242;224m", "\x1b[48;2;175;220;181m"},
		{"\x1b[48;2;250;225;225m", "\x1b[48;2;239;182;182m"},
	} {
		got := renderSyntax("abc", []syntaxSpan{{0, 3, "\x1b[35m"}}, []syntaxSpan{{0, 3, ""}}, 0, "", tc.background, true)
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "\x1b[38;2;128;35;108m") {
			t.Errorf("missing light inline or syntax color: %q", got)
		}
	}
}

func TestToggleThemeAndHistorySearch(t *testing.T) {
	m := model{screen: "history", history: historyState{searching: true}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = updated.(model)
	if m.lightTheme || m.history.draft != "T" {
		t.Fatal("theme toggle intercepted history search")
	}
	m.history.searching = false
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	if !updated.(model).lightTheme {
		t.Fatal("theme did not toggle")
	}
}

func TestAutomaticThemeChangesAndManualOverride(t *testing.T) {
	m := model{themeAuto: true}
	updated, _ := m.Update(terminalBackgroundChanged(true))
	m = updated.(model)
	if !m.lightTheme {
		t.Fatal("automatic theme did not switch to light")
	}
	updated, _ = m.Update(terminalBackgroundChanged(false))
	m = updated.(model)
	if m.lightTheme {
		t.Fatal("automatic theme did not switch to dark")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = updated.(model)
	updated, _ = m.Update(terminalBackgroundChanged(false))
	m = updated.(model)
	if !m.lightTheme || m.themeAuto {
		t.Fatal("automatic detection overrode manual theme choice")
	}
}

func TestThemeAndRepositoryRefreshCadence(t *testing.T) {
	queries := 0
	m := model{screen: "commit", themeAuto: true, themeQuery: func() { queries++ }}
	for i := 1; i <= 10; i++ {
		updated, cmd := m.Update(tick{})
		m = updated.(model)
		if i == 10 {
			batch, ok := cmd().(tea.BatchMsg)
			if !ok || len(batch) != 2 {
				t.Fatalf("tick %d: missing scheduled theme query", i)
			}
			batch[1]()
		}
	}
	if queries != 1 {
		t.Fatalf("got %d theme queries in 5 seconds", queries)
	}
	queries = 0
	m.screen = ""
	for i := 1; i <= 120; i++ {
		m.ticks = i
		if cmd := m.queryThemeOnTick(); cmd != nil {
			cmd()
		}
	}
	if queries != 12 {
		t.Fatalf("got %d theme queries per minute, want 12", queries)
	}
	m.blurred = true
	if m.queryThemeOnTick() != nil {
		t.Fatal("blurred terminal should not be queried")
	}
	m.blurred, m.ticks = false, 10
	notified, _ := m.Update(terminalThemeNotifications(true))
	m = notified.(model)
	if !m.themeNotify || m.queryThemeOnTick() != nil {
		t.Fatal("confirmed terminal notifications should replace polling")
	}
}

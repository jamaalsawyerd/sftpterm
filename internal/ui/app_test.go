package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	m, err := New(Options{Config: config.Default()})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", next)
	}
	return nm, cmd
}

func press(t *testing.T, m Model, k rune) (Model, tea.Cmd) {
	t.Helper()
	return update(t, m, tea.KeyPressMsg{Code: k, Text: string(k)})
}

func TestNewRejectsUnknownTheme(t *testing.T) {
	cfg := config.Default()
	cfg.Theme = "vaporwave-deluxe"
	if _, err := New(Options{Config: cfg}); err == nil {
		t.Fatal("expected an error for an unknown theme")
	}
}

// The rendered view must exactly fill the terminal at any size, otherwise the
// layout jitters or wraps.
func TestViewFitsTerminal(t *testing.T) {
	sizes := [][2]int{{120, 40}, {80, 24}, {minWidth, minHeight}, {49, 11}, {20, 5}, {200, 60}}
	for _, showAll := range []bool{false, true} {
		for _, sz := range sizes {
			t.Run(fmt.Sprintf("%dx%d/full=%v", sz[0], sz[1], showAll), func(t *testing.T) {
				m := newTestModel(t)
				m.notice = strings.Repeat("a very long warning ", 20)
				m, _ = update(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
				m.help.ShowAll = showAll

				out := m.View().Content
				if h := lipgloss.Height(out); h != sz[1] {
					t.Errorf("height = %d, want %d", h, sz[1])
				}
				for i, line := range strings.Split(out, "\n") {
					if w := lipgloss.Width(line); w > sz[0] {
						t.Errorf("line %d width = %d, exceeds %d", i, w, sz[0])
					}
				}
			})
		}
	}
}

func TestThemeKeyCyclesPalette(t *testing.T) {
	m := newTestModel(t)
	start := m.cfg.Theme
	m, cmd := press(t, m, 'T')
	if m.cfg.Theme != theme.Next(start).Name {
		t.Errorf("theme = %q, want %q", m.cfg.Theme, theme.Next(start).Name)
	}
	if cmd == nil || !strings.Contains(m.notice, m.cfg.Theme) {
		t.Error("expected a notice announcing the new theme")
	}
}

func TestNoticeExpiresOnlyForMatchingID(t *testing.T) {
	m := newTestModel(t)
	m, _ = press(t, m, 'T')
	stale := clearNoticeMsg{id: m.noticeID}
	m, _ = press(t, m, 'T') // newer notice

	m, _ = update(t, m, stale)
	if m.notice == "" {
		t.Fatal("a stale timer cleared a newer notice")
	}
	m, _ = update(t, m, clearNoticeMsg{id: m.noticeID})
	if m.notice != "" {
		t.Fatal("notice was not cleared by its own timer")
	}
}

func TestHelpAndQuitKeys(t *testing.T) {
	m := newTestModel(t)
	m, _ = press(t, m, '?')
	if !m.help.ShowAll {
		t.Error("? should expand help")
	}
	_, cmd := press(t, m, 'q')
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q should quit")
	}
}

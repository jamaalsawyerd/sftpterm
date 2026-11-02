package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	m, err := New(Options{Config: testConfig()})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// loadLocal runs the local pane's initial directory load synchronously.
func loadLocal(t *testing.T, m Model) Model {
	t.Helper()
	msg := m.local.Navigate(t.TempDir(), "")()
	m, _ = update(t, m, msg)
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
				m.help.ShowAll = showAll
				m.setFocus(leftSide) // the pane's full help is the tallest
				m, _ = update(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
				m = loadLocal(t, m)

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
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	before := m.bodyHeight
	m, _ = press(t, m, '?')
	if !m.help.ShowAll {
		t.Error("? should expand help")
	}
	if m.bodyHeight >= before {
		t.Errorf("panes should shrink to make room for full help: %d -> %d", before, m.bodyHeight)
	}
	_, cmd := press(t, m, 'q')
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q should quit")
	}
}

func TestFilteringCapturesGlobalKeys(t *testing.T) {
	m := newTestModel(t)
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = loadLocal(t, m)
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(t, m, '/')
	if !m.local.Filtering() {
		t.Fatal("/ should start filtering")
	}
	start := m.cfg.Theme
	m, cmd := press(t, m, 'q')
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q quit while typing a filter")
		}
	}
	m, _ = press(t, m, 'T')
	if m.cfg.Theme != start {
		t.Error("T changed the theme while typing a filter")
	}
	_, cmd = update(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c should still quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c should still quit")
	}
}

func TestPaneErrorBecomesNotice(t *testing.T) {
	m := newTestModel(t)
	m, cmd := update(t, m, pane.ErrorMsg{Err: os.ErrPermission})
	if cmd == nil || m.noticeKind != noticeError || !strings.Contains(m.notice, "permission") {
		t.Errorf("notice = %q (kind %d)", m.notice, m.noticeKind)
	}
}

// Regression: Init must not depend on state changes that are lost because it
// has a value receiver.
func TestInitLoadsStartDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/hello.txt", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Config: testConfig(), StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	var msgs []tea.Msg
	switch msg := m.Init()().(type) {
	case tea.BatchMsg:
		for _, cmd := range msg {
			msgs = append(msgs, cmd())
		}
	default:
		msgs = append(msgs, msg)
	}
	for _, msg := range msgs {
		m, _ = update(t, m, msg)
	}
	if m.local.Path() != dir {
		t.Fatalf("local pane path = %q, want %q", m.local.Path(), dir)
	}
	if !strings.Contains(m.View().Content, "hello.txt") {
		t.Error("start dir listing not rendered")
	}
}

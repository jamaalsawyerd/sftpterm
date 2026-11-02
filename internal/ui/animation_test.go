package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/transfer"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/fx"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/queue"
)

// testConfig is the default config with animations off, so tests that aren't
// about animation don't have to dismiss the splash first.
func testConfig() config.Config {
	c := config.Default()
	c.Animations = false
	return c
}

func animatedModel(t *testing.T) Model {
	t.Helper()
	m, err := New(Options{Config: config.Default(), StartDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestSplashPlaysThenGetsOutOfTheWay(t *testing.T) {
	m := animatedModel(t)
	if !m.splash || !strings.Contains(ansi.Strip(m.View().Content), "press any key") {
		t.Fatal("splash should show at startup")
	}
	ticks := 0
	for m.splash {
		var cmd tea.Cmd
		m, cmd = update(t, m, animTickMsg{})
		if m.splash && cmd == nil {
			t.Fatal("the animation stopped ticking mid-splash")
		}
		if ticks++; ticks > fx.FPS*5 {
			t.Fatal("splash never ended")
		}
	}
	if strings.Contains(ansi.Strip(m.View().Content), "press any key") {
		t.Error("splash still drawn after it ended")
	}
}

func TestAnyKeySkipsSplashWithoutActing(t *testing.T) {
	m := animatedModel(t)
	m, cmd := press(t, m, 'q')
	if m.splash {
		t.Fatal("a key should dismiss the splash")
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("the key that dismissed the splash also quit the app")
		}
	}
}

func TestFocusBarSlides(t *testing.T) {
	m := animatedModel(t)
	m, _ = press(t, m, 'x') // skip splash
	x0 := m.focusX.Value()
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // right → left
	if !m.animating || !m.focusX.Moving() {
		t.Fatal("switching focus should start the animation")
	}
	m, _ = update(t, m, animTickMsg{})
	mid := m.focusX.Value()
	if mid == x0 || mid == 0 {
		t.Errorf("focus bar should be between positions mid-slide: start %v, now %v", x0, mid)
	}
	for range fx.FPS * 3 {
		m, _ = update(t, m, animTickMsg{})
	}
	if x, _ := m.focusTarget(); m.focusX.Value() != x {
		t.Errorf("focus bar settled at %v, want %v", m.focusX.Value(), x)
	}

	// With animations off it jumps straight there.
	m.cfg.Animations = false
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if x, _ := m.focusTarget(); m.focusX.Value() != x || m.focusX.Moving() {
		t.Error("with animations off the focus bar should jump")
	}
}

func TestConfettiOnlyForWorthwhileBatches(t *testing.T) {
	m := animatedModel(t)
	m, _ = press(t, m, 'x')
	m, _ = update(t, m, queue.IdleMsg{IdleEvent: transfer.IdleEvent{Done: 1, Elapsed: 100 * time.Millisecond}})
	if m.confetti != nil {
		t.Error("a tiny, quick transfer shouldn't throw confetti")
	}
	m, _ = update(t, m, queue.IdleMsg{IdleEvent: transfer.IdleEvent{Done: 2, Failed: 1, Elapsed: time.Minute}})
	if m.confetti != nil {
		t.Error("no confetti when something failed")
	}
	m, cmd := update(t, m, queue.IdleMsg{IdleEvent: transfer.IdleEvent{Done: 3, Bytes: 1 << 30, Elapsed: 30 * time.Second}})
	if m.confetti == nil || cmd == nil {
		t.Fatal("a big successful batch deserves confetti")
	}
	for range fx.FPS * 4 {
		m, _ = update(t, m, animTickMsg{})
		if lines := strings.Split(m.View().Content, "\n"); len(lines) != m.height {
			t.Fatalf("confetti broke the layout: %d lines", len(lines))
		}
	}
	if m.confetti != nil || m.animating {
		t.Error("confetti should end and the ticker stop")
	}
}

func TestBellOnlyWhenEnabled(t *testing.T) {
	long := queue.IdleMsg{IdleEvent: transfer.IdleEvent{Done: 1, Elapsed: time.Minute}}
	rang := func(m Model) bool {
		_, cmd := update(t, m, long)
		return cmdProduces(cmd, func(msg tea.Msg) bool { _, ok := msg.(tea.RawMsg); return ok })
	}
	m := newTestModel(t)
	if rang(m) {
		t.Error("bell rang while disabled")
	}
	m.cfg.Bell = true
	if !rang(m) {
		t.Error("bell didn't ring when enabled")
	}
}

// cmdProduces runs cmd (expanding batches, skipping timers) and reports
// whether any message matches.
func cmdProduces(cmd tea.Cmd, match func(tea.Msg) bool) bool {
	if cmd == nil {
		return false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if cmdProduces(c, match) {
					return true
				}
			}
			return false
		}
		return match(msg)
	case <-time.After(50 * time.Millisecond):
		return false // a timer (e.g. a notice expiry), not what we're looking for
	}
}

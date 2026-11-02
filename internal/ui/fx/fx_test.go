package fx

import (
	"image/color"
	"math/rand/v2"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

func TestSliderSettles(t *testing.T) {
	s := NewSlider()
	s.SetTarget(40)
	frames := 0
	for s.Step() {
		frames++
		if frames > FPS*5 {
			t.Fatal("slider never settled")
		}
	}
	if s.Value() != 40 || s.Moving() {
		t.Errorf("settled at %v (moving %v)", s.Value(), s.Moving())
	}
	if frames < 3 {
		t.Errorf("settled in %d frames; that's a jump, not an animation", frames)
	}
	s.Jump(5)
	if s.Value() != 5 || s.Moving() {
		t.Error("Jump should move immediately")
	}
}

func TestConfettiStaysOnScreenAndEnds(t *testing.T) {
	colors := []color.Color{lipgloss.Color("#ff0000"), lipgloss.Color("#00ff00")}
	c := NewConfetti(80, 24, colors, rand.New(rand.NewPCG(1, 2)))
	frames, peak := 0, 0
	for c.Step() {
		frames++
		cells := c.Cells()
		peak = max(peak, len(cells))
		for _, cell := range cells {
			if cell.X < 0 || cell.X >= 80 || cell.Y < 0 || cell.Y >= 24 {
				t.Fatalf("cell off screen: %+v", cell)
			}
		}
	}
	if frames == 0 || frames > confettiFrames {
		t.Errorf("burst lasted %d frames", frames)
	}
	if peak == 0 {
		t.Error("no confetti was ever visible")
	}
}

func TestSplashFitsAndReveals(t *testing.T) {
	p, _ := theme.Get("charm")
	s := theme.NewStyles(p)
	for _, frame := range []int{0, 5, SplashFrames} {
		out := Splash(s, frame, 60, 16)
		if h := lipgloss.Height(out); h != 16 {
			t.Errorf("frame %d height %d", frame, h)
		}
		for _, l := range strings.Split(out, "\n") {
			if w := lipgloss.Width(l); w > 60 {
				t.Errorf("frame %d line width %d", frame, w)
			}
		}
	}
	if final := ansi.Strip(Splash(s, SplashFrames, 60, 16)); !strings.Contains(final, tagline) || !strings.Contains(final, "┌─┐") {
		t.Errorf("final frame incomplete:\n%s", final)
	}
}

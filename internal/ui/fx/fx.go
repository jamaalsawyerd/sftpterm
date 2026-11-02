// Package fx holds sftpterm's decorative animations: a spring-driven slider,
// confetti, and the splash screen. Nothing here affects behavior; the app can
// turn all of it off.
package fx

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

// FPS is the animation frame rate.
const FPS = 30

// Slider eases a value toward a target with a slightly bouncy spring.
type Slider struct {
	pos, vel, target float64
	spring           harmonica.Spring
}

// NewSlider returns a slider resting at 0.
func NewSlider() Slider {
	return Slider{spring: harmonica.NewSpring(harmonica.FPS(FPS), 9.0, 0.65)}
}

// SetTarget starts moving toward t.
func (s *Slider) SetTarget(t float64) { s.target = t }

// Jump moves to t immediately.
func (s *Slider) Jump(t float64) { s.pos, s.vel, s.target = t, 0, t }

// Step advances one frame and reports whether the slider is still moving.
func (s *Slider) Step() bool {
	s.pos, s.vel = s.spring.Update(s.pos, s.vel, s.target)
	if math.Abs(s.pos-s.target) < 0.05 && math.Abs(s.vel) < 0.05 {
		s.pos, s.vel = s.target, 0
		return false
	}
	return true
}

// Moving reports whether Step would change anything.
func (s Slider) Moving() bool { return s.pos != s.target || s.vel != 0 }

// Value is the current position.
func (s Slider) Value() float64 { return s.pos }

// Cell is one styled character to draw at a screen position.
type Cell = theme.Cell

var confettiGlyphs = []string{"✦", "✶", "✷", "•", "★", "◆", "▪", "✸", "·"}

type particle struct {
	p     *harmonica.Projectile
	glyph string
	color color.Color
}

// Confetti is a burst of particles that arc up and fall under gravity.
type Confetti struct {
	parts  []particle
	frames int
	width  int
	height int
}

// confettiFrames caps how long a burst lasts (about three seconds).
const confettiFrames = FPS * 3

// NewConfetti launches a burst sized for a width×height screen.
func NewConfetti(width, height int, colors []color.Color, rng *rand.Rand) *Confetti {
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // decoration
	}
	n := min(max(width/2, 20), 90)
	c := &Confetti{width: width, height: height}
	gravity := harmonica.Vector{X: 0, Y: 26, Z: 0}
	for range n {
		// Launch from two cannons near the bottom corners, aimed inward.
		left := rng.IntN(2) == 0
		x := 2.0
		vx := 8 + rng.Float64()*float64(width)/2.5
		if !left {
			x = float64(width - 3)
			vx = -vx
		}
		start := harmonica.Point{X: x, Y: float64(height - 2), Z: 0}
		vel := harmonica.Vector{X: vx, Y: -(14 + rng.Float64()*float64(height)*1.4), Z: 0}
		c.parts = append(c.parts, particle{
			p:     harmonica.NewProjectile(harmonica.FPS(FPS), start, vel, gravity),
			glyph: confettiGlyphs[rng.IntN(len(confettiGlyphs))],
			color: colors[rng.IntN(len(colors))],
		})
	}
	return c
}

// Step advances one frame and reports whether the burst is still going.
func (c *Confetti) Step() bool {
	c.frames++
	alive := c.parts[:0]
	for _, pt := range c.parts {
		pos := pt.p.Update()
		if pos.Y < float64(c.height)+1 && pos.X > -2 && pos.X < float64(c.width)+2 {
			alive = append(alive, pt)
		}
	}
	c.parts = alive
	return len(c.parts) > 0 && c.frames < confettiFrames
}

// Cells returns the particles currently on screen.
func (c *Confetti) Cells() []Cell {
	cells := make([]Cell, 0, len(c.parts))
	for _, pt := range c.parts {
		pos := pt.p.Position()
		x, y := int(math.Round(pos.X)), int(math.Round(pos.Y))
		if x < 0 || y < 0 || x >= c.width || y >= c.height {
			continue
		}
		cells = append(cells, Cell{X: x, Y: y, Text: lipgloss.NewStyle().Foreground(pt.color).Render(pt.glyph)})
	}
	return cells
}

// logo is "sftpterm" in a compact box-drawing font.
var logo = []string{
	"┌─┐┌─┐┌┬┐┌─┐┌┬┐┌─┐┬─┐┌┬┐",
	"└─┐├┤  │ ├─┘ │ ├┤ ├┬┘│││",
	"└─┘└   ┴ ┴   ┴ └─┘┴└─┴ ┴",
}

const tagline = "files, but make it fun"

// SplashFrames is how long the splash plays before getting out of the way.
const SplashFrames = FPS * 3 / 2

// Splash renders frame n of the startup animation, centered in w×h.
func Splash(s theme.Styles, frame, w, h int) string {
	p := s.Palette
	// The gradient sweeps across the logo as the frames go by.
	shift := float64(frame) / float64(SplashFrames)
	stops := rotate([]color.Color{p.Primary, p.Accent, p.Secondary, p.Primary}, shift)

	lines := make([]string, 0, len(logo)+4)
	reveal := min(frame*2, len([]rune(logo[0]))) // wipe in from the left
	for _, l := range logo {
		r := []rune(l)
		visible := string(r[:reveal]) + strings.Repeat(" ", len(r)-reveal)
		lines = append(lines, theme.Gradient(lipgloss.NewStyle().Bold(true), visible, stops...))
	}
	typed := min(max(frame-12, 0), len(tagline)) // then type the tagline
	lines = append(lines, "", s.Muted.Render(padRight(tagline[:typed], len(tagline))))

	sparkle := []string{"✦", "✧", "·", " "}[frame/4%4]
	lines = append(lines, "", lipgloss.NewStyle().Foreground(p.Accent).Render(sparkle)+
		s.Subtle.Render("  press any key  ")+lipgloss.NewStyle().Foreground(p.Accent).Render(sparkle))

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, lines...))
}

func rotate(stops []color.Color, by float64) []color.Color {
	if len(stops) < 2 {
		return stops
	}
	blend := lipgloss.Blend1D(24, stops...)
	off := int(by*float64(len(blend))) % len(blend)
	out := append(append([]color.Color{}, blend[off:]...), blend[:off]...)
	return out
}

func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

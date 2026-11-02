package theme

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestFrameExactSize(t *testing.T) {
	p, _ := Get("charm")
	s := NewStyles(p)
	content := "short\n" + strings.Repeat("very long line ", 20) + "\nthird\nfourth\nfifth"
	for _, sz := range [][2]int{{40, 10}, {10, 3}, {5, 4}, {2, 2}, {3, 2}, {80, 2}} {
		for _, focused := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/%v", sz[0], sz[1], focused), func(t *testing.T) {
				out := s.Frame("Local", "/home/someone/a/very/deep/directory/tree", content, sz[0], sz[1], focused)
				if h := lipgloss.Height(out); h != sz[1] {
					t.Errorf("height = %d, want %d", h, sz[1])
				}
				for i, line := range strings.Split(out, "\n") {
					if w := lipgloss.Width(line); w != sz[0] {
						t.Errorf("line %d width = %d, want %d: %q", i, w, sz[0], line)
					}
				}
			})
		}
	}
}

func TestTruncateLeft(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"/home/user", 20, "/home/user"},
		{"/home/user/projects", 10, "…/projects"},
		{"abc", 1, "…"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := TruncateLeft(tt.in, tt.width); got != tt.want {
			t.Errorf("TruncateLeft(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

func TestOverlay(t *testing.T) {
	base := "aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc\ndddddddddd"
	got := Overlay(base, "XX\nYY", 3, 1)
	want := "aaaaaaaaaa\nbbbXXbbbbb\ncccYYccccc\ndddddddddd"
	if plain := ansiStrip(got); plain != want {
		t.Errorf("Overlay =\n%s\nwant\n%s", plain, want)
	}
	// Styled base content keeps its width around the overlay.
	styled := lipgloss.NewStyle().Bold(true).Render("0123456789")
	out := Overlay(styled, "##", 4, 0)
	if w := lipgloss.Width(out); w != 10 || ansiStrip(out) != "0123##6789" {
		t.Errorf("styled overlay = %q (width %d)", ansiStrip(out), w)
	}
	// Rows outside the base are ignored.
	if out := Overlay("ab", "X\nY\nZ", 0, 0); ansiStrip(out) != "Xb" {
		t.Errorf("clipped overlay = %q", ansiStrip(out))
	}
}

func TestOverlayCells(t *testing.T) {
	base := "..........\n..........\n.........."
	out := OverlayCells(base, []Cell{{X: 2, Y: 0, Text: "A"}, {X: 7, Y: 0, Text: "B"}, {X: 9, Y: 2, Text: "C"}, {X: 50, Y: 1, Text: "Z"}, {X: 1, Y: 9, Text: "Z"}})
	want := "..A....B..\n..........\n.........C"
	if got := ansiStrip(out); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Regression: many cells on one styled row used to grow the line
// exponentially, because each splice re-copied every escape code.
func TestOverlayCellsStaysLinear(t *testing.T) {
	p, _ := Get("charm")
	row := Gradient(lipgloss.NewStyle(), strings.Repeat("─", 100), p.Primary, p.Secondary)
	var cells []Cell
	for x := 0; x < 100; x += 3 {
		cells = append(cells, Cell{X: x, Y: 0, Text: "*"})
	}
	out := OverlayCells(row, cells)
	if len(out) > 40*len(row) {
		t.Fatalf("line grew from %d to %d bytes", len(row), len(out))
	}
	if w := lipgloss.Width(out); w != 100 {
		t.Errorf("width = %d, want 100", w)
	}
}

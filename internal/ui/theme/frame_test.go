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

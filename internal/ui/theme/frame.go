package theme

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Frame draws content inside a rounded box of exactly width×height cells,
// with title and subtitle set into the top border:
//
//	╭─ Local ~/Downloads ─────╮
//	│ content                 │
//	╰─────────────────────────╯
//
// Content lines are truncated or padded to fit. A subtitle that doesn't fit is
// shortened from the left, which suits paths.
func (s Styles) Frame(title, subtitle, content string, width, height int, focused bool) string {
	if width < 2 || height < 2 {
		return blank(width, height)
	}
	iw, ih := width-2, height-2
	b := lipgloss.RoundedBorder()

	edgeColor, titleStyle := s.Palette.Border, s.Muted.Bold(true)
	if focused {
		edgeColor, titleStyle = s.Palette.Primary, lipgloss.NewStyle().Foreground(s.Palette.Primary).Bold(true)
	}
	edge := lipgloss.NewStyle().Foreground(edgeColor)

	// Top border, with the label after one dash: "╭─ title subtitle ───╮".
	label := ""
	if title != "" && iw >= 4 {
		t := " " + titleStyle.Render(title) + " "
		if avail := iw - 1 - lipgloss.Width(t) - 1; subtitle != "" && avail >= 2 {
			t += s.Muted.Render(TruncateLeft(subtitle, avail)) + " "
		}
		label = ansi.Truncate(t, iw-1, "")
	}
	var sb strings.Builder
	if label == "" {
		sb.WriteString(edge.Render(b.TopLeft + strings.Repeat(b.Top, iw) + b.TopRight))
	} else {
		fill := iw - 1 - lipgloss.Width(label)
		sb.WriteString(edge.Render(b.TopLeft + b.Top))
		sb.WriteString(label)
		sb.WriteString(edge.Render(strings.Repeat(b.Top, fill) + b.TopRight))
	}

	lines := strings.Split(content, "\n")
	left, right := edge.Render(b.Left), edge.Render(b.Right)
	for i := range ih {
		line := ""
		if i < len(lines) {
			line = Fit(lines[i], iw)
		} else {
			line = strings.Repeat(" ", iw)
		}
		sb.WriteString("\n" + left + line + right)
	}
	sb.WriteString("\n" + edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, iw)+b.BottomRight))
	return sb.String()
}

// Fit truncates or right-pads s to exactly width cells.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	if w := lipgloss.Width(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// TruncateLeft shortens s to width cells by dropping characters from the
// start, marking the cut with "…".
func TruncateLeft(s string, width int) string {
	w := lipgloss.Width(s)
	if w <= width {
		return s
	}
	if width <= 1 {
		return strings.Repeat("…", max(width, 0))
	}
	return "…" + ansi.TruncateLeft(s, w-(width-1), "")
}

func blank(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	line := strings.Repeat(" ", width)
	return strings.TrimSuffix(strings.Repeat(line+"\n", height), "\n")
}

// Overlay draws top over base with its top-left corner at column x, row y,
// keeping base visible around it. Both are multi-line styled strings.
func Overlay(base, top string, x, y int) string {
	lines := strings.Split(base, "\n")
	for i, tl := range strings.Split(top, "\n") {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		bl := lines[row]
		bw := lipgloss.Width(bl)
		if bw < x {
			bl += strings.Repeat(" ", x-bw)
		}
		left := ansi.Truncate(bl, x, "")
		right := ansi.TruncateLeft(bl, x+lipgloss.Width(tl), "")
		lines[row] = left + "\x1b[m" + tl + "\x1b[m" + right
	}
	return strings.Join(lines, "\n")
}

// Cell is one styled snippet to draw at a screen position.
type Cell struct {
	X, Y int
	Text string
}

// OverlayCells draws scattered cells over base, splitting it only once.
// Cells outside base are skipped; later cells win where they overlap.
func OverlayCells(base string, cells []Cell) string {
	if len(cells) == 0 {
		return base
	}
	lines := strings.Split(base, "\n")
	byRow := map[int][]Cell{}
	for _, c := range cells {
		if c.Y >= 0 && c.Y < len(lines) && c.X >= 0 {
			byRow[c.Y] = append(byRow[c.Y], c)
		}
	}
	for row, cs := range byRow {
		// Cut every piece from the original line exactly once. Splicing the
		// already-spliced line repeatedly would duplicate its escape codes
		// on every pass (truncation keeps them), growing exponentially.
		orig := lines[row]
		width := lipgloss.Width(orig)
		slices.SortStableFunc(cs, func(a, b Cell) int { return a.X - b.X })
		var b strings.Builder
		at := 0
		for _, c := range cs {
			w := lipgloss.Width(c.Text)
			if c.X < at || c.X+w > width {
				continue // overlaps the previous cell, or runs off the end
			}
			b.WriteString(ansi.Cut(orig, at, c.X))
			b.WriteString("\x1b[m" + c.Text + "\x1b[m")
			at = c.X + w
		}
		b.WriteString(ansi.TruncateLeft(orig, at, ""))
		lines[row] = b.String()
	}
	return strings.Join(lines, "\n")
}

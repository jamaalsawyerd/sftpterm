package theme

import (
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

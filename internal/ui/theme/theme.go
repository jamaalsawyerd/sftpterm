// Package theme defines sftpterm's color palettes and the Lip Gloss styles
// derived from them. Every color in the UI should come from here.
package theme

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// Palette is a named set of colors.
type Palette struct {
	Name      string
	Primary   color.Color // gradient start, focused borders, key hints
	Secondary color.Color // gradient end
	Accent    color.Color // highlights that should pop
	Text      color.Color
	Muted     color.Color // secondary text
	Subtle    color.Color // separators, disabled things
	Border    color.Color // unfocused borders
	Highlight color.Color // selection and status bar background
	Success   color.Color
	Warning   color.Color
	Error     color.Color
}

func hex(s string) color.Color { return lipgloss.Color(s) }

var palettes = []Palette{
	{
		Name: "charm", Primary: hex("#FF6AC1"), Secondary: hex("#9F7AEA"), Accent: hex("#6EE7F7"),
		Text: hex("#EDE7F6"), Muted: hex("#9A93B0"), Subtle: hex("#4E4866"), Border: hex("#5C5470"),
		Highlight: hex("#2F2645"), Success: hex("#5AF78E"), Warning: hex("#F3F99D"), Error: hex("#FF5C57"),
	},
	{
		Name: "catppuccin-mocha", Primary: hex("#CBA6F7"), Secondary: hex("#89B4FA"), Accent: hex("#94E2D5"),
		Text: hex("#CDD6F4"), Muted: hex("#7F849C"), Subtle: hex("#45475A"), Border: hex("#585B70"),
		Highlight: hex("#313244"), Success: hex("#A6E3A1"), Warning: hex("#F9E2AF"), Error: hex("#F38BA8"),
	},
	{
		Name: "catppuccin-latte", Primary: hex("#8839EF"), Secondary: hex("#1E66F5"), Accent: hex("#179299"),
		Text: hex("#4C4F69"), Muted: hex("#8C8FA1"), Subtle: hex("#BCC0CC"), Border: hex("#ACB0BE"),
		Highlight: hex("#CCD0DA"), Success: hex("#40A02B"), Warning: hex("#DF8E1D"), Error: hex("#D20F39"),
	},
	{
		Name: "dracula", Primary: hex("#BD93F9"), Secondary: hex("#FF79C6"), Accent: hex("#8BE9FD"),
		Text: hex("#F8F8F2"), Muted: hex("#6272A4"), Subtle: hex("#44475A"), Border: hex("#6272A4"),
		Highlight: hex("#44475A"), Success: hex("#50FA7B"), Warning: hex("#F1FA8C"), Error: hex("#FF5555"),
	},
	{
		Name: "tokyo-night", Primary: hex("#7AA2F7"), Secondary: hex("#BB9AF7"), Accent: hex("#7DCFFF"),
		Text: hex("#C0CAF5"), Muted: hex("#565F89"), Subtle: hex("#3B4261"), Border: hex("#414868"),
		Highlight: hex("#283457"), Success: hex("#9ECE6A"), Warning: hex("#E0AF68"), Error: hex("#F7768E"),
	},
	{
		Name: "nord", Primary: hex("#88C0D0"), Secondary: hex("#B48EAD"), Accent: hex("#8FBCBB"),
		Text: hex("#ECEFF4"), Muted: hex("#7B88A1"), Subtle: hex("#3B4252"), Border: hex("#4C566A"),
		Highlight: hex("#3B4252"), Success: hex("#A3BE8C"), Warning: hex("#EBCB8B"), Error: hex("#BF616A"),
	},
}

// Names lists the built-in palettes in display order.
func Names() []string {
	names := make([]string, len(palettes))
	for i, p := range palettes {
		names[i] = p.Name
	}
	return names
}

// Get returns the palette with the given name.
func Get(name string) (Palette, bool) {
	for _, p := range palettes {
		if p.Name == name {
			return p, true
		}
	}
	return Palette{}, false
}

// Next returns the palette after the named one, wrapping around.
func Next(name string) Palette {
	for i, p := range palettes {
		if p.Name == name {
			return palettes[(i+1)%len(palettes)]
		}
	}
	return palettes[0]
}

// Styles are the Lip Gloss styles derived from a palette.
type Styles struct {
	Palette Palette

	Text    lipgloss.Style
	Muted   lipgloss.Style
	Subtle  lipgloss.Style
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style

	Card lipgloss.Style

	StatusBar     lipgloss.Style
	StatusAccent  lipgloss.Style
	StatusSegment lipgloss.Style

	Help help.Styles
}

// NewStyles builds the style set for a palette.
func NewStyles(p Palette) Styles {
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	bar := lipgloss.NewStyle().Background(p.Highlight).Foreground(p.Text)

	return Styles{
		Palette: p,
		Text:    fg(p.Text),
		Muted:   fg(p.Muted),
		Subtle:  fg(p.Subtle),
		Success: fg(p.Success),
		Warning: fg(p.Warning),
		Error:   fg(p.Error),

		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForegroundBlend(p.Primary, p.Secondary).
			Padding(1, 4),

		StatusBar:     bar,
		StatusAccent:  lipgloss.NewStyle().Background(p.Primary).Foreground(p.Highlight).Bold(true).Padding(0, 1),
		StatusSegment: bar.Padding(0, 1),

		Help: help.Styles{
			ShortKey:       fg(p.Primary),
			ShortDesc:      fg(p.Muted),
			ShortSeparator: fg(p.Subtle),
			Ellipsis:       fg(p.Subtle),
			FullKey:        fg(p.Primary),
			FullDesc:       fg(p.Muted),
			FullSeparator:  fg(p.Subtle),
		},
	}
}

// Gradient renders s with each character colored along a blend of stops,
// applying base to every character (e.g. for bold).
func Gradient(base lipgloss.Style, s string, stops ...color.Color) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	colors := lipgloss.Blend1D(len(runes), stops...)
	if len(colors) < len(runes) {
		return base.Render(s)
	}
	var b strings.Builder
	for i, r := range runes {
		b.WriteString(base.Foreground(colors[i]).Render(string(r)))
	}
	return b.String()
}

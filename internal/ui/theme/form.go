package theme

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// FormTheme adapts the palette for huh forms.
func (s Styles) FormTheme() huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles {
		p := s.Palette
		t := huh.ThemeBase(true)
		t.Focused.Base = t.Focused.Base.BorderForeground(p.Primary)
		t.Focused.Title = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)
		t.Focused.Description = lipgloss.NewStyle().Foreground(p.Muted)
		t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(p.Error).SetString(" *")
		t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(p.Error)
		t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(p.Accent)
		t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(p.Subtle)
		t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(p.Secondary)
		t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(p.Text)
		t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(p.Highlight).Background(p.Primary).Padding(0, 2)
		t.Focused.BlurredButton = lipgloss.NewStyle().Foreground(p.Muted).Background(p.Highlight).Padding(0, 2)
		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Title = lipgloss.NewStyle().Foreground(p.Muted)
		t.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(p.Subtle)
		t.Group.Title = t.Focused.Title
		t.Help = s.Help
		return t
	})
}

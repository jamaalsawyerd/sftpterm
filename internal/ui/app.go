// Package ui contains the Bubble Tea models that make up the sftpterm interface.
package ui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#B388FF")).
			Padding(1, 4)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF79C6"))
	hintStyle  = lipgloss.NewStyle().Faint(true)
)

type model struct {
	width, height int
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	body := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render("✦ sftpterm"),
		"",
		"Work in progress. Check back soon!",
		"",
		hintStyle.Render("press q to quit"),
	)
	content := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, cardStyle.Render(body))

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "sftpterm"
	return v
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context) error {
	_, err := tea.NewProgram(model{}, tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

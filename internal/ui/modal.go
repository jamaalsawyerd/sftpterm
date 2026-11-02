package ui

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

// confirmDialog asks a yes/no question before something destructive, like
// quitting with transfers running.
type confirmDialog struct {
	title, body string
	yes         func(Model) (tea.Model, tea.Cmd)
}

func (d *confirmDialog) view(s theme.Styles, width int) string {
	p := s.Palette
	inner := max(min(width, 56)-6, 20)
	content := strings.Join([]string{
		theme.Gradient(lipgloss.NewStyle().Bold(true), d.title, p.Warning, p.Primary),
		"",
		lipgloss.NewStyle().Width(inner).Foreground(p.Text).Render(d.body),
		"",
		lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("y") + s.Text.Render(" yes   ") +
			lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("n") + s.Text.Render(" no"),
	}, "\n")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForegroundBlend(p.Warning, p.Primary).
		Padding(1, 2).Render(content)
}

var confirmHelp = []key.Binding{
	key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n/esc", "no")),
}

// Where settings from the form go.
const (
	scopeSession = "session"
	scopeDefault = "default"
	scopeHost    = "host"
)

// settingsForm edits appearance and transfer settings.
type settingsForm struct {
	form *huh.Form
	vals *settingsValues
	host string // alias of the current connection, if any
}

type settingsValues struct {
	theme, icons                  string
	showHidden                    bool
	concurrency, upload, download string
	scope                         string
}

type settingsSubmittedMsg struct{}

type settingsCancelledMsg struct{}

func newSettingsForm(cfg config.Config, t config.Transfers, host string, s theme.Styles) *settingsForm {
	v := &settingsValues{
		theme: cfg.Theme, icons: cfg.Icons, showHidden: cfg.ShowHidden,
		concurrency: strconv.Itoa(t.Concurrency),
		upload:      t.UploadLimit.String(),
		download:    t.DownloadLimit.String(),
		scope:       scopeSession,
	}
	rate := func(s string) error { _, err := units.ParseRate(s); return err }
	scopes := []huh.Option[string]{
		huh.NewOption("Just for this session", scopeSession),
		huh.NewOption("Save as my defaults", scopeDefault),
	}
	if host != "" {
		scopes = append(scopes, huh.NewOption("Save; transfer settings only for "+host, scopeHost))
	}
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Theme").Options(huh.NewOptions(theme.Names()...)...).Value(&v.theme),
			huh.NewSelect[string]().Title("Icons").Options(huh.NewOptions(config.IconSets...)...).Value(&v.icons),
			huh.NewConfirm().Title("Show hidden files?").Affirmative("Show").Negative("Hide").Value(&v.showHidden),
		).Title("Appearance"),
		huh.NewGroup(
			huh.NewInput().Title("Simultaneous transfers").Description("1–16").Value(&v.concurrency).
				Validate(func(s string) error {
					n, err := strconv.Atoi(strings.TrimSpace(s))
					if err != nil || n < config.MinConcurrency || n > config.MaxConcurrency {
						return errors.New("enter a number from 1 to 16")
					}
					return nil
				}),
			huh.NewInput().Title("Upload limit").Description(`e.g. 10MB/s, 500K, or "unlimited"`).Value(&v.upload).Validate(rate),
			huh.NewInput().Title("Download limit").Description(`e.g. 10MB/s, 500K, or "unlimited"`).Value(&v.download).Validate(rate),
		).Title("Transfers"),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Apply").Options(scopes...).Value(&v.scope),
		),
	).WithTheme(s.FormTheme()).WithKeyMap(keys).WithWidth(48).WithShowHelp(true)
	f.SubmitCmd = func() tea.Msg { return settingsSubmittedMsg{} }
	f.CancelCmd = func() tea.Msg { return settingsCancelledMsg{} }
	return &settingsForm{form: f, vals: v, host: host}
}

// transfers parses the submitted transfer settings; validation already ran.
func (f *settingsForm) transfers() config.Transfers {
	n, _ := strconv.Atoi(strings.TrimSpace(f.vals.concurrency))
	up, _ := units.ParseRate(f.vals.upload)
	down, _ := units.ParseRate(f.vals.download)
	return config.Transfers{Concurrency: n, UploadLimit: up, DownloadLimit: down}
}

func (f *settingsForm) view(s theme.Styles) string {
	p := s.Palette
	title := theme.Gradient(lipgloss.NewStyle().Bold(true), "Settings", p.Primary, p.Secondary)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForegroundBlend(p.Primary, p.Secondary).
		Padding(1, 2).Render(title + "\n\n" + f.form.View())
}

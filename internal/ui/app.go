// Package ui contains the Bubble Tea models that make up the sftpterm interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

// The smallest terminal we try to draw a full layout in.
const (
	minWidth  = 50
	minHeight = 12
)

const (
	noticeTTL        = 3 * time.Second
	startupNoticeTTL = 8 * time.Second
)

// Options configure a new Model.
type Options struct {
	Config config.Config
	// Warnings are shown in the status bar at startup, e.g. unknown config keys.
	Warnings []string
}

type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeWarn
)

// clearNoticeMsg expires the status bar notice with the matching id, so an
// older timer can't clear a newer notice.
type clearNoticeMsg struct{ id int }

// Model is the root Bubble Tea model.
type Model struct {
	cfg    config.Config
	styles theme.Styles
	keys   keyMap
	help   help.Model

	width, height int

	notice     string
	noticeKind noticeKind
	noticeID   int
}

// New builds the root model.
func New(opts Options) (Model, error) {
	p, ok := theme.Get(opts.Config.Theme)
	if !ok {
		return Model{}, fmt.Errorf("unknown theme %q (available: %s)",
			opts.Config.Theme, strings.Join(theme.Names(), ", "))
	}
	m := Model{
		cfg:  opts.Config,
		keys: defaultKeys(),
		help: help.New(),
	}
	m.setPalette(p)
	if len(opts.Warnings) > 0 {
		m.notice = strings.Join(opts.Warnings, "; ")
		m.noticeKind = noticeWarn
	}
	return m, nil
}

func (m *Model) setPalette(p theme.Palette) {
	m.cfg.Theme = p.Name
	m.styles = theme.NewStyles(p)
	m.help.Styles = m.styles.Help
}

// setNotice shows a transient message in the status bar.
func (m *Model) setNotice(kind noticeKind, text string, ttl time.Duration) tea.Cmd {
	m.noticeID++
	m.notice, m.noticeKind = text, kind
	id := m.noticeID
	return tea.Tick(ttl, func(time.Time) tea.Msg { return clearNoticeMsg{id: id} })
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.notice == "" {
		return nil
	}
	id := m.noticeID
	return tea.Tick(startupNoticeTTL, func(time.Time) tea.Msg { return clearNoticeMsg{id: id} })
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width - 2)

	case clearNoticeMsg:
		if msg.id == m.noticeID {
			m.notice = ""
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Theme):
			m.setPalette(theme.Next(m.cfg.Theme))
			log.Debug("theme changed", "theme", m.cfg.Theme)
			return m, m.setNotice(noticeInfo, "theme → "+m.cfg.Theme, noticeTTL)
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "sftpterm"
	return v
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // waiting for the first WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return m.tooSmallView()
	}

	header := m.headerView()
	footer := lipgloss.JoinVertical(lipgloss.Left, m.statusView(), m.helpView())
	bodyHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 0)
	welcome := m.welcomeView()
	if w, h := lipgloss.Size(welcome); w > m.width || h > bodyHeight {
		welcome = m.compactWelcomeView()
	}
	welcome = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(bodyHeight).Render(welcome)
	body := lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, welcome)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) headerView() string {
	p := m.styles.Palette
	title := theme.Gradient(lipgloss.NewStyle().Bold(true), "✦ sftpterm", p.Primary, p.Secondary)
	conn := m.styles.Muted.Render("○ not connected")

	ruleWidth := max(m.width-lipgloss.Width(title)-lipgloss.Width(conn)-4, 0)
	rule := theme.Gradient(lipgloss.NewStyle(), strings.Repeat("─", ruleWidth), p.Secondary, p.Primary)

	// A blank line separates the header from the body.
	return " " + title + " " + rule + " " + conn + " \n"
}

func (m Model) welcomeView() string {
	p := m.styles.Palette
	logo := theme.Gradient(lipgloss.NewStyle().Bold(true), "✦  s f t p t e r m  ✦", p.Primary, p.Accent, p.Secondary)
	return m.styles.Card.Render(lipgloss.JoinVertical(lipgloss.Center,
		logo,
		"",
		m.styles.Text.Render("files, but make it fun"),
		"",
		m.styles.Muted.Render("Connections are on the way."),
		m.styles.Muted.Render("Press ")+m.styles.Help.ShortKey.Render("T")+m.styles.Muted.Render(" to try a different theme."),
	))
}

func (m Model) compactWelcomeView() string {
	p := m.styles.Palette
	return lipgloss.JoinVertical(lipgloss.Center,
		theme.Gradient(lipgloss.NewStyle().Bold(true), "✦ sftpterm ✦", p.Primary, p.Accent, p.Secondary),
		m.styles.Muted.Render("files, but make it fun"),
	)
}

func (m Model) statusView() string {
	t := m.cfg.Transfers
	left := lipgloss.JoinHorizontal(lipgloss.Top,
		m.styles.StatusAccent.Render(m.cfg.Theme),
		m.styles.StatusSegment.Render("⇅ "+strconv.Itoa(t.Concurrency)+" at once"),
		m.styles.StatusSegment.Render("↑ "+t.UploadLimit.String()+"  ↓ "+t.DownloadLimit.String()),
	)

	right := ""
	if m.notice != "" {
		avail := max(m.width-lipgloss.Width(left)-3, 0)
		text := ansi.Truncate(m.notice, avail, "…")
		style := m.styles.StatusSegment.Foreground(m.styles.Palette.Accent)
		if m.noticeKind == noticeWarn {
			style = m.styles.StatusSegment.Foreground(m.styles.Palette.Warning)
		}
		right = style.Render(text)
	}

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return left + m.styles.StatusBar.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) helpView() string {
	return lipgloss.NewStyle().Padding(0, 1).Render(m.help.View(m.keys))
}

func (m Model) tooSmallView() string {
	msg := lipgloss.JoinVertical(lipgloss.Center,
		m.styles.Warning.Bold(true).Render("Terminal too small"),
		m.styles.Muted.Render(fmt.Sprintf("need %d×%d, have %d×%d", minWidth, minHeight, m.width, m.height)),
	)
	msg = lipgloss.NewStyle().MaxWidth(m.width).Render(msg)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, opts Options) error {
	m, err := New(opts)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

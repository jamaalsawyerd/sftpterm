// Package ui contains the Bubble Tea models that make up the sftpterm interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// The smallest terminal we try to draw a full layout in.
const (
	minWidth  = 50
	minHeight = 12
)

const (
	noticeTTL        = 3 * time.Second
	errorNoticeTTL   = 6 * time.Second
	startupNoticeTTL = 8 * time.Second
)

// headerHeight is the title line plus a blank line.
const headerHeight = 2

// Options configure a new Model.
type Options struct {
	Config config.Config
	// Warnings are shown in the status bar at startup, e.g. unknown config keys.
	Warnings []string
	// StartDir is the local directory to open; defaults to the working directory.
	StartDir string
}

type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeWarn
	noticeError
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

	local pane.Model
	// initCmd loads the first directory. It is created in New because Init
	// has a value receiver: state changed there (like a pane's load
	// sequence number) would be lost.
	initCmd tea.Cmd

	width, height int
	bodyHeight    int

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
	startDir := opts.StartDir
	if startDir == "" {
		if wd, err := os.Getwd(); err == nil {
			startDir = wd
		} else if home, err := os.UserHomeDir(); err == nil {
			startDir = home
		}
	}

	m := Model{
		cfg:  opts.Config,
		keys: defaultKeys(),
		help: help.New(),
		local: pane.New(vfs.NewLocal(), pane.Options{
			ShowHidden: opts.Config.ShowHidden,
			Icons:      opts.Config.Icons,
		}),
	}
	m.local.SetFocused(true)
	m.initCmd = m.local.Navigate(startDir, "")
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
	m.local.SetStyles(m.styles)
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
	cmds := []tea.Cmd{m.initCmd}
	if m.notice != "" {
		id := m.noticeID
		cmds = append(cmds, tea.Tick(startupNoticeTTL, func(time.Time) tea.Msg { return clearNoticeMsg{id: id} }))
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width - 2)
		m.layout()
		return m, nil

	case clearNoticeMsg:
		if msg.id == m.noticeID {
			m.notice = ""
		}
		return m, nil

	case pane.ErrorMsg:
		log.Debug("pane error", "err", msg.Err)
		return m, m.setNotice(noticeError, msg.Err.Error(), errorNoticeTTL)

	case tea.KeyPressMsg:
		// While a filter is being typed, every key except ctrl+c belongs to it.
		if msg.String() == "ctrl+c" || !m.local.Filtering() {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Help):
				m.help.ShowAll = !m.help.ShowAll
				m.layout()
				return m, nil
			case key.Matches(msg, m.keys.Theme):
				m.setPalette(theme.Next(m.cfg.Theme))
				log.Debug("theme changed", "theme", m.cfg.Theme)
				return m, m.setNotice(noticeInfo, "theme → "+m.cfg.Theme, noticeTTL)
			}
		}
	}

	var cmd tea.Cmd
	m.local, cmd = m.local.Update(msg)
	m.layout() // the help footer changes height when filtering starts or stops
	return m, cmd
}

// layout distributes the terminal between header, panes, and footer.
func (m *Model) layout() {
	footerHeight := 1 + lipgloss.Height(m.helpView())
	m.bodyHeight = max(m.height-headerHeight-footerHeight, 0)
	m.local.SetSize(m.width/2, m.bodyHeight)
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

	body := lipgloss.JoinHorizontal(lipgloss.Top, m.local.View(), m.remoteView(m.width-m.width/2, m.bodyHeight))
	return lipgloss.JoinVertical(lipgloss.Left, m.headerView(), body, m.statusView(), m.helpView())
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

// remoteView is a placeholder until connections exist.
func (m Model) remoteView(width, height int) string {
	p := m.styles.Palette
	inner := lipgloss.JoinVertical(lipgloss.Center,
		theme.Gradient(lipgloss.NewStyle().Bold(true), "✦ sftpterm ✦", p.Primary, p.Accent, p.Secondary),
		m.styles.Muted.Render("files, but make it fun"),
		"",
		m.styles.Subtle.Render("remote connections are on the way"),
	)
	content := lipgloss.Place(max(width-2, 0), max(height-2, 0), lipgloss.Center, lipgloss.Center, inner)
	return m.styles.Frame("Remote", "not connected", content, width, height, false)
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
		switch m.noticeKind {
		case noticeWarn:
			style = m.styles.StatusSegment.Foreground(m.styles.Palette.Warning)
		case noticeError:
			style = m.styles.StatusSegment.Foreground(m.styles.Palette.Error)
		case noticeInfo:
		}
		right = style.Render(text)
	}

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return left + m.styles.StatusBar.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) helpView() string {
	keys := helpKeys{app: m.keys, pane: m.local.KeyMap(), filtering: m.local.Filtering()}
	// MaxWidth guards against help.ShortHelpView overflowing when its "…"
	// tail doesn't fit (bubbles v2.2.1).
	return lipgloss.NewStyle().Padding(0, 1).MaxWidth(m.width).Render(m.help.View(keys))
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

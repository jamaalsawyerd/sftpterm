// Package ui contains the Bubble Tea models that make up the sftpterm interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
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
	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/connect"
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
	// SSHConfig supplies hosts for the connection screen; nil means none.
	SSHConfig *sshx.SSHConfig
	// Destination, if set, is connected to at startup.
	Destination *sshx.Destination
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

// disconnectedMsg reports that a connection ended, by request or not.
type disconnectedMsg struct {
	conn *sshx.Conn
	err  error
}

type side int

const (
	leftSide side = iota
	rightSide
)

// Model is the root Bubble Tea model.
type Model struct {
	cfg    config.Config
	styles theme.Styles
	keys   keyMap
	help   help.Model

	local   pane.Model
	connect connect.Model
	remote  pane.Model
	conn    *sshx.Conn // nil when disconnected
	focus   side

	// initCmd holds the startup work. It is created in New because Init has
	// a value receiver: state changed there (like a pane's load sequence
	// number) would be lost.
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
	sshcfg := opts.SSHConfig
	if sshcfg == nil {
		sshcfg = sshx.EmptySSHConfig()
	}
	styles := theme.NewStyles(p)

	m := Model{
		cfg:     opts.Config,
		keys:    defaultKeys(),
		help:    help.New(),
		local:   pane.New(vfs.NewLocal(), paneOptions(opts.Config, styles)),
		connect: connect.New(sshcfg, styles),
	}
	m.setPalette(p)
	m.setFocus(rightSide)

	cmds := []tea.Cmd{m.local.Navigate(startDir, "")}
	if opts.Destination != nil {
		cmds = append(cmds, m.connect.Connect(sshcfg.TargetFor(*opts.Destination)))
	}
	if len(opts.Warnings) > 0 {
		m.notice = strings.Join(opts.Warnings, "; ")
		m.noticeKind = noticeWarn
		id := m.noticeID
		cmds = append(cmds, tea.Tick(startupNoticeTTL, func(time.Time) tea.Msg { return clearNoticeMsg{id: id} }))
	}
	m.initCmd = tea.Batch(cmds...)
	return m, nil
}

func paneOptions(cfg config.Config, s theme.Styles) pane.Options {
	return pane.Options{ShowHidden: cfg.ShowHidden, Icons: cfg.Icons, Styles: s}
}

func (m *Model) setPalette(p theme.Palette) {
	m.cfg.Theme = p.Name
	m.styles = theme.NewStyles(p)
	m.help.Styles = m.styles.Help
	m.local.SetStyles(m.styles)
	m.remote.SetStyles(m.styles)
	m.connect.SetStyles(m.styles)
}

func (m *Model) setFocus(s side) {
	m.focus = s
	m.local.SetFocused(s == leftSide)
	m.remote.SetFocused(s == rightSide && m.conn != nil)
	m.connect.SetFocused(s == rightSide && m.conn == nil)
}

// setNotice shows a transient message in the status bar.
func (m *Model) setNotice(kind noticeKind, text string, ttl time.Duration) tea.Cmd {
	m.noticeID++
	m.notice, m.noticeKind = text, kind
	id := m.noticeID
	return tea.Tick(ttl, func(time.Time) tea.Msg { return clearNoticeMsg{id: id} })
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return m.initCmd }

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

	case connect.ConnectedMsg:
		return m.onConnected(msg.Conn)

	case disconnectedMsg:
		if msg.conn != m.conn {
			return m, nil // a connection we already let go of
		}
		label := m.conn.Target.String()
		log.Debug("connection lost", "target", label, "err", msg.err)
		m.dropConnection()
		return m, m.setNotice(noticeError, "connection to "+label+" lost", errorNoticeTTL)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m.broadcast(msg)
}

// broadcast delivers a non-key message to every child. Each one ignores
// messages that aren't addressed to it.
func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds [3]tea.Cmd
	m.local, cmds[0] = m.local.Update(msg)
	if m.conn != nil {
		m.remote, cmds[1] = m.remote.Update(msg)
	}
	m.connect, cmds[2] = m.connect.Update(msg)
	m.layout()
	return m, tea.Batch(cmds[:]...)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	// Prompts are modal.
	if m.connect.HasDialog() {
		var cmd tea.Cmd
		m.connect, cmd = m.connect.Update(msg)
		return m, cmd
	}

	if !m.focusedCapturesKeys() {
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m.quit()
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Theme):
			m.setPalette(theme.Next(m.cfg.Theme))
			log.Debug("theme changed", "theme", m.cfg.Theme)
			return m, m.setNotice(noticeInfo, "theme → "+m.cfg.Theme, noticeTTL)
		case key.Matches(msg, m.keys.SwitchPane):
			m.setFocus(1 - m.focus)
			return m, nil
		case key.Matches(msg, m.keys.Disconnect) && m.conn != nil:
			return m.disconnect()
		}
	}

	var cmd tea.Cmd
	switch {
	case m.focus == leftSide:
		m.local, cmd = m.local.Update(msg)
	case m.conn != nil:
		m.remote, cmd = m.remote.Update(msg)
	default:
		m.connect, cmd = m.connect.Update(msg)
	}
	m.layout() // the help footer changes height when filtering starts or stops
	return m, cmd
}

// focusedCapturesKeys reports whether the focused component is taking text
// input, so global single-letter keys must not fire.
func (m Model) focusedCapturesKeys() bool {
	switch {
	case m.focus == leftSide:
		return m.local.Filtering()
	case m.conn != nil:
		return m.remote.Filtering()
	default:
		return m.connect.CapturesKeys()
	}
}

func (m Model) onConnected(conn *sshx.Conn) (tea.Model, tea.Cmd) {
	label := conn.Target.String()
	fsys, err := vfs.NewSFTP(conn.SFTP, label)
	if err != nil {
		_ = conn.Close()
		return m, m.setNotice(noticeError, err.Error(), errorNoticeTTL)
	}
	if m.conn != nil { // shouldn't happen, but never leak a connection
		_ = m.conn.Close()
	}
	m.conn = conn
	m.remote = pane.New(fsys, paneOptions(m.cfg, m.styles))

	home, _ := fsys.Home()
	start := conn.Target.Path
	switch {
	case start == "":
		start = home
	case !strings.HasPrefix(start, "/"):
		start = path.Join(home, start)
	}
	load := m.remote.Navigate(start, "")
	m.setFocus(rightSide)
	m.layout()
	log.Debug("connected", "target", label, "start", start)

	wait := func() tea.Msg { return disconnectedMsg{conn: conn, err: conn.Wait()} }
	return m, tea.Batch(load, wait, m.setNotice(noticeInfo, "connected to "+label, noticeTTL))
}

func (m Model) disconnect() (tea.Model, tea.Cmd) {
	conn := m.conn
	label := conn.Target.String()
	m.dropConnection()
	closeConn := func() tea.Msg { _ = conn.Close(); return nil }
	return m, tea.Batch(closeConn, m.setNotice(noticeInfo, "disconnected from "+label, noticeTTL))
}

func (m *Model) dropConnection() {
	m.conn = nil
	m.remote = pane.Model{}
	m.setFocus(m.focus)
	m.layout()
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.connect.Close()
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
	return m, tea.Quit
}

// layout distributes the terminal between header, panes, and footer.
func (m *Model) layout() {
	footerHeight := 1 + lipgloss.Height(m.helpView())
	m.bodyHeight = max(m.height-headerHeight-footerHeight, 0)
	lw := m.width / 2
	m.local.SetSize(lw, m.bodyHeight)
	m.remote.SetSize(m.width-lw, m.bodyHeight)
	m.connect.SetSize(m.width-lw, m.bodyHeight)
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "sftpterm"
	if m.conn != nil {
		v.WindowTitle = "sftpterm · " + m.conn.Target.String()
	}
	return v
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // waiting for the first WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return m.tooSmallView()
	}

	right := m.connect.View()
	if m.conn != nil {
		right = m.remote.View()
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.local.View(), right)
	screen := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), body, m.statusView(), m.helpView())

	if d := m.connect.Dialog(m.width); d != "" {
		dw, dh := lipgloss.Size(d)
		screen = theme.Overlay(screen, d, max((m.width-dw)/2, 0), max((m.height-dh)/2, 0))
	}
	return screen
}

func (m Model) headerView() string {
	p := m.styles.Palette
	title := theme.Gradient(lipgloss.NewStyle().Bold(true), "✦ sftpterm", p.Primary, p.Secondary)

	var conn string
	switch {
	case m.conn != nil:
		conn = m.styles.Success.Render("● " + m.conn.Target.String())
	case m.connect.Connecting():
		conn = m.styles.Warning.Render("◌ connecting to " + m.connect.Target().Alias + "…")
	default:
		conn = m.styles.Muted.Render("○ not connected")
	}
	conn = ansi.Truncate(conn, max(m.width/2, 10), "…")

	ruleWidth := max(m.width-lipgloss.Width(title)-lipgloss.Width(conn)-4, 0)
	rule := theme.Gradient(lipgloss.NewStyle(), strings.Repeat("─", ruleWidth), p.Secondary, p.Primary)

	// A blank line separates the header from the body.
	return " " + title + " " + rule + " " + conn + " \n"
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
	// MaxWidth guards against help.ShortHelpView overflowing when its "…"
	// tail doesn't fit (bubbles v2.2.1).
	return lipgloss.NewStyle().Padding(0, 1).MaxWidth(m.width).Render(m.help.View(m.helpKeys()))
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
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if fm, ok := final.(Model); ok && fm.conn != nil {
		_ = fm.conn.Close()
	}
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

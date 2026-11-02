// Package ui contains the Bubble Tea models that make up the sftpterm interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/state"
	"github.com/jamaalsawyerd/sftpterm/internal/transfer"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/connect"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/fx"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/queue"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
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
	// ConfigPath is where settings are saved; empty disables saving.
	ConfigPath string
	// State holds bookmarks and last locations; nil means don't remember.
	State *state.Store
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
	queueSide
)

// Model is the root Bubble Tea model.
type Model struct {
	cfg     config.Config // effective settings for this session
	fileCfg config.Config // what's in the config file
	cfgPath string
	store   *state.Store
	// transfers are the settings in effect now: the session's, or the
	// connected host's overrides.
	transfers config.Transfers
	styles    theme.Styles
	keys      keyMap
	help      help.Model

	local   pane.Model
	connect connect.Model
	remote  pane.Model
	conn    *sshx.Conn // nil when disconnected
	engine  *transfer.Engine
	queue   queue.Model
	focus   side

	confirm   *confirmDialog
	settings  *settingsForm
	input     *inputDialog
	preview   *previewDialog
	bookmarks *bookmarkPicker

	// initCmd holds the startup work. It is created in New because Init has
	// a value receiver: state changed there (like a pane's load sequence
	// number) would be lost.
	initCmd tea.Cmd

	width, height int
	bodyHeight    int

	// Animation state. animating is true while a frame tick is pending, so
	// only one tick chain ever runs.
	animating bool
	splash    bool
	frame     int
	focusX    fx.Slider
	focusW    fx.Slider
	confetti  *fx.Confetti

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
		cfg:       opts.Config,
		fileCfg:   opts.Config,
		cfgPath:   opts.ConfigPath,
		store:     opts.State,
		transfers: opts.Config.Transfers,
		keys:      defaultKeys(),
		help:      help.New(),
		local:     pane.New(vfs.NewLocal(), paneOptions(opts.Config, styles)),
		connect:   connect.New(sshcfg, opts.State, styles),
		focusX:    fx.NewSlider(),
		focusW:    fx.NewSlider(),
		splash:    opts.Config.Animations,
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
	if m.splash {
		m.animating = true
		cmds = append(cmds, animTick())
	}
	m.initCmd = tea.Batch(cmds...)
	return m, nil
}

// animTickMsg advances every running animation by one frame.
type animTickMsg struct{}

func animTick() tea.Cmd {
	return tea.Tick(time.Second/fx.FPS, func(time.Time) tea.Msg { return animTickMsg{} })
}

// animationsRunning reports whether any animation needs another frame.
func (m Model) animationsRunning() bool {
	return m.splash || m.confetti != nil || m.focusX.Moving() || m.focusW.Moving()
}

func (m Model) stepAnimations() (tea.Model, tea.Cmd) {
	m.animating = false
	if m.splash {
		m.frame++
		if m.frame >= fx.SplashFrames+fx.FPS/2 { // hold the final frame briefly
			m.splash = false
		}
	}
	m.focusX.Step()
	m.focusW.Step()
	if m.confetti != nil && !m.confetti.Step() {
		m.confetti = nil
	}
	return m, nil // Update restarts the tick if anything is still moving
}

// focusTarget is where the focus bar should sit for the current focus.
func (m Model) focusTarget() (x, w float64) {
	lw := m.width / 2
	switch m.focus {
	case rightSide:
		return float64(lw), float64(m.width - lw)
	case queueSide:
		return 0, float64(m.width)
	case leftSide:
	}
	return 0, float64(lw)
}

// aimFocusBar moves the focus bar to the focused side, animating if enabled.
func (m *Model) aimFocusBar(animate bool) {
	x, w := m.focusTarget()
	if animate && m.cfg.Animations {
		m.focusX.SetTarget(x)
		m.focusW.SetTarget(w)
		return
	}
	m.focusX.Jump(x)
	m.focusW.Jump(w)
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
	m.queue.SetStyles(m.styles)
}

func (m *Model) setFocus(s side) {
	if s == queueSide && !m.queue.HasItems() {
		s = leftSide
	}
	changed := m.focus != s
	m.focus = s
	if changed {
		m.aimFocusBar(true)
	}
	m.local.SetFocused(s == leftSide)
	m.remote.SetFocused(s == rightSide && m.conn != nil)
	m.connect.SetFocused(s == rightSide && m.conn == nil)
	m.queue.SetFocused(s == queueSide)
}

// nextFocus cycles local → remote → queue (when it has items).
func (m Model) nextFocus() side {
	switch m.focus {
	case leftSide:
		return rightSide
	case rightSide:
		if m.queue.HasItems() {
			return queueSide
		}
	case queueSide:
	}
	return leftSide
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

// Update implements tea.Model. It wraps update so any change that starts an
// animation also starts the frame ticker.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	if !nm.animating && nm.animationsRunning() {
		nm.animating = true
		cmd = tea.Batch(cmd, animTick())
	}
	return nm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case animTickMsg:
		return m.stepAnimations()

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width - 2)
		m.layout()
		m.aimFocusBar(false)
		m.confetti = nil // sized for the old screen
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
		return m.onConnected(msg.Conn, msg.LocalPath)

	case disconnectedMsg:
		if msg.conn != m.conn {
			return m, nil // a connection we already let go of
		}
		label := m.conn.Target.String()
		log.Debug("connection lost", "target", label, "err", msg.err)
		m.dropConnection()
		return m, m.setNotice(noticeError, "connection to "+label+" lost", errorNoticeTTL)

	case queue.ItemFinishedMsg:
		return m.onItemFinished(msg.ItemDoneEvent)

	case queue.IdleMsg:
		ev := msg.IdleEvent
		var extra []tea.Cmd
		if m.cfg.Animations && ev.Failed == 0 && ev.Done > 0 && (ev.Elapsed >= 2*time.Second || ev.Done >= 5) {
			p := m.styles.Palette
			m.confetti = fx.NewConfetti(m.width, m.height, []color.Color{p.Primary, p.Secondary, p.Accent, p.Success, p.Warning}, nil)
		}
		if m.cfg.Bell && ev.Elapsed >= 5*time.Second {
			extra = append(extra, tea.Raw("\a"))
		}
		if ev.Done+ev.Failed > 1 {
			text := fmt.Sprintf("✓ all done: %d items, %s in %s", ev.Done, units.FormatBytes(ev.Bytes), units.FormatDuration(ev.Elapsed))
			kind := noticeInfo
			if ev.Failed > 0 {
				text = fmt.Sprintf("finished with %d failed, %d done", ev.Failed, ev.Done)
				kind = noticeWarn
			}
			return m, tea.Batch(append(extra, m.setNotice(kind, text, errorNoticeTTL))...)
		}
		return m, tea.Batch(extra...)

	case settingsSubmittedMsg:
		if m.settings != nil {
			return m.applySettings(m.settings)
		}
		return m, nil

	case addBookmarkMsg:
		return m.onAddBookmark(msg)

	case settingsCancelledMsg:
		m.settings = nil
		return m, nil

	case opDoneMsg:
		return m.onOpDone(msg)

	case pane.OpenFileMsg:
		return m, tea.Batch(loadPreview(msg), m.setNotice(noticeInfo, "opening "+vfs.Sanitize(msg.Entry.Name)+"…", noticeTTL))

	case previewMsg:
		m.preview = newPreview(msg, m.styles, m.width, m.height)
		m.notice = ""
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	if m.input != nil { // cursor blink
		_, cmd := m.input.update(msg)
		mm, bcmd := m.broadcast(msg)
		return mm, tea.Batch(cmd, bcmd)
	}
	if m.settings != nil {
		f, cmd := m.settings.form.Update(msg)
		if form, ok := f.(*huh.Form); ok {
			m.settings.form = form
		}
		mm, bcmd := m.broadcast(msg)
		return mm, tea.Batch(cmd, bcmd)
	}
	return m.broadcast(msg)
}

// broadcast delivers a non-key message to every child. Each one ignores
// messages that aren't addressed to it.
func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds [4]tea.Cmd
	m.local, cmds[0] = m.local.Update(msg)
	if m.conn != nil {
		m.remote, cmds[1] = m.remote.Update(msg)
	}
	m.connect, cmds[2] = m.connect.Update(msg)
	m.queue, cmds[3] = m.queue.Update(msg)
	m.layout()
	return m, tea.Batch(cmds[:]...)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	// Any key skips the splash (unless a prompt is already waiting).
	if m.splash && !m.connect.HasDialog() {
		m.splash = false
		return m, nil
	}
	// Prompts are modal.
	switch {
	case m.connect.HasDialog():
		var cmd tea.Cmd
		m.connect, cmd = m.connect.Update(msg)
		return m, cmd
	case m.queue.HasDialog():
		var cmd tea.Cmd
		m.queue, cmd = m.queue.Update(msg)
		return m, cmd
	case m.confirm != nil:
		d := m.confirm
		switch msg.String() {
		case "y", "Y", "enter":
			m.confirm = nil
			return d.yes(m)
		case "n", "N", "esc":
			m.confirm = nil
		}
		return m, nil
	case m.settings != nil:
		f, cmd := m.settings.form.Update(msg)
		if form, ok := f.(*huh.Form); ok {
			m.settings.form = form
		}
		return m, cmd
	case m.input != nil:
		done, cmd := m.input.update(msg)
		if done {
			m.input = nil
		}
		return m, cmd
	case m.preview != nil:
		done, cmd := m.preview.update(msg)
		if done {
			m.preview = nil
		}
		return m, cmd
	case m.bookmarks != nil:
		return m.updateBookmarks(msg)
	}

	if !m.focusedCapturesKeys() {
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m.confirmIfBusy("Quit sftpterm?", (Model).quit)
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Theme):
			m.setPalette(theme.Next(m.cfg.Theme))
			log.Debug("theme changed", "theme", m.cfg.Theme)
			return m, m.setNotice(noticeInfo, "theme → "+m.cfg.Theme, noticeTTL)
		case key.Matches(msg, m.keys.SwitchPane):
			m.setFocus(m.nextFocus())
			return m, nil
		case key.Matches(msg, m.keys.Disconnect) && m.conn != nil:
			return m.confirmIfBusy("Disconnect?", (Model).disconnect)
		case key.Matches(msg, m.keys.Copy) && m.focus != queueSide && (m.focus == leftSide || m.conn != nil):
			return m.copySelection()
		case key.Matches(msg, m.keys.MoreParallel), key.Matches(msg, m.keys.LessParallel):
			t := m.transfers
			if key.Matches(msg, m.keys.MoreParallel) {
				t.Concurrency = min(t.Concurrency+1, config.MaxConcurrency)
			} else {
				t.Concurrency = max(t.Concurrency-1, config.MinConcurrency)
			}
			m.applyTransferSettings(t)
			return m, m.setNotice(noticeInfo, fmt.Sprintf("%d transfers at once", t.Concurrency), noticeTTL)
		case key.Matches(msg, m.keys.Settings):
			alias := ""
			if m.conn != nil {
				alias = m.conn.Target.Alias
			}
			m.settings = newSettingsForm(m.cfg, m.transfers, alias, m.styles)
			return m, m.settings.form.Init()
		case key.Matches(msg, m.keys.AddBookmark) && m.focus != queueSide && (m.focus == leftSide || m.conn != nil):
			return m.startAddBookmark()
		case key.Matches(msg, m.keys.Bookmarks) && m.focus != queueSide && (m.focus == leftSide || m.conn != nil):
			return m.openBookmarks()
		case m.focusedPane() != nil && key.Matches(msg, m.keys.NewFolder):
			return m.startNewFolder()
		case m.focusedPane() != nil && key.Matches(msg, m.keys.Rename):
			return m.startRename()
		case m.focusedPane() != nil && key.Matches(msg, m.keys.Delete):
			return m.startDelete()
		case m.focusedPane() != nil && key.Matches(msg, m.keys.View):
			if e, ok := m.focusedPane().Selected(); ok && !e.IsDir {
				p := m.focusedPane()
				return m.Update(pane.OpenFileMsg{FS: p.FS(), Path: p.FS().Join(p.Path(), e.Name), Entry: e})
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch {
	case m.focus == leftSide:
		m.local, cmd = m.local.Update(msg)
	case m.focus == queueSide:
		m.queue, cmd = m.queue.Update(msg)
		if !m.queue.HasItems() {
			m.setFocus(leftSide)
		}
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
	case m.focus == queueSide:
		return false
	case m.conn != nil:
		return m.remote.Filtering()
	default:
		return m.connect.CapturesKeys()
	}
}

// confirmIfBusy runs action now, or asks first if transfers are running.
func (m Model) confirmIfBusy(title string, action func(Model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if m.engine == nil || !m.engine.Busy() {
		return action(m)
	}
	n := m.queue.Snapshot().Active + m.queue.Snapshot().Queued
	m.confirm = &confirmDialog{
		title: title,
		body:  fmt.Sprintf("%d transfers are still in progress. They will be cancelled and partial files removed.", max(n, 1)),
		yes:   action,
	}
	return m, nil
}

// copySelection queues the marked entries (or the one under the cursor) from
// the focused pane into the other pane's directory.
func (m Model) copySelection() (tea.Model, tea.Cmd) {
	if m.conn == nil || m.engine == nil {
		return m, m.setNotice(noticeWarn, "connect to a server first", noticeTTL)
	}
	src, dst, dir := &m.local, &m.remote, transfer.Upload
	if m.focus == rightSide {
		src, dst, dir = &m.remote, &m.local, transfer.Download
	}
	entries := src.Marked()
	if len(entries) == 0 {
		if e, ok := src.Selected(); ok {
			entries = []vfs.Entry{e}
		}
	}
	if len(entries) == 0 || dst.Path() == "" {
		return m, m.setNotice(noticeWarn, "nothing to copy", noticeTTL)
	}
	m.engine.Enqueue(dir, src.Path(), entries, dst.Path())
	src.ClearMarks()
	log.Debug("queued", "dir", dir, "count", len(entries), "from", src.Path(), "to", dst.Path())

	what := entries[0].Name
	if len(entries) > 1 {
		what = fmt.Sprintf("%d items", len(entries))
	}
	kick := m.queue.Kick()
	m.layout()
	return m, tea.Batch(kick, m.setNotice(noticeInfo, dir.String()+"ing "+what, noticeTTL))
}

func (m Model) onItemFinished(ev transfer.ItemDoneEvent) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// Refresh the destination pane if it's showing where the item landed.
	if ev.Dir == transfer.Upload && m.conn != nil && m.remote.Path() == ev.DstDir {
		cmds = append(cmds, m.remote.Navigate(ev.DstDir, ""))
	}
	if ev.Dir == transfer.Download && m.local.Path() == ev.DstDir {
		cmds = append(cmds, m.local.Navigate(ev.DstDir, ""))
	}
	verb := map[transfer.Direction]string{transfer.Upload: "uploaded", transfer.Download: "downloaded"}[ev.Dir]
	switch ev.State {
	case transfer.Done:
		text := "✓ " + verb + " " + ev.Name + " (" + units.FormatBytes(ev.Bytes) + ")"
		if ev.Skipped > 0 {
			text += fmt.Sprintf(", %d skipped", ev.Skipped)
		}
		cmds = append(cmds, m.setNotice(noticeInfo, text, noticeTTL))
	case transfer.Failed:
		cmds = append(cmds, m.setNotice(noticeError, ev.Name+": "+ev.Err.Error(), errorNoticeTTL))
	case transfer.Cancelled:
		cmds = append(cmds, m.setNotice(noticeInfo, "cancelled "+ev.Name, noticeTTL))
	case transfer.Queued, transfer.Scanning, transfer.Active:
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) applyTransferSettings(t config.Transfers) {
	m.transfers = t
	if m.conn == nil {
		m.cfg.Transfers = t
	}
	if m.engine != nil {
		m.engine.SetConcurrency(t.Concurrency)
		m.engine.SetLimit(transfer.Upload, t.UploadLimit)
		m.engine.SetLimit(transfer.Download, t.DownloadLimit)
	}
}

func (m Model) onConnected(conn *sshx.Conn, localPath string) (tea.Model, tea.Cmd) {
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
	m.transfers = m.cfg.TransfersFor(conn.Target.Alias)
	t := m.transfers
	m.engine = transfer.New(transfer.Options{
		Local: m.local.FS(), Remote: fsys, Concurrency: t.Concurrency,
		UploadLimit: t.UploadLimit, DownloadLimit: t.DownloadLimit,
	})
	m.queue = queue.New(m.engine, m.styles)

	home, _ := fsys.Home()
	start := conn.Target.Path
	cmds := []tea.Cmd{}
	notice := "connected to " + label
	switch {
	case start != "" && !strings.HasPrefix(start, "/"):
		start = path.Join(home, start)
	case start == "" && m.store != nil:
		// Pick up where we left off with this host.
		if last, ok := m.store.Last(conn.Target.String()); ok {
			start = last.Remote
			if localPath == "" {
				localPath = last.Local
			}
			notice = "connected to " + label + " · back where you left off"
		}
	}
	if start == "" {
		start = home
	}
	if localPath != "" && localPath != m.local.Path() {
		if st, err := os.Stat(localPath); err == nil && st.IsDir() {
			cmds = append(cmds, m.local.Navigate(localPath, ""))
		}
	}
	load := m.remote.NavigateOr(start, home, "")
	m.setFocus(rightSide)
	m.layout()
	log.Debug("connected", "target", label, "start", start)

	wait := func() tea.Msg { return disconnectedMsg{conn: conn, err: conn.Wait()} }
	cmds = append(cmds, load, wait, m.queue.Init(), m.setNotice(noticeInfo, notice, noticeTTL))
	return m, tea.Batch(cmds...)
}

func (m Model) disconnect() (tea.Model, tea.Cmd) {
	m.rememberLast()
	conn, engine := m.conn, m.engine
	label := conn.Target.String()
	m.engine = nil // shut down below, in order
	m.dropConnection()
	closeConn := func() tea.Msg { shutdown(engine, conn); return nil }
	return m, tea.Batch(closeConn, m.setNotice(noticeInfo, "disconnected from "+label, noticeTTL))
}

// shutdownGrace is how long cancelled transfers get to remove their partial
// files before the connection is closed underneath them.
const shutdownGrace = 3 * time.Second

// shutdown cancels transfers, gives them a moment to clean up, then closes
// the connection.
func shutdown(engine *transfer.Engine, conn *sshx.Conn) {
	if engine != nil {
		engine.CancelAll()
		done := make(chan struct{})
		go func() { engine.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(shutdownGrace):
			log.Debug("transfers did not stop in time; closing anyway")
		}
	}
	if conn != nil {
		_ = conn.Close()
	}
}

// rememberLast records where both panes are, for the next connection to
// this host.
func (m *Model) rememberLast() {
	if m.conn == nil || m.store == nil {
		return
	}
	m.store.SetLast(m.connKey(), m.local.Path(), m.remote.Path())
	if err := m.store.Save(); err != nil {
		log.Debug("saving state", "err", err)
	}
}

func (m *Model) dropConnection() {
	m.rememberLast()
	m.transfers = m.cfg.Transfers
	m.bookmarks = nil
	m.connect.Refresh()
	if m.engine != nil {
		go m.engine.Close() // the connection is gone; workers exit as I/O fails
		m.engine = nil
	}
	m.conn = nil
	m.remote = pane.Model{}
	m.queue = queue.Model{}
	m.confirm = nil
	if m.focus == queueSide {
		m.focus = rightSide
	}
	m.setFocus(m.focus)
	m.layout()
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.rememberLast()
	m.connect.Close()
	shutdown(m.engine, m.conn)
	m.engine, m.conn = nil, nil
	return m, tea.Quit
}

// layout distributes the terminal between header, panes, and footer.
func (m *Model) layout() {
	footerHeight := 1 + lipgloss.Height(m.helpView())
	queueHeight := m.queue.Height()
	// Keep at least a few rows for the panes on short terminals.
	if m.height-headerHeight-footerHeight-queueHeight < 6 {
		queueHeight = 0
	}
	m.bodyHeight = max(m.height-headerHeight-footerHeight-queueHeight, 0)
	lw := m.width / 2
	m.local.SetSize(lw, m.bodyHeight)
	m.remote.SetSize(m.width-lw, m.bodyHeight)
	m.connect.SetSize(m.width-lw, m.bodyHeight)
	m.queue.SetWidth(m.width)
}

// queueVisible reports whether layout left room for the queue panel.
func (m Model) queueVisible() bool {
	return m.queue.HasItems() && m.height-headerHeight-1-lipgloss.Height(m.helpView())-m.bodyHeight > 0
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "sftpterm"
	if m.conn != nil {
		v.WindowTitle = "sftpterm · " + m.conn.Target.String()
	}
	v.ProgressBar = m.terminalProgress()
	return v
}

// terminalProgress reports overall transfer progress to terminals that show
// it in the tab or taskbar (OSC 9;4). Others ignore it.
func (m Model) terminalProgress() *tea.ProgressBar {
	if m.engine == nil {
		return nil
	}
	snap := m.queue.Snapshot()
	if snap.Active+snap.Queued == 0 {
		return nil
	}
	var done, total int64
	for _, it := range snap.Items {
		if !it.State.Finished() {
			done += it.Done
			total += it.Total
		}
	}
	if total == 0 {
		return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, int(done*100/total))
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // waiting for the first WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return m.tooSmallView()
	case m.splash && !m.connect.HasDialog():
		return fx.Splash(m.styles, m.frame, m.width, m.height)
	}

	right := m.connect.View()
	if m.conn != nil {
		right = m.remote.View()
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.local.View(), right)
	parts := []string{m.headerView(), body}
	if m.queueVisible() {
		parts = append(parts, m.queue.View())
	}
	parts = append(parts, m.statusView(), m.helpView())
	screen := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if d := m.dialogView(); d != "" {
		dw, dh := lipgloss.Size(d)
		screen = theme.Overlay(screen, d, max((m.width-dw)/2, 0), max((m.height-dh)/2, 0))
	}
	if m.confetti != nil {
		screen = theme.OverlayCells(screen, m.confetti.Cells())
	}
	return screen
}

// dialogView renders whichever modal is open, if any.
func (m Model) dialogView() string {
	switch {
	case m.connect.HasDialog():
		return m.connect.Dialog(m.width)
	case m.queue.HasDialog():
		return m.queue.Dialog(m.width)
	case m.confirm != nil:
		return m.confirm.view(m.styles, m.width)
	case m.settings != nil:
		return m.settings.view(m.styles)
	case m.input != nil:
		return m.input.view(m.styles, m.width)
	case m.preview != nil:
		return m.preview.view(m.styles)
	case m.bookmarks != nil:
		return m.bookmarks.view(m.styles, m.width)
	}
	return ""
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

	return " " + title + " " + rule + " " + conn + " \n" + m.focusBar()
}

// focusBar is the line under the header: a glowing strip above whichever
// side has focus, sliding when focus moves.
func (m Model) focusBar() string {
	x := max(min(int(m.focusX.Value()+0.5), m.width), 0)
	w := max(min(int(m.focusW.Value()+0.5), m.width-x), 0)
	if w < 4 {
		return strings.Repeat(" ", m.width)
	}
	p := m.styles.Palette
	bar := theme.Gradient(lipgloss.NewStyle(), strings.Repeat("▁", w-2), p.Primary, p.Accent, p.Secondary)
	return strings.Repeat(" ", x+1) + bar + strings.Repeat(" ", m.width-x-w+1)
}

func (m Model) statusView() string {
	t := m.transfers
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
	if fm, ok := final.(Model); ok {
		shutdown(fm.engine, fm.conn)
	}
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

// applySettings applies the settings form and saves it if asked.
func (m Model) applySettings(f *settingsForm) (tea.Model, tea.Cmd) {
	v := f.vals
	m.settings = nil
	if p, ok := theme.Get(v.theme); ok && p.Name != m.cfg.Theme {
		m.setPalette(p)
	}
	m.cfg.Icons, m.cfg.ShowHidden = v.icons, v.showHidden
	m.cfg.Animations, m.cfg.Bell = v.animations, v.bell
	for _, p := range []*pane.Model{&m.local, &m.remote} {
		p.SetIcons(v.icons)
		p.SetShowHidden(v.showHidden)
	}
	t := f.transfers()
	m.applyTransferSettings(t)

	switch v.scope {
	case scopeSession:
		return m, m.setNotice(noticeInfo, "settings applied for this session", noticeTTL)
	case scopeDefault:
		m.cfg.Transfers = t
		m.fileCfg.Transfers = t
	case scopeHost:
		m.cfg.SetHostTransfers(f.host, t)
		m.fileCfg.SetHostTransfers(f.host, t)
	}
	m.fileCfg.Theme, m.fileCfg.Icons, m.fileCfg.ShowHidden = m.cfg.Theme, v.icons, v.showHidden
	m.fileCfg.Animations, m.fileCfg.Bell = v.animations, v.bell
	if m.cfgPath == "" {
		return m, m.setNotice(noticeWarn, "no config file to save to; applied for this session", noticeTTL)
	}
	if err := config.Save(m.cfgPath, m.fileCfg); err != nil {
		return m, m.setNotice(noticeError, "saving settings: "+err.Error(), errorNoticeTTL)
	}
	where := "as defaults"
	if v.scope == scopeHost {
		where = "for " + f.host
	}
	return m, m.setNotice(noticeInfo, "✓ settings saved "+where, noticeTTL)
}

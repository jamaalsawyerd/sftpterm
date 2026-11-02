// Package connect implements the connection screen: a host picker fed by
// ~/.ssh/config, a form for ad-hoc hosts, and the prompts shown while
// authenticating.
package connect

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

// ConnectedMsg is sent when a connection attempt succeeds. The receiver owns
// the connection and must close it.
type ConnectedMsg struct{ Conn *sshx.Conn }

type state int

const (
	statePick state = iota
	stateForm
	stateConnecting
)

type formSubmittedMsg struct{}

type formCancelledMsg struct{}

type keyMap struct {
	Up, Down, Connect, New, Cancel key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Connect: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect")),
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new host")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// formValues lives on the heap so the huh form's pointers stay valid while the
// Model is copied around by value.
type formValues struct {
	host, user, port, path string
}

// Model is the connection screen.
type Model struct {
	sshcfg *sshx.SSHConfig
	hosts  []sshx.Target
	cursor int // index into hosts; len(hosts) is "new connection"

	state state
	form  *huh.Form
	vals  *formValues

	attempt int
	cancel  context.CancelFunc
	events  chan any
	target  sshx.Target
	stage   string
	spinner spinner.Model
	dialog  *dialog
	err     error

	width, height int
	focused       bool
	styles        theme.Styles
	keys          keyMap
}

// New builds the connection screen from an OpenSSH config.
func New(cfg *sshx.SSHConfig, styles theme.Styles) Model {
	m := Model{sshcfg: cfg, keys: defaultKeys(), spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
	for _, alias := range cfg.Hosts() {
		m.hosts = append(m.hosts, cfg.Resolve(alias))
	}
	m.SetStyles(styles)
	return m
}

// SetStyles applies a theme.
func (m *Model) SetStyles(s theme.Styles) {
	m.styles = s
	m.spinner.Style = lipgloss.NewStyle().Foreground(s.Palette.Accent)
	if m.form != nil {
		m.form.WithTheme(formTheme(s))
	}
}

// SetSize sets the outer size, including the frame.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	if m.form != nil {
		m.form.WithWidth(m.formWidth())
	}
}

// SetFocused marks the screen as receiving keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// Connecting reports whether an attempt is in progress.
func (m Model) Connecting() bool { return m.state == stateConnecting }

// Target is the host being connected to.
func (m Model) Target() sshx.Target { return m.target }

// CapturesKeys reports whether all keys (even global ones like q) should go
// to this screen: a prompt or form is open.
func (m Model) CapturesKeys() bool { return m.dialog != nil || m.state == stateForm }

// Connect starts connecting to t, e.g. for a destination given on the
// command line.
func (m *Model) Connect(t sshx.Target) tea.Cmd {
	m.abort()
	m.attempt++
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.events, m.target = cancel, make(chan any), t
	m.state, m.stage, m.err = stateConnecting, "starting", nil
	log.Debug("connecting", "target", t.String(), "addr", t.Addr())
	go run(ctx, t, m.events)
	return tea.Batch(waitForEvent(m.attempt, m.events), m.spinner.Tick)
}

// abort cancels any attempt in progress.
func (m *Model) abort() {
	if m.dialog != nil {
		m.dialog.cancel()
		m.dialog = nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.state == stateConnecting {
		m.state = statePick
		m.attempt++ // ignore anything still in flight
	}
}

// Update handles messages for the connection screen. Key presses are only
// handled when focused or when a dialog is open.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventMsg:
		return m.handleEvent(msg)
	case spinner.TickMsg:
		if m.state != stateConnecting {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case formSubmittedMsg:
		return m.submitForm()
	case formCancelledMsg:
		m.state, m.form = statePick, nil
		return m, nil
	}

	if m.dialog != nil {
		done, cmd := m.dialog.update(msg, m.keys)
		if done {
			m.dialog = nil
			return m, waitForEvent(m.attempt, m.events)
		}
		return m, cmd
	}

	if m.state == stateForm && m.form != nil {
		f, cmd := m.form.Update(msg)
		if form, ok := f.(*huh.Form); ok {
			m.form = form
		}
		return m, cmd
	}

	km, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.focused {
		return m, nil
	}
	switch m.state {
	case stateConnecting:
		if key.Matches(km, m.keys.Cancel) {
			m.abort()
		}
	case statePick:
		switch {
		case key.Matches(km, m.keys.Up):
			m.cursor = max(m.cursor-1, 0)
		case key.Matches(km, m.keys.Down):
			m.cursor = min(m.cursor+1, len(m.hosts))
		case key.Matches(km, m.keys.New):
			return m, m.openForm()
		case key.Matches(km, m.keys.Connect):
			if m.cursor == len(m.hosts) {
				return m, m.openForm()
			}
			return m, m.Connect(m.hosts[m.cursor])
		case key.Matches(km, m.keys.Cancel):
			m.err = nil
		}
	case stateForm:
	}
	return m, nil
}

func (m Model) handleEvent(msg eventMsg) (Model, tea.Cmd) {
	if msg.attempt != m.attempt {
		// A late event from an abandoned attempt: release whatever it holds.
		switch ev := msg.event.(type) {
		case askEvent:
			ev.reply <- answer{}
		case hostKeyEvent:
			ev.reply <- false
		case doneEvent:
			if ev.conn != nil {
				_ = ev.conn.Close()
			}
		}
		return m, nil
	}

	switch ev := msg.event.(type) {
	case progressEvent:
		m.stage = ev.stage
	case askEvent:
		m.dialog = newAskDialog(ev, m.styles)
		return m, m.dialog.init()
	case hostKeyEvent:
		m.dialog = newHostKeyDialog(ev)
		return m, nil
	case doneEvent:
		m.state, m.cancel = statePick, nil
		if ev.err != nil {
			log.Debug("connect failed", "target", m.target.String(), "err", ev.err)
			if !errors.Is(ev.err, sshx.ErrCancelled) && !errors.Is(ev.err, context.Canceled) {
				m.err = ev.err
			}
			return m, nil
		}
		conn := ev.conn
		return m, func() tea.Msg { return ConnectedMsg{Conn: conn} }
	}
	return m, waitForEvent(m.attempt, m.events)
}

func (m *Model) openForm() tea.Cmd {
	m.vals = &formValues{}
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	m.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Host").Placeholder("alias, hostname, or IP").Value(&m.vals.host).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("host is required")
				}
				return nil
			}),
		huh.NewInput().Title("User").Placeholder("from ssh config, or your username").Value(&m.vals.user),
		huh.NewInput().Title("Port").Placeholder("22").Value(&m.vals.port).
			Validate(func(s string) error {
				if s == "" {
					return nil
				}
				if p, err := strconv.Atoi(s); err != nil || p < 1 || p > 65535 {
					return errors.New("port must be 1-65535")
				}
				return nil
			}),
		huh.NewInput().Title("Directory").Placeholder("home directory").Value(&m.vals.path),
	)).
		WithTheme(formTheme(m.styles)).
		WithKeyMap(keys).
		WithWidth(m.formWidth()).
		WithShowHelp(true)
	m.form.SubmitCmd = func() tea.Msg { return formSubmittedMsg{} }
	m.form.CancelCmd = func() tea.Msg { return formCancelledMsg{} }
	m.state, m.err = stateForm, nil
	return m.form.Init()
}

func (m Model) submitForm() (Model, tea.Cmd) {
	v := m.vals
	m.form = nil
	if v == nil {
		m.state = statePick
		return m, nil
	}
	d := sshx.Destination{Host: strings.TrimSpace(v.host), User: strings.TrimSpace(v.user), Path: strings.TrimSpace(v.path)}
	if p, err := strconv.Atoi(strings.TrimSpace(v.port)); err == nil {
		d.Port = p
	}
	// Let "user@host" typed into the host field work too.
	if parsed, err := sshx.ParseDestination(d.Host); err == nil {
		d.Host = parsed.Host
		if d.User == "" {
			d.User = parsed.User
		}
		if d.Path == "" {
			d.Path = parsed.Path
		}
	}
	return m, m.Connect(m.sshcfg.TargetFor(d))
}

func (m Model) formWidth() int { return max(m.width-6, 20) }

// HelpKeys returns the bindings relevant to the current state.
func (m Model) HelpKeys() []key.Binding {
	switch {
	case m.dialog != nil && m.dialog.isHostKey():
		return []key.Binding{
			key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "trust")),
			key.NewBinding(key.WithKeys("n"), key.WithHelp("n/esc", "cancel")),
		}
	case m.dialog != nil:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
			m.keys.Cancel,
		}
	case m.state == stateForm:
		return []key.Binding{
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next/connect")),
			m.keys.Cancel,
		}
	case m.state == stateConnecting:
		return []key.Binding{m.keys.Cancel}
	}
	return []key.Binding{m.keys.Connect, m.keys.New}
}

// HasDialog reports whether a prompt is open. Prompts are modal: they take
// every key regardless of focus.
func (m Model) HasDialog() bool { return m.dialog != nil }

// Dialog renders the active prompt no wider than maxWidth, or "" when there
// is none.
func (m Model) Dialog(maxWidth int) string {
	if m.dialog == nil {
		return ""
	}
	return m.dialog.view(m.styles, min(dialogWidth, maxWidth-2))
}

// View renders the screen at exactly its configured size.
func (m Model) View() string {
	iw, ih := max(m.width-2, 0), max(m.height-2, 0)
	var body string
	switch m.state {
	case stateForm:
		body = lipgloss.NewStyle().Padding(1, 2).Render(m.form.View())
	case stateConnecting:
		body = m.connectingView(iw, ih)
	default:
		body = m.pickView(iw)
	}
	return m.styles.Frame("Connect", "", body, m.width, m.height, m.focused)
}

func (m Model) connectingView(iw, ih int) string {
	p := m.styles.Palette
	lines := lipgloss.JoinVertical(lipgloss.Left,
		m.spinner.View()+" "+lipgloss.NewStyle().Bold(true).Foreground(p.Text).Render("Connecting to "+m.target.Alias),
		"  "+m.styles.Muted.Render(m.stage),
		"",
		"  "+m.styles.Subtle.Render("esc to cancel"),
	)
	return lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, lines)
}

func (m Model) pickView(iw int) string {
	p := m.styles.Palette
	var b strings.Builder
	b.WriteString("\n " + theme.Gradient(lipgloss.NewStyle().Bold(true), "✦ Where to?", p.Primary, p.Secondary) + "\n\n")

	if m.err != nil {
		msg := lipgloss.NewStyle().Width(max(iw-4, 10)).Render(m.err.Error())
		for l := range strings.SplitSeq(msg, "\n") {
			b.WriteString("  " + m.styles.Error.Render(l) + "\n")
		}
		b.WriteString("\n")
	}

	aliasWidth := 4
	for _, h := range m.hosts {
		aliasWidth = max(aliasWidth, lipgloss.Width(h.Alias))
	}
	aliasWidth = min(aliasWidth, max(iw/2, 4))

	row := func(i int, label, detail string, labelStyle lipgloss.Style) {
		selected := i == m.cursor
		marker := "  "
		if selected {
			marker = lipgloss.NewStyle().Foreground(p.Primary).Render("▸ ")
			labelStyle = labelStyle.Foreground(p.Primary).Bold(true)
		}
		if detail != "" {
			label = theme.Fit(label, aliasWidth) // align the details column
		}
		line := " " + marker + labelStyle.Render(label)
		if detail != "" {
			line += "  " + m.styles.Muted.Render(detail)
		}
		b.WriteString(line + "\n")
	}
	for i, h := range m.hosts {
		detail := h.User + "@" + h.Host
		if h.Port != sshx.DefaultPort {
			detail += ":" + strconv.Itoa(h.Port)
		}
		row(i, h.Alias, detail, m.styles.Text)
	}
	row(len(m.hosts), "+ new connection…", "", m.styles.Muted)

	if len(m.hosts) == 0 {
		b.WriteString("\n  " + m.styles.Subtle.Render("No hosts found in ~/.ssh/config.") + "\n")
	}
	return b.String()
}

// Close abandons any attempt in progress.
func (m *Model) Close() { m.abort() }

func formTheme(s theme.Styles) huh.Theme {
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

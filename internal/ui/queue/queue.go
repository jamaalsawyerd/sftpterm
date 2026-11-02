// Package queue renders the transfer queue panel and asks the user how to
// resolve name conflicts.
package queue

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/transfer"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

const (
	tickInterval = 100 * time.Millisecond
	// MaxRows is the most items shown at once; the panel scrolls beyond that.
	MaxRows = 6
)

// ItemFinishedMsg reports a finished top-level item to the app.
type ItemFinishedMsg struct{ transfer.ItemDoneEvent }

// IdleMsg reports that every queued transfer has finished.
type IdleMsg struct{ transfer.IdleEvent }

type tickMsg struct{ engine *transfer.Engine }

type eventMsg struct {
	engine *transfer.Engine
	event  transfer.Event
}

// KeyMap holds the queue's bindings.
type KeyMap struct {
	Up, Down, Cancel, CancelAll, Retry, Clear key.Binding
}

// DefaultKeyMap returns the queue bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Cancel:    key.NewBinding(key.WithKeys("x", "delete"), key.WithHelp("x", "cancel")),
		CancelAll: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "cancel all")),
		Retry:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
		Clear:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear finished")),
	}
}

// Model is the transfer queue panel.
type Model struct {
	engine  *transfer.Engine
	snap    transfer.Snapshot
	ticking bool
	cursor  int
	offset  int
	focused bool
	width   int
	styles  theme.Styles
	keys    KeyMap
	dialog  *conflictDialog
}

// New creates a panel for engine.
func New(engine *transfer.Engine, styles theme.Styles) Model {
	return Model{engine: engine, styles: styles, keys: DefaultKeyMap()}
}

// Init starts listening for engine events.
func (m Model) Init() tea.Cmd { return m.listen() }

func (m Model) listen() tea.Cmd {
	e := m.engine
	if e == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-e.Events()
		if !ok {
			return nil
		}
		return eventMsg{engine: e, event: ev}
	}
}

// Kick refreshes immediately and starts the progress ticker if it isn't
// running. Call it after enqueueing.
func (m *Model) Kick() tea.Cmd {
	if m.engine == nil {
		return nil
	}
	m.snap = m.engine.Snapshot()
	m.clamp()
	if m.ticking {
		return nil
	}
	m.ticking = true
	return m.tick()
}

func (m Model) tick() tea.Cmd {
	e := m.engine
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{engine: e} })
}

// SetStyles applies a theme.
func (m *Model) SetStyles(s theme.Styles) { m.styles = s }

// SetWidth sets the panel's outer width.
func (m *Model) SetWidth(w int) { m.width = w }

// SetFocused marks the panel as receiving keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// KeyMap returns the bindings for help.
func (m Model) KeyMap() KeyMap { return m.keys }

// HasItems reports whether there is anything to show.
func (m Model) HasItems() bool { return len(m.snap.Items) > 0 }

// Snapshot is the most recent queue state.
func (m Model) Snapshot() transfer.Snapshot { return m.snap }

// Height is the panel's outer height, or 0 when it should be hidden.
func (m Model) Height() int {
	if !m.HasItems() {
		return 0
	}
	return min(len(m.snap.Items), MaxRows) + 2
}

// HasDialog reports whether a conflict prompt is open.
func (m Model) HasDialog() bool { return m.dialog != nil }

// Update handles queue messages, and keys when focused or prompting.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if msg.engine != m.engine || m.engine == nil {
			return m, nil
		}
		m.snap = m.engine.Snapshot()
		m.clamp()
		if m.engine.Busy() {
			return m, m.tick()
		}
		m.ticking = false
		return m, nil

	case eventMsg:
		if msg.engine != m.engine {
			return m, nil // from an engine we already closed
		}
		return m.handleEvent(msg.event)

	case tea.KeyPressMsg:
		if m.dialog != nil {
			if done := m.dialog.update(msg); done {
				m.dialog = nil
				return m, m.listen()
			}
			return m, nil
		}
		if m.focused {
			return m.handleKey(msg)
		}
	}
	return m, nil
}

func (m Model) handleEvent(ev transfer.Event) (Model, tea.Cmd) {
	switch ev := ev.(type) {
	case transfer.ConflictEvent:
		m.dialog = &conflictDialog{ev: ev}
		return m, nil // resume listening once answered
	case transfer.ItemDoneEvent:
		m.snap = m.engine.Snapshot()
		m.clamp()
		return m, tea.Batch(m.listen(), func() tea.Msg { return ItemFinishedMsg{ev} })
	case transfer.IdleEvent:
		m.snap = m.engine.Snapshot()
		return m, tea.Batch(m.listen(), func() tea.Msg { return IdleMsg{ev} })
	}
	return m, m.listen()
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	sel, ok := m.selected()
	switch {
	case key.Matches(msg, m.keys.Up):
		m.cursor--
	case key.Matches(msg, m.keys.Down):
		m.cursor++
	case key.Matches(msg, m.keys.Cancel) && ok:
		m.engine.Cancel(sel.ID)
	case key.Matches(msg, m.keys.CancelAll):
		m.engine.CancelAll()
	case key.Matches(msg, m.keys.Retry) && ok:
		if m.engine.Retry(sel.ID) {
			return m, m.Kick()
		}
	case key.Matches(msg, m.keys.Clear):
		m.engine.ClearFinished()
		m.snap = m.engine.Snapshot()
	}
	m.clamp()
	return m, nil
}

func (m Model) selected() (transfer.ItemSnapshot, bool) {
	if m.cursor < 0 || m.cursor >= len(m.snap.Items) {
		return transfer.ItemSnapshot{}, false
	}
	return m.snap.Items[m.cursor], true
}

func (m *Model) clamp() {
	m.cursor = max(min(m.cursor, len(m.snap.Items)-1), 0)
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+MaxRows {
		m.offset = m.cursor - MaxRows + 1
	}
	m.offset = max(min(m.offset, len(m.snap.Items)-MaxRows), 0)
}

// View renders the panel at its width and Height().
func (m Model) View() string {
	h := m.Height()
	if h == 0 {
		return ""
	}
	iw := max(m.width-2, 0)
	var lines []string
	end := min(m.offset+MaxRows, len(m.snap.Items))
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.row(m.snap.Items[i], i == m.cursor && m.focused, iw))
	}
	return m.styles.Frame("Transfers", m.summary(), strings.Join(lines, "\n"), m.width, h, m.focused)
}

func (m Model) summary() string {
	s := m.snap
	var parts []string
	if s.Active > 0 {
		parts = append(parts, strconv.Itoa(s.Active)+" active")
	}
	if s.Queued > 0 {
		parts = append(parts, strconv.Itoa(s.Queued)+" queued")
	}
	if s.UpRate > 0 {
		parts = append(parts, "↑ "+units.FormatRate(s.UpRate))
	}
	if s.DownRate > 0 {
		parts = append(parts, "↓ "+units.FormatRate(s.DownRate))
	}
	if len(parts) == 0 {
		return "idle"
	}
	return strings.Join(parts, " · ")
}

func (m Model) row(it transfer.ItemSnapshot, selected bool, iw int) string {
	p := m.styles.Palette
	paint := func(st lipgloss.Style, s string) string {
		if selected {
			st = st.Background(p.Highlight)
		}
		return st.Render(s)
	}

	arrow, arrowStyle := "⇡", lipgloss.NewStyle().Foreground(p.Primary)
	if it.Dir == transfer.Download {
		arrow, arrowStyle = "⇣", lipgloss.NewStyle().Foreground(p.Accent)
	}
	glyph, glyphStyle := arrow, arrowStyle
	switch it.State {
	case transfer.Done:
		glyph, glyphStyle = "✓", m.styles.Success
	case transfer.Failed:
		glyph, glyphStyle = "✗", m.styles.Error
	case transfer.Cancelled:
		glyph, glyphStyle = "⊘", m.styles.Muted
	case transfer.Queued:
		glyph, glyphStyle = "·", m.styles.Subtle
	case transfer.Scanning, transfer.Active:
	}

	name := it.Name
	if it.IsDir {
		name += "/"
	}
	nameWidth := min(max(iw/4, 12), 32)
	head := paint(lipgloss.NewStyle(), " ") + paint(glyphStyle, glyph) + paint(lipgloss.NewStyle(), " ") +
		paint(m.styles.Text.Bold(it.State == transfer.Active), theme.Fit(name, nameWidth)) + paint(lipgloss.NewStyle(), " ")
	rest := max(iw-lipgloss.Width(head), 0)

	var tail string
	switch it.State {
	case transfer.Done:
		tail = paint(m.styles.Muted, doneDetail(it))
	case transfer.Failed:
		msg := "failed"
		if it.Err != nil {
			msg = it.Err.Error()
		}
		tail = paint(m.styles.Error, msg)
	case transfer.Cancelled:
		tail = paint(m.styles.Muted, "cancelled")
	case transfer.Queued:
		tail = paint(m.styles.Subtle, "waiting…")
	case transfer.Scanning, transfer.Active:
		tail = m.progressDetail(it, rest, paint)
	}
	return head + theme.Fit(tail, rest)
}

func (m Model) progressDetail(it transfer.ItemSnapshot, width int, paint func(lipgloss.Style, string) string) string {
	p := m.styles.Palette
	barWidth := min(max(width/3, 8), 30)
	bar := progress.New(
		progress.WithColors(p.Primary, p.Secondary),
		progress.WithoutPercentage(),
		progress.WithWidth(barWidth),
		progress.WithFillCharacters('█', '░'),
	)
	bar.EmptyColor = p.Subtle
	pct := it.Percent()

	info := []string{padLeft(strconv.Itoa(int(pct*100))+"%", 4)}
	if it.State == transfer.Scanning && it.Total == 0 {
		info = []string{"scanning…"}
	}
	info = append(info, units.FormatBytes(it.Done)+"/"+units.FormatBytes(it.Total))
	if it.Rate > 0 {
		info = append(info, units.FormatRate(it.Rate))
	}
	if eta := it.ETA(); eta > 0 {
		info = append(info, units.FormatDuration(eta)+" left")
	}
	if it.IsDir {
		files := strconv.Itoa(it.FilesOK) + "/" + strconv.Itoa(it.Files) + " files"
		if it.Current != "" {
			files += " · " + it.Current
		}
		info = append(info, files)
	}
	return bar.ViewAs(pct) + paint(m.styles.Muted, " "+strings.Join(info, "  "))
}

func doneDetail(it transfer.ItemSnapshot) string {
	parts := []string{units.FormatBytes(it.Done) + " in " + units.FormatDuration(it.Elapsed)}
	if it.Rate > 0 && it.Elapsed > time.Second {
		parts = append(parts, units.FormatRate(it.Rate))
	}
	if it.IsDir {
		parts = append(parts, strconv.Itoa(it.FilesOK)+" files")
	}
	if it.Skipped > 0 {
		parts = append(parts, strconv.Itoa(it.Skipped)+" skipped")
	}
	return strings.Join(parts, " · ")
}

func padLeft(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

// HelpKeys returns bindings for the current state.
func (m Model) HelpKeys() []key.Binding {
	if m.dialog != nil {
		return conflictHelp
	}
	return []key.Binding{m.keys.Cancel, m.keys.Retry, m.keys.Clear, m.keys.CancelAll}
}

// Dialog renders the conflict prompt, or "" when none is open.
func (m Model) Dialog(maxWidth int) string {
	if m.dialog == nil {
		return ""
	}
	return m.dialog.view(m.styles, min(66, maxWidth-2))
}

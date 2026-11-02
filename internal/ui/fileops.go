package ui

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// previewLimit is how much of a file the preview reads.
const previewLimit = 256 * 1024

// opDoneMsg reports a finished file operation so the pane can refresh.
type opDoneMsg struct {
	fsys       vfs.FS
	dir        string // directory to refresh
	selectName string
	notice     string
	err        error
}

// previewMsg delivers file contents for the preview.
type previewMsg struct {
	name      string
	size      int64
	data      []byte
	truncated bool
	err       error
}

// focusedPane returns the pane with focus, or nil when focus is elsewhere
// (the connect screen or the queue).
func (m *Model) focusedPane() *pane.Model {
	switch {
	case m.focus == leftSide:
		return &m.local
	case m.focus == rightSide && m.conn != nil:
		return &m.remote
	}
	return nil
}

// targets is what a file operation acts on: the marked entries, or the one
// under the cursor.
func targets(p *pane.Model) []vfs.Entry {
	if marked := p.Marked(); len(marked) > 0 {
		return marked
	}
	if e, ok := p.Selected(); ok {
		return []vfs.Entry{e}
	}
	return nil
}

func (m Model) startNewFolder() (tea.Model, tea.Cmd) {
	p := m.focusedPane()
	fsys, dir := p.FS(), p.Path()
	m.input = newInputDialog("New folder", "Create a folder in "+theme.TruncateLeft(vfs.Sanitize(dir), 34), "", m.styles,
		func(name string) tea.Cmd {
			return func() tea.Msg {
				target := fsys.Join(dir, name)
				err := fsys.Mkdir(target)
				if err != nil {
					if _, serr := fsys.Stat(target); serr == nil {
						err = fmt.Errorf("%s already exists", name)
					}
				}
				return opDoneMsg{fsys: fsys, dir: dir, selectName: name, err: err, notice: "created " + name + "/"}
			}
		})
	return m, m.input.init()
}

func (m Model) startRename() (tea.Model, tea.Cmd) {
	p := m.focusedPane()
	e, ok := p.Selected()
	if !ok {
		return m, nil
	}
	fsys, dir, old := p.FS(), p.Path(), e.Name
	m.input = newInputDialog("Rename", "New name for "+vfs.Sanitize(old), old, m.styles,
		func(name string) tea.Cmd {
			return func() tea.Msg {
				if name == old {
					return nil
				}
				to := fsys.Join(dir, name)
				// Rename replaces existing files on most systems; never clobber.
				if _, err := fsys.Stat(to); err == nil {
					return opDoneMsg{fsys: fsys, dir: dir, err: fmt.Errorf("%s already exists", name)}
				} else if !errors.Is(err, fs.ErrNotExist) {
					return opDoneMsg{fsys: fsys, dir: dir, err: err}
				}
				err := fsys.Rename(fsys.Join(dir, old), to)
				return opDoneMsg{fsys: fsys, dir: dir, selectName: name, err: err, notice: "renamed to " + name}
			}
		})
	return m, m.input.init()
}

func (m Model) startDelete() (tea.Model, tea.Cmd) {
	p := m.focusedPane()
	entries := targets(p)
	if len(entries) == 0 {
		return m, nil
	}
	fsys, dir := p.FS(), p.Path()

	var size int64
	dirs := 0
	for _, e := range entries {
		if e.IsDir && !e.IsLink {
			dirs++
		} else {
			size += e.Size
		}
	}
	what := vfs.Sanitize(entries[0].Name)
	if len(entries) > 1 {
		what = strconv.Itoa(len(entries)) + " items"
	}
	body := "Permanently delete " + what + " from " + fsys.Label() + "?"
	if size > 0 {
		body += " (" + units.FormatBytes(size) + ")"
	}
	if dirs > 0 {
		body += "\n\nFolders are deleted with everything inside them."
	}
	body += "\nThis can't be undone."

	m.confirm = &confirmDialog{title: "Delete?", body: body, yes: func(m Model) (tea.Model, tea.Cmd) {
		if p := m.focusedPane(); p != nil {
			p.ClearMarks()
		}
		run := func() tea.Msg {
			var errs []error
			for _, e := range entries {
				if err := vfs.RemoveAll(fsys, fsys.Join(dir, e.Name)); err != nil {
					errs = append(errs, err)
				}
			}
			log.Debug("deleted", "count", len(entries), "dir", dir, "errors", len(errs))
			return opDoneMsg{fsys: fsys, dir: dir, err: errors.Join(errs...), notice: "deleted " + what}
		}
		return m, tea.Batch(run, m.setNotice(noticeInfo, "deleting "+what+"…", noticeTTL))
	}}
	return m, nil
}

func (m Model) onOpDone(msg opDoneMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, p := range []*pane.Model{&m.local, &m.remote} {
		if p.FS() == msg.fsys && p.Path() == msg.dir {
			cmds = append(cmds, p.Navigate(msg.dir, msg.selectName))
		}
	}
	if msg.err != nil {
		cmds = append(cmds, m.setNotice(noticeError, vfs.Sanitize(msg.err.Error()), errorNoticeTTL))
	} else if msg.notice != "" {
		cmds = append(cmds, m.setNotice(noticeInfo, "✓ "+vfs.Sanitize(msg.notice), noticeTTL))
	}
	return m, tea.Batch(cmds...)
}

func loadPreview(f pane.OpenFileMsg) tea.Cmd {
	return func() tea.Msg {
		data, trunc, err := vfs.ReadHead(f.FS, f.Path, previewLimit)
		return previewMsg{name: f.Entry.Name, size: f.Entry.Size, data: data, truncated: trunc, err: err}
	}
}

// inputDialog asks for a single name.
type inputDialog struct {
	title, prompt string
	input         textinput.Model
	err           string
	submit        func(string) tea.Cmd
}

func newInputDialog(title, prompt, value string, s theme.Styles, submit func(string) tea.Cmd) *inputDialog {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.SetValue(value)
	ti.CursorEnd()
	ti.SetWidth(44)
	p := s.Palette
	st := textinput.StyleState{Text: s.Text, Placeholder: s.Subtle, Suggestion: s.Subtle, Prompt: lipgloss.NewStyle().Foreground(p.Primary)}
	ti.SetStyles(textinput.Styles{Focused: st, Blurred: st, Cursor: textinput.CursorStyle{Color: p.Accent, Shape: tea.CursorBlock, Blink: true}})
	return &inputDialog{title: title, prompt: prompt, input: ti, submit: submit}
}

func (d *inputDialog) init() tea.Cmd { return d.input.Focus() }

// update returns done=true when the dialog should close, plus any command to
// run (the operation itself, on submit).
func (d *inputDialog) update(msg tea.Msg) (done bool, cmd tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "esc":
			return true, nil
		case "enter":
			name := strings.TrimSpace(d.input.Value())
			if err := vfs.CheckName(name); err != nil {
				d.err = "That name can't be used: it's empty, \".\", \"..\", or contains / \\ or control characters."
				return false, nil
			}
			return true, d.submit(name)
		}
	}
	d.err = ""
	d.input, cmd = d.input.Update(msg)
	return false, cmd
}

func (d *inputDialog) view(s theme.Styles, width int) string {
	p := s.Palette
	inner := max(min(width, 60)-6, 20)
	lines := []string{
		theme.Gradient(lipgloss.NewStyle().Bold(true), d.title, p.Primary, p.Secondary),
		"",
		lipgloss.NewStyle().Width(inner).Foreground(p.Muted).Render(d.prompt),
		"",
		d.input.View(),
	}
	if d.err != "" {
		lines = append(lines, "", lipgloss.NewStyle().Width(inner).Foreground(p.Error).Render(d.err))
	}
	lines = append(lines, "", s.Subtle.Render("enter confirm • esc cancel"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForegroundBlend(p.Primary, p.Secondary).
		Padding(1, 2).Width(inner + 6).Render(strings.Join(lines, "\n"))
}

// previewDialog shows the start of a file, scrollable.
type previewDialog struct {
	title string
	info  string
	vp    viewport.Model
}

func newPreview(msg previewMsg, s theme.Styles, width, height int) *previewDialog {
	w := max(min(width-8, 120), 20)
	h := max(height-10, 5)
	vp := viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))

	info := units.FormatBytes(msg.size)
	switch {
	case msg.err != nil:
		vp.SetContent(s.Error.Render(vfs.Sanitize(msg.err.Error())))
	case vfs.LooksBinary(msg.data):
		info += " · binary, showing hex of the first 4 KiB"
		vp.SetContent(s.Muted.Render(strings.TrimRight(hex.Dump(msg.data[:min(len(msg.data), 4096)]), "\n")))
	case len(msg.data) == 0:
		vp.SetContent(s.Subtle.Render("(empty file)"))
	default:
		if msg.truncated {
			info += " · showing the first " + units.FormatBytes(previewLimit)
		}
		text := strings.ReplaceAll(string(msg.data), "\r\n", "\n")
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		numW := len(strconv.Itoa(len(lines)))
		out := make([]string, len(lines))
		for i, l := range lines {
			// Sanitize: file contents could carry terminal escape sequences.
			num := s.Subtle.Render(fmt.Sprintf("%*d ", numW, i+1))
			out[i] = num + s.Text.Render(vfs.Sanitize(l))
		}
		vp.SetContentLines(out)
	}
	return &previewDialog{title: vfs.Sanitize(msg.name), info: info, vp: vp}
}

var previewHelp = []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "scroll")),
	key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "page")),
	key.NewBinding(key.WithKeys("esc", "q", "v"), key.WithHelp("esc", "close")),
}

func (d *previewDialog) update(msg tea.Msg) (done bool, cmd tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "esc", "q", "v", "enter":
			return true, nil
		case "g", "home":
			d.vp.GotoTop()
			return false, nil
		case "G", "end":
			d.vp.GotoBottom()
			return false, nil
		}
	}
	d.vp, cmd = d.vp.Update(msg)
	return false, cmd
}

func (d *previewDialog) view(s theme.Styles) string {
	p := s.Palette
	title := theme.Gradient(lipgloss.NewStyle().Bold(true), d.title, p.Primary, p.Secondary)
	pos := ""
	if total := d.vp.TotalLineCount(); total > d.vp.Height() {
		pos = fmt.Sprintf("%d%%", int(d.vp.ScrollPercent()*100))
	}
	footer := s.Subtle.Render(d.info)
	if pos != "" {
		footer += s.Subtle.Render(" · " + pos)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForegroundBlend(p.Primary, p.Secondary).
		Padding(0, 1).Render(title + "\n\n" + d.vp.View() + "\n\n" + footer)
}

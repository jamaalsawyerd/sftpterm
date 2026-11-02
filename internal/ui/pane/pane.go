// Package pane implements a scrollable, filterable directory listing that works
// with any vfs.FS.
package pane

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// SortMode orders entries within a listing. Directories always come first.
type SortMode int

// Sort modes, in the order the sort key cycles through them.
const (
	SortName SortMode = iota
	SortSize
	SortModified
	numSortModes
)

func (s SortMode) String() string {
	switch s {
	case SortSize:
		return "size"
	case SortModified:
		return "modified"
	default:
		return "name"
	}
}

// LoadedMsg carries the result of reading a directory back to the pane that
// asked for it.
type LoadedMsg struct {
	paneID     int64
	seq        int
	path       string
	entries    []vfs.Entry
	err        error
	selectName string
}

// ErrorMsg reports an error the app should surface to the user.
type ErrorMsg struct{ Err error }

// OpenFileMsg asks the app to open (preview) a file.
type OpenFileMsg struct {
	FS    vfs.FS
	Path  string
	Entry vfs.Entry
}

// Options configure a new pane.
type Options struct {
	ShowHidden bool
	Icons      string
	Styles     theme.Styles
}

type row struct {
	entry  vfs.Entry
	parent bool // the ".." row
}

var nextID atomic.Int64

// Model is a directory listing for one file system.
type Model struct {
	id   int64
	fsys vfs.FS
	home string

	path    string
	all     []vfs.Entry
	rows    []row // visible rows: hidden/filter applied, sorted
	loaded  bool  // a listing has been received at least once
	loading bool
	err     error // load error, shown when there is no listing
	seq     int   // drops responses to superseded loads

	cursor, offset int
	marked         map[string]struct{}

	focused    bool
	showHidden bool
	sortMode   SortMode
	filter     textinput.Model
	filtering  bool

	width, height int
	styles        theme.Styles
	icons         IconSet
	keys          KeyMap
}

// New creates a pane for fsys. Call Navigate to load its first directory.
func New(fsys vfs.FS, opts Options) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "type to filter"

	m := Model{
		id:         nextID.Add(1),
		fsys:       fsys,
		marked:     map[string]struct{}{},
		showHidden: opts.ShowHidden,
		icons:      Icons(opts.Icons),
		keys:       DefaultKeyMap(),
		filter:     ti,
	}
	m.home, _ = fsys.Home()
	m.SetStyles(opts.Styles)
	return m
}

// SetStyles applies a new theme.
func (m *Model) SetStyles(s theme.Styles) {
	m.styles = s
	p := s.Palette
	state := textinput.StyleState{
		Text:        s.Text,
		Placeholder: s.Subtle,
		Suggestion:  s.Subtle,
		Prompt:      lipgloss.NewStyle().Foreground(p.Primary).Bold(true),
	}
	m.filter.SetStyles(textinput.Styles{
		Focused: state,
		Blurred: state,
		Cursor:  textinput.CursorStyle{Color: p.Primary, Shape: tea.CursorBlock, Blink: true},
	})
}

// SetSize sets the outer size of the pane, including its border.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.filter.SetWidth(max(width-6, 1))
	m.ensureVisible()
}

// SetFocused marks the pane as the one receiving keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// Focused reports whether the pane receives keys.
func (m Model) Focused() bool { return m.focused }

// Filtering reports whether the filter input is capturing keys.
func (m Model) Filtering() bool { return m.filtering }

// Path is the directory currently shown.
func (m Model) Path() string { return m.path }

// FS is the file system the pane browses.
func (m Model) FS() vfs.FS { return m.fsys }

// KeyMap returns the pane's bindings, for help display.
func (m Model) KeyMap() KeyMap { return m.keys }

// Selected returns the entry under the cursor, if any (never "..").
func (m Model) Selected() (vfs.Entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].parent {
		return vfs.Entry{}, false
	}
	return m.rows[m.cursor].entry, true
}

// Marked returns the marked entries in the current directory.
func (m Model) Marked() []vfs.Entry {
	var out []vfs.Entry
	for _, e := range m.all {
		if _, ok := m.marked[e.Name]; ok {
			out = append(out, e)
		}
	}
	return out
}

// ClearMarks unmarks everything, e.g. after the marked files were queued.
func (m *Model) ClearMarks() { clear(m.marked) }

// Navigate loads path in the background. When it arrives, the cursor is placed
// on selectName if present (or kept in place when refreshing the same path).
func (m *Model) Navigate(path, selectName string) tea.Cmd {
	m.seq++
	m.loading = true
	id, seq, fsys := m.id, m.seq, m.fsys
	return func() tea.Msg {
		entries, err := fsys.ReadDir(path)
		return LoadedMsg{paneID: id, seq: seq, path: path, entries: entries, err: err, selectName: selectName}
	}
}

// Update handles messages addressed to this pane. Key presses are ignored
// unless the pane is focused.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case LoadedMsg:
		if msg.paneID != m.id || msg.seq != m.seq {
			return m, nil
		}
		return m.handleLoaded(msg)
	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.handleKey(msg)
	}
	if m.filtering { // cursor blink etc.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleLoaded(msg LoadedMsg) (Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		err := m.describeError(msg.path, msg.err)
		if !m.loaded {
			m.path, m.err = msg.path, err
		}
		return m, func() tea.Msg { return ErrorMsg{Err: err} }
	}

	keep := msg.selectName
	if m.loaded && msg.path == m.path {
		if keep == "" {
			keep = m.currentName()
		}
		present := map[string]bool{}
		for _, e := range msg.entries {
			present[e.Name] = true
		}
		for name := range m.marked {
			if !present[name] {
				delete(m.marked, name)
			}
		}
	} else {
		clear(m.marked)
		m.resetFilter()
		m.offset = 0
	}

	m.path, m.all, m.loaded, m.err = msg.path, msg.entries, true, nil
	m.rebuild(keep)
	return m, nil
}

// describeError names just the directory rather than its full path, so the
// reason ("permission denied") survives truncation in the status bar.
func (m Model) describeError(path string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return fmt.Errorf("can't open %s: %w", vfs.Sanitize(m.fsys.Base(path)), err)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.PageUp):
		m.move(-max(m.listHeight(), 1))
	case key.Matches(msg, m.keys.PageDown):
		m.move(max(m.listHeight(), 1))
	case key.Matches(msg, m.keys.Top):
		m.cursor = 0
	case key.Matches(msg, m.keys.Bottom):
		m.cursor = max(len(m.rows)-1, 0)
	case key.Matches(msg, m.keys.Open):
		cmd = m.open()
	case key.Matches(msg, m.keys.Parent):
		cmd = m.parent()
	case key.Matches(msg, m.keys.Home):
		if m.home != "" {
			cmd = m.Navigate(m.home, "")
		}
	case key.Matches(msg, m.keys.Refresh):
		if m.path != "" {
			cmd = m.Navigate(m.path, m.currentName())
		}
	case key.Matches(msg, m.keys.Mark):
		m.toggleMark()
		m.move(1)
	case key.Matches(msg, m.keys.MarkAll):
		m.toggleMarkAll()
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		cmd = m.filter.Focus()
	case key.Matches(msg, m.keys.ClearFilter):
		if m.filter.Value() != "" {
			cur := m.currentName()
			m.resetFilter()
			m.rebuild(cur)
		}
	case key.Matches(msg, m.keys.Hidden):
		m.showHidden = !m.showHidden
		m.rebuild(m.currentName())
	case key.Matches(msg, m.keys.Sort):
		m.sortMode = (m.sortMode + 1) % numSortModes
		m.rebuild(m.currentName())
	}
	m.ensureVisible()
	return m, cmd
}

func (m Model) updateFilter(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, m.keys.ClearFilter):
		cur := m.currentName()
		m.resetFilter()
		m.rebuild(cur)
	case key.Matches(msg, m.keys.ApplyFilter):
		m.filtering = false
		m.filter.Blur()
	case msg.String() == "up" || msg.String() == "ctrl+p":
		m.move(-1)
	case msg.String() == "down" || msg.String() == "ctrl+n":
		m.move(1)
	default:
		before := m.filter.Value()
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.rebuild("")
		}
	}
	m.ensureVisible()
	return m, cmd
}

func (m *Model) resetFilter() {
	m.filtering = false
	m.filter.Blur()
	m.filter.SetValue("")
}

func (m *Model) open() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	switch {
	case r.parent:
		return m.parent()
	case r.entry.IsDir:
		return m.Navigate(m.fsys.Join(m.path, r.entry.Name), "")
	}
	msg := OpenFileMsg{FS: m.fsys, Path: m.fsys.Join(m.path, r.entry.Name), Entry: r.entry}
	return func() tea.Msg { return msg }
}

func (m *Model) parent() tea.Cmd {
	if m.path == "" || m.fsys.IsRoot(m.path) {
		return nil
	}
	return m.Navigate(m.fsys.Dir(m.path), m.fsys.Base(m.path))
}

func (m *Model) toggleMark() {
	e, ok := m.Selected()
	if !ok {
		return
	}
	if _, marked := m.marked[e.Name]; marked {
		delete(m.marked, e.Name)
	} else {
		m.marked[e.Name] = struct{}{}
	}
}

// toggleMarkAll marks every visible entry, or unmarks them all if they are
// already marked.
func (m *Model) toggleMarkAll() {
	all := true
	for _, r := range m.rows {
		if _, ok := m.marked[r.entry.Name]; !r.parent && !ok {
			all = false
			break
		}
	}
	for _, r := range m.rows {
		switch {
		case r.parent:
		case all:
			delete(m.marked, r.entry.Name)
		default:
			m.marked[r.entry.Name] = struct{}{}
		}
	}
}

func (m Model) currentName() string {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].entry.Name
	}
	return ""
}

// rebuild recomputes the visible rows and puts the cursor on keep if it is
// still visible.
func (m *Model) rebuild(keep string) {
	q := strings.ToLower(m.filter.Value())
	rows := make([]row, 0, len(m.all)+1)
	if m.path != "" && !m.fsys.IsRoot(m.path) && q == "" {
		rows = append(rows, row{parent: true, entry: vfs.Entry{Name: "..", IsDir: true}})
	}
	start := len(rows)
	for _, e := range m.all {
		if (!m.showHidden && e.Hidden()) || (q != "" && !strings.Contains(strings.ToLower(e.Name), q)) {
			continue
		}
		rows = append(rows, row{entry: e})
	}
	slices.SortStableFunc(rows[start:], func(a, b row) int { return m.compare(a.entry, b.entry) })
	m.rows = rows

	m.cursor = 0
	if keep != "" {
		if i := slices.IndexFunc(m.rows, func(r row) bool { return r.entry.Name == keep }); i >= 0 {
			m.cursor = i
		}
	}
	m.ensureVisible()
}

func (m Model) compare(a, b vfs.Entry) int {
	if a.IsDir != b.IsDir {
		if a.IsDir {
			return -1
		}
		return 1
	}
	byName := cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.Name, b.Name))
	switch m.sortMode {
	case SortSize:
		return cmp.Or(cmp.Compare(b.Size, a.Size), byName)
	case SortModified:
		return cmp.Or(b.ModTime.Compare(a.ModTime), byName)
	default:
		return byName
	}
}

func (m *Model) move(delta int) {
	m.cursor = max(min(m.cursor+delta, len(m.rows)-1), 0)
}

// listHeight is the number of rows visible: the border takes two lines and
// the footer one.
func (m Model) listHeight() int { return max(m.height-3, 0) }

func (m *Model) ensureVisible() {
	h := m.listHeight()
	m.cursor = max(min(m.cursor, len(m.rows)-1), 0)
	if h == 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(min(m.offset, len(m.rows)-h), 0)
}

// View renders the pane at exactly its configured size.
func (m Model) View() string {
	return m.styles.Frame(m.fsys.Label(), m.displayPath(), m.body(), m.width, m.height, m.focused)
}

func (m Model) displayPath() string {
	if m.home == "" || !strings.HasPrefix(m.path, m.home) {
		return vfs.Sanitize(m.path)
	}
	rest := m.path[len(m.home):]
	if rest == "" || rest[0] == '/' || rest[0] == '\\' {
		return "~" + vfs.Sanitize(rest)
	}
	return vfs.Sanitize(m.path)
}

func (m Model) body() string {
	iw := max(m.width-2, 0)
	h := m.listHeight()
	lines := make([]string, 0, h+1)

	switch {
	case !m.loaded && m.err != nil:
		msg := lipgloss.NewStyle().Width(max(iw-2, 1)).Render(vfs.Sanitize(m.err.Error()))
		for l := range strings.SplitSeq(msg, "\n") {
			lines = append(lines, " "+m.styles.Error.Render(l))
		}
	case !m.loaded:
		lines = append(lines, " "+m.styles.Muted.Render("loading…"))
	default:
		end := min(m.offset+h, len(m.rows))
		for i := m.offset; i < end; i++ {
			lines = append(lines, m.renderRow(m.rows[i], i == m.cursor, iw))
		}
		if len(lines) < h && !slices.ContainsFunc(m.rows, func(r row) bool { return !r.parent }) {
			empty := "empty directory"
			if m.filter.Value() != "" {
				empty = "no matches"
			}
			lines = append(lines, "   "+m.styles.Subtle.Render(empty))
		}
	}

	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, m.footer(iw)), "\n")
}

const (
	sizeWidth = 9
	dateWidth = 12
)

func (m Model) renderRow(r row, selected bool, iw int) string {
	p := m.styles.Palette
	paint := func(s lipgloss.Style, text string) string {
		if selected && m.focused {
			s = s.Background(p.Highlight)
		}
		return s.Render(text)
	}
	plain := lipgloss.NewStyle()

	e := r.entry
	_, isMarked := m.marked[e.Name]
	isMarked = isMarked && !r.parent

	icon, iconStyle, nameStyle := m.icons.File, m.styles.Muted, m.styles.Text
	name := vfs.Sanitize(e.Name) // names may come from an untrusted server
	switch {
	case r.parent:
		icon = m.icons.Parent
		nameStyle = m.styles.Muted
	case e.IsDir:
		icon, iconStyle = m.icons.Dir, lipgloss.NewStyle().Foreground(p.Primary)
		nameStyle = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)
		name += "/"
	case e.IsLink:
		icon, iconStyle, nameStyle = m.icons.Link, m.styles.Muted.Foreground(p.Accent), m.styles.Text.Foreground(p.Accent)
	case e.Executable():
		icon, iconStyle, nameStyle = m.icons.Exec, m.styles.Success, m.styles.Success
	case e.Hidden():
		nameStyle = m.styles.Muted
	}
	if e.IsLink && e.IsDir {
		icon = m.icons.Link
	}
	mark := strings.Repeat(" ", m.icons.Width())
	if isMarked {
		mark = m.icons.Mark
		nameStyle = m.styles.Warning.Bold(true)
	}
	if selected && !m.focused {
		nameStyle = nameStyle.Underline(true)
	}

	right := ""
	if iw >= 30 {
		size := ""
		if !e.IsDir && !r.parent {
			size = units.FormatBytes(e.Size)
		}
		right += " " + padLeft(size, sizeWidth)
	}
	if iw >= 52 {
		right += " " + padLeft(formatTime(e.ModTime), dateWidth)
	}

	fixed := 1 + m.icons.Width() + 1 + m.icons.Width() + 1 // " mark icon "
	nameWidth := max(iw-fixed-lipgloss.Width(right)-1, 1)

	return paint(plain, " ") +
		paint(m.styles.Warning, mark) +
		paint(plain, " ") +
		paint(iconStyle, icon) +
		paint(plain, " ") +
		paint(nameStyle, theme.Fit(name, nameWidth)) +
		paint(m.styles.Muted, right) +
		paint(plain, " ")
}

func (m Model) footer(iw int) string {
	if m.filtering {
		return " " + m.filter.View()
	}

	var left []string
	switch {
	case m.loading && m.loaded:
		left = append(left, m.styles.Muted.Render("loading…"))
	case m.loaded:
		n, hidden := 0, 0
		for _, e := range m.all {
			if e.Hidden() && !m.showHidden {
				hidden++
			}
		}
		for _, r := range m.rows {
			if !r.parent {
				n++
			}
		}
		left = append(left, m.styles.Muted.Render(plural(n, "item")))
		if q := m.filter.Value(); q != "" {
			left = append(left, lipgloss.NewStyle().Foreground(m.styles.Palette.Primary).Render("/"+q))
		} else if hidden > 0 {
			left = append(left, m.styles.Subtle.Render(strconv.Itoa(hidden)+" hidden"))
		}
	}
	if marked := m.Marked(); len(marked) > 0 {
		var total int64
		for _, e := range marked {
			if !e.IsDir {
				total += e.Size
			}
		}
		left = append(left, m.styles.Warning.Render(strconv.Itoa(len(marked))+" marked ("+units.FormatBytes(total)+")"))
	}

	l := " " + strings.Join(left, m.styles.Subtle.Render(" · "))
	r := m.styles.Subtle.Render("sort: "+m.sortMode.String()) + " "
	if gap := iw - lipgloss.Width(l) - lipgloss.Width(r); gap > 0 {
		return l + strings.Repeat(" ", gap) + r
	}
	return l
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func padLeft(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if t.Year() == time.Now().Year() {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}

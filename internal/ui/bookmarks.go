package ui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/state"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// bookmarkPicker lists the current connection's bookmarks.
type bookmarkPicker struct {
	key    string
	items  []state.Bookmark
	cursor int
}

var bookmarkHelp = []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "go")),
	key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
}

func (m Model) connKey() string {
	if m.conn == nil {
		return ""
	}
	return m.conn.Target.String()
}

func (m Model) startAddBookmark() (tea.Model, tea.Cmd) {
	if m.conn == nil || m.store == nil {
		return m, m.setNotice(noticeWarn, "connect to a server to bookmark folders", noticeTTL)
	}
	key, local, remote := m.connKey(), m.local.Path(), m.remote.Path()
	name := m.remote.FS().Base(remote)
	if remote == "/" || name == "" {
		name = "root"
	}
	m.input = newInputDialog("Bookmark",
		"Save this pair of folders:\n  "+theme.TruncateLeft(vfs.Sanitize(local), 44)+"\n  "+theme.TruncateLeft(vfs.Sanitize(remote), 44),
		vfs.Sanitize(name), m.styles,
		func(name string) tea.Cmd {
			return func() tea.Msg {
				return addBookmarkMsg{key: key, b: state.Bookmark{Name: name, Local: local, Remote: remote}}
			}
		})
	m.input.allowAnyName = true
	return m, m.input.init()
}

type addBookmarkMsg struct {
	key string
	b   state.Bookmark
}

func (m Model) onAddBookmark(msg addBookmarkMsg) (tea.Model, tea.Cmd) {
	m.store.AddBookmark(msg.key, msg.b)
	m.connect.Refresh()
	if err := m.store.Save(); err != nil {
		return m, m.setNotice(noticeError, "saving bookmarks: "+err.Error(), errorNoticeTTL)
	}
	return m, m.setNotice(noticeInfo, "★ bookmarked "+msg.b.Name, noticeTTL)
}

func (m Model) openBookmarks() (tea.Model, tea.Cmd) {
	if m.conn == nil || m.store == nil {
		return m, m.setNotice(noticeWarn, "bookmarks are per connection: connect first", noticeTTL)
	}
	items := m.store.Bookmarks(m.connKey())
	if len(items) == 0 {
		return m, m.setNotice(noticeInfo, "no bookmarks for this connection yet; press b to add one", noticeTTL)
	}
	m.bookmarks = &bookmarkPicker{key: m.connKey(), items: items}
	return m, nil
}

func (m Model) updateBookmarks(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.bookmarks
	switch msg.String() {
	case "esc", "q", "'":
		m.bookmarks = nil
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, len(p.items)-1)
	case "d", "delete":
		b := p.items[p.cursor]
		m.store.RemoveBookmark(p.key, b.Name)
		m.connect.Refresh()
		p.items = m.store.Bookmarks(p.key)
		p.cursor = min(p.cursor, len(p.items)-1)
		if len(p.items) == 0 {
			m.bookmarks = nil
		}
		cmd := m.setNotice(noticeInfo, "removed bookmark "+vfs.Sanitize(b.Name), noticeTTL)
		if err := m.store.Save(); err != nil {
			cmd = m.setNotice(noticeError, "saving bookmarks: "+err.Error(), errorNoticeTTL)
		}
		return m, cmd
	case "enter":
		b := p.items[p.cursor]
		m.bookmarks = nil
		cmds := []tea.Cmd{
			m.remote.NavigateOr(b.Remote, "", ""),
			m.setNotice(noticeInfo, "★ "+vfs.Sanitize(b.Name), noticeTTL),
		}
		if st, err := os.Stat(b.Local); err == nil && st.IsDir() {
			cmds = append(cmds, m.local.Navigate(b.Local, ""))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (p *bookmarkPicker) view(s theme.Styles, width int) string {
	pal := s.Palette
	inner := max(min(width, 80)-6, 30)
	nameW := 4
	for _, b := range p.items {
		nameW = max(nameW, min(lipgloss.Width(b.Name), 20))
	}
	pathW := max((inner-nameW-6)/2, 8)
	lines := []string{theme.Gradient(lipgloss.NewStyle().Bold(true), "★ Bookmarks", pal.Primary, pal.Secondary), ""}
	for i, b := range p.items {
		marker, nameStyle := "  ", s.Text
		if i == p.cursor {
			marker, nameStyle = lipgloss.NewStyle().Foreground(pal.Primary).Render("▸ "), lipgloss.NewStyle().Foreground(pal.Primary).Bold(true)
		}
		lines = append(lines, marker+nameStyle.Render(theme.Fit(vfs.Sanitize(b.Name), nameW))+"  "+
			s.Muted.Render(theme.TruncateLeft(vfs.Sanitize(b.Local), pathW))+s.Subtle.Render(" ⇄ ")+
			s.Muted.Render(theme.TruncateLeft(vfs.Sanitize(b.Remote), pathW)))
	}
	lines = append(lines, "", s.Subtle.Render("enter go • d delete • esc close"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForegroundBlend(pal.Primary, pal.Secondary).
		Padding(1, 2).Render(strings.Join(lines, "\n"))
}

package queue

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/transfer"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

var conflictHelp = []key.Binding{
	key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "overwrite")),
	key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "skip")),
	key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "keep both")),
	key.NewBinding(key.WithKeys("O", "S", "K"), key.WithHelp("O/S/K", "same for all")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel item")),
}

type conflictDialog struct {
	ev transfer.ConflictEvent
}

// update answers the engine on a valid key and reports whether the dialog
// is finished.
func (d *conflictDialog) update(msg tea.KeyPressMsg) bool {
	var res transfer.Resolution
	switch msg.String() {
	case "o":
		res = transfer.Resolution{Action: transfer.Overwrite}
	case "O":
		res = transfer.Resolution{Action: transfer.Overwrite, ApplyToAll: true}
	case "s":
		res = transfer.Resolution{Action: transfer.Skip}
	case "S":
		res = transfer.Resolution{Action: transfer.Skip, ApplyToAll: true}
	case "k":
		res = transfer.Resolution{Action: transfer.KeepBoth}
	case "K":
		res = transfer.Resolution{Action: transfer.KeepBoth, ApplyToAll: true}
	case "esc":
		res = transfer.Resolution{Action: transfer.CancelItem}
	default:
		return false
	}
	d.ev.Reply <- res
	return true
}

func (d *conflictDialog) view(s theme.Styles, width int) string {
	p := s.Palette
	inner := max(width-6, 20)
	ev := d.ev

	describe := func(label string, e entryInfo, newer bool) string {
		line := s.Muted.Render(padRight(label, 13)) + s.Text.Render(units.FormatBytes(e.size)) +
			s.Muted.Render(" · "+e.mtime.Local().Format("Jan _2 2006 15:04"))
		if newer {
			line += s.Success.Render("  newer")
		}
		return line
	}
	src := entryInfo{ev.Src.Size, ev.Src.ModTime}
	dst := entryInfo{ev.Dst.Size, ev.Dst.ModTime}
	incoming := "uploading"
	if ev.Dir == transfer.Download {
		incoming = "downloading"
	}

	hint := func(k, label string) string {
		return lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render(k) + s.Text.Render(" "+label)
	}
	all := "Shift+key applies to this whole batch"
	if ev.Pending > 0 {
		all += " (" + strconv.Itoa(ev.Pending) + " more files)"
	}

	body := strings.Join([]string{
		theme.Gradient(lipgloss.NewStyle().Bold(true), "Already exists", p.Warning, p.Primary),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(p.Text).Render(theme.Fit(pathBase(ev.Path), inner)),
		s.Muted.Render("in " + theme.TruncateLeft(pathDir(ev.Path), inner-3)),
		"",
		describe(incoming, src, newer(src.mtime, dst.mtime)),
		describe("existing", dst, newer(dst.mtime, src.mtime)),
		"",
		hint("o", "overwrite") + "   " + hint("s", "skip") + "   " + hint("k", "keep both") + "   " + hint("esc", "cancel"),
		s.Subtle.Render(all),
	}, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForegroundBlend(p.Warning, p.Primary).
		Padding(1, 2).
		Render(body)
}

type entryInfo struct {
	size  int64
	mtime time.Time
}

func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// newer reports whether a is meaningfully newer than b. Times are compared
// at one-second precision because SFTP doesn't carry sub-second times.
func newer(a, b time.Time) bool { return a.Truncate(time.Second).After(b.Truncate(time.Second)) }

// pathBase and pathDir split a local or remote path for display.
func pathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func pathDir(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i > 0 {
		return p[:i]
	}
	return p
}

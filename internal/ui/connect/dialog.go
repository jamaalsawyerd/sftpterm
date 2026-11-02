package connect

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

const dialogWidth = 64

// dialog is a modal prompt shown over the whole screen during a connection
// attempt: either a text/secret question or a host key confirmation.
type dialog struct {
	title string
	body  string

	// question
	input    textinput.Model
	askReply chan<- answer

	// host key
	trust    bool // which button is selected
	keyReply chan<- bool
}

func newAskDialog(ev askEvent, s theme.Styles) *dialog {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.SetWidth(dialogWidth - 10)
	if ev.q.Secret {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	p := s.Palette
	st := textinput.StyleState{Text: s.Text, Placeholder: s.Subtle, Suggestion: s.Subtle, Prompt: lipgloss.NewStyle().Foreground(p.Primary)}
	ti.SetStyles(textinput.Styles{Focused: st, Blurred: st, Cursor: textinput.CursorStyle{Color: p.Accent, Shape: tea.CursorBlock, Blink: true}})
	ti.Focus()
	return &dialog{title: ev.q.Title, body: ev.q.Prompt, input: ti, askReply: ev.reply}
}

func newHostKeyDialog(ev hostKeyEvent) *dialog {
	i := ev.info
	body := "sftpterm hasn't seen " + i.Alias + " (" + i.Hostname + ") before.\n\n" +
		i.KeyType + " key fingerprint:\n" + i.Fingerprint + "\n\n" +
		"Check that this matches the server before trusting it.\n" +
		"It will be saved to " + i.File + "."
	return &dialog{title: "New host", body: body, trust: false, keyReply: ev.reply}
}

func (d *dialog) isHostKey() bool { return d.keyReply != nil }

// init returns the command that starts the input cursor blinking.
func (d *dialog) init() tea.Cmd {
	if d.isHostKey() {
		return nil
	}
	return textinput.Blink
}

// update handles a key and reports whether the dialog is finished.
func (d *dialog) update(msg tea.Msg, k keyMap) (done bool, cmd tea.Cmd) {
	km, isKey := msg.(tea.KeyPressMsg)
	if d.isHostKey() {
		if !isKey {
			return false, nil
		}
		switch {
		case km.String() == "y":
			d.keyReply <- true
			return true, nil
		case km.String() == "n", key.Matches(km, k.Cancel):
			d.keyReply <- false
			return true, nil
		case km.String() == "enter":
			d.keyReply <- d.trust
			return true, nil
		case km.String() == "left", km.String() == "right", km.String() == "tab", km.String() == "h", km.String() == "l":
			d.trust = !d.trust
		}
		return false, nil
	}

	if isKey {
		switch {
		case km.String() == "enter":
			d.askReply <- answer{text: d.input.Value(), ok: true}
			return true, nil
		case key.Matches(km, k.Cancel):
			d.askReply <- answer{}
			return true, nil
		}
	}
	d.input, cmd = d.input.Update(msg)
	return false, cmd
}

// cancel answers the dialog negatively, e.g. when the attempt is abandoned.
func (d *dialog) cancel() {
	if d.isHostKey() {
		d.keyReply <- false
	} else {
		d.askReply <- answer{}
	}
}

func (d *dialog) view(s theme.Styles, width int) string {
	p := s.Palette
	inner := max(width-6, 10)
	title := theme.Gradient(lipgloss.NewStyle().Bold(true), d.title, p.Primary, p.Secondary)
	body := lipgloss.NewStyle().Width(inner).Foreground(p.Text).Render(d.body)

	var bottom string
	if d.isHostKey() {
		btn := func(label string, active bool, c lipgloss.Style) string {
			st := lipgloss.NewStyle().Padding(0, 2).Foreground(p.Muted).Background(p.Highlight)
			if active {
				st = c.Padding(0, 2).Bold(true)
			}
			return st.Render(label)
		}
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			btn("Trust (y)", d.trust, lipgloss.NewStyle().Foreground(p.Highlight).Background(p.Success)),
			"  ",
			btn("Cancel (n)", !d.trust, lipgloss.NewStyle().Foreground(p.Highlight).Background(p.Error)),
		)
		bottom = lipgloss.PlaceHorizontal(inner, lipgloss.Center, bottom)
	} else {
		bottom = d.input.View() + "\n\n" + s.Subtle.Render("enter submit • esc cancel")
	}

	content := strings.Join([]string{title, "", body, "", bottom}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForegroundBlend(p.Primary, p.Secondary).
		Padding(1, 2).
		Render(content)
}

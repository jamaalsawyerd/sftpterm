package ui

import (
	"charm.land/bubbles/v2/key"
)

// keyMap holds the global key bindings. Screen-specific bindings live with
// their screens.
type keyMap struct {
	SwitchPane key.Binding
	Disconnect key.Binding
	Theme      key.Binding
	Help       key.Binding
	Quit       key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		SwitchPane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch side")),
		Disconnect: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "disconnect")),
		Theme:      key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "next theme")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more keys")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// helpKeys is a help.KeyMap assembled for whatever currently has focus.
type helpKeys struct {
	short []key.Binding
	full  [][]key.Binding
}

// ShortHelp implements help.KeyMap.
func (h helpKeys) ShortHelp() []key.Binding { return h.short }

// FullHelp implements help.KeyMap.
func (h helpKeys) FullHelp() [][]key.Binding {
	if h.full == nil {
		return [][]key.Binding{h.short}
	}
	return h.full
}

func (m Model) helpKeys() helpKeys {
	g := m.keys
	if m.connect.HasDialog() {
		return helpKeys{short: m.connect.HelpKeys()}
	}

	paneFocused := m.focus == leftSide || m.conn != nil
	if !paneFocused {
		if m.connect.CapturesKeys() || m.connect.Connecting() {
			return helpKeys{short: m.connect.HelpKeys()}
		}
		short := append([]key.Binding{g.SwitchPane}, m.connect.HelpKeys()...)
		return helpKeys{short: append(short, g.Theme, g.Help, g.Quit)}
	}

	p := m.local.KeyMap()
	filtering := m.local.Filtering()
	if m.focus == rightSide {
		p = m.remote.KeyMap()
		filtering = m.remote.Filtering()
	}
	if filtering {
		return helpKeys{short: []key.Binding{p.ApplyFilter, p.ClearFilter}}
	}

	conn := []key.Binding{g.SwitchPane}
	if m.conn != nil {
		conn = append(conn, g.Disconnect)
	}
	return helpKeys{
		short: []key.Binding{g.SwitchPane, p.Open, p.Parent, p.Mark, p.Filter, g.Theme, g.Help, g.Quit},
		full: [][]key.Binding{
			{p.Up, p.Down, p.PageUp, p.PageDown, p.Top, p.Bottom},
			{p.Open, p.Parent, p.Home, p.Refresh},
			{p.Mark, p.MarkAll, p.Filter, p.ClearFilter},
			{p.Hidden, p.Sort, g.Theme},
			append(conn, g.Help, g.Quit),
		},
	}
}

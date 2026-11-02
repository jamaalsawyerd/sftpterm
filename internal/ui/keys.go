package ui

import (
	"charm.land/bubbles/v2/key"
)

// keyMap holds the global key bindings. Screen-specific bindings live with
// their screens.
type keyMap struct {
	SwitchPane   key.Binding
	Copy         key.Binding
	MoreParallel key.Binding
	LessParallel key.Binding
	Settings     key.Binding
	NewFolder    key.Binding
	Rename       key.Binding
	Delete       key.Binding
	View         key.Binding
	Disconnect   key.Binding
	Theme        key.Binding
	Help         key.Binding
	Quit         key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		SwitchPane:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch side")),
		Copy:         key.NewBinding(key.WithKeys("c", "f5"), key.WithHelp("c", "copy to other side")),
		MoreParallel: key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+/-", "parallel transfers")),
		LessParallel: key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "fewer transfers")),
		Settings:     key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "speed limits")),
		NewFolder:    key.NewBinding(key.WithKeys("n", "f7"), key.WithHelp("n", "new folder")),
		Rename:       key.NewBinding(key.WithKeys("r", "f2"), key.WithHelp("r", "rename")),
		Delete:       key.NewBinding(key.WithKeys("d", "delete", "f8"), key.WithHelp("d", "delete")),
		View:         key.NewBinding(key.WithKeys("v", "f3"), key.WithHelp("v", "preview")),
		Disconnect:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "disconnect")),
		Theme:        key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "next theme")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more keys")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
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
	switch {
	case m.connect.HasDialog():
		return helpKeys{short: m.connect.HelpKeys()}
	case m.queue.HasDialog():
		return helpKeys{short: m.queue.HelpKeys()}
	case m.confirm != nil:
		return helpKeys{short: confirmHelp}
	case m.preview != nil:
		return helpKeys{short: previewHelp}
	case m.input != nil:
		return helpKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	case m.settings != nil:
		return helpKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next/apply")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	case m.focus == queueSide:
		q := m.queue.KeyMap()
		return helpKeys{
			short: append([]key.Binding{g.SwitchPane}, append(m.queue.HelpKeys(), g.MoreParallel, g.Settings, g.Help, g.Quit)...),
			full:  [][]key.Binding{{q.Up, q.Down}, m.queue.HelpKeys(), {g.MoreParallel, g.LessParallel, g.Settings}, {g.SwitchPane, g.Help, g.Quit}},
		}
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
	short := []key.Binding{g.SwitchPane, p.Open, p.Mark}
	if m.conn != nil {
		short = append(short, g.Copy)
	}
	return helpKeys{
		short: append(short, p.Filter, g.Theme, g.Help, g.Quit),
		full: [][]key.Binding{
			{p.Up, p.Down, p.PageUp, p.PageDown, p.Top, p.Bottom},
			{p.Open, p.Parent, p.Home, p.Refresh},
			{p.Mark, p.MarkAll, p.Filter, p.ClearFilter},
			{g.Copy, g.View, g.NewFolder, g.Rename, g.Delete},
			{g.MoreParallel, g.Settings},
			{p.Hidden, p.Sort, g.Theme},
			append(conn, g.Help, g.Quit),
		},
	}
}

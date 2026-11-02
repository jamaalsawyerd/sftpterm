package ui

import (
	"charm.land/bubbles/v2/key"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
)

// keyMap holds the global key bindings. Screen-specific bindings live with
// their screens.
type keyMap struct {
	Theme key.Binding
	Help  key.Binding
	Quit  key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Theme: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "next theme")),
		Help:  key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more keys")),
		Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Theme, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Theme},
		{k.Help, k.Quit},
	}
}

// helpKeys combines global and pane bindings for the help bar.
type helpKeys struct {
	app       keyMap
	pane      pane.KeyMap
	filtering bool
}

// ShortHelp implements help.KeyMap.
func (h helpKeys) ShortHelp() []key.Binding {
	if h.filtering {
		return []key.Binding{h.pane.ApplyFilter, h.pane.ClearFilter}
	}
	return []key.Binding{h.pane.Open, h.pane.Parent, h.pane.Mark, h.pane.Filter, h.app.Theme, h.app.Help, h.app.Quit}
}

// FullHelp implements help.KeyMap.
func (h helpKeys) FullHelp() [][]key.Binding {
	if h.filtering {
		return [][]key.Binding{h.ShortHelp()}
	}
	p := h.pane
	return [][]key.Binding{
		{p.Up, p.Down, p.PageUp, p.PageDown, p.Top, p.Bottom},
		{p.Open, p.Parent, p.Home, p.Refresh},
		{p.Mark, p.MarkAll, p.Filter, p.ClearFilter},
		{p.Hidden, p.Sort, h.app.Theme},
		{h.app.Help, h.app.Quit},
	}
}

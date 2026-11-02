package ui

import "charm.land/bubbles/v2/key"

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

package pane

import "charm.land/bubbles/v2/key"

// KeyMap holds the key bindings for browsing a pane.
type KeyMap struct {
	Up, Down, PageUp, PageDown, Top, Bottom key.Binding

	Open, Parent, Home, Refresh key.Binding

	Mark, MarkAll key.Binding

	Filter, ApplyFilter, ClearFilter key.Binding

	Hidden, Sort key.Binding
}

// DefaultKeyMap returns the default pane bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		Top:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),

		Open:    key.NewBinding(key.WithKeys("enter", "right", "l"), key.WithHelp("enter", "open")),
		Parent:  key.NewBinding(key.WithKeys("backspace", "left", "h"), key.WithHelp("⌫", "up a dir")),
		Home:    key.NewBinding(key.WithKeys("~"), key.WithHelp("~", "home")),
		Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),

		Mark:    key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "mark")),
		MarkAll: key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "mark all")),

		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ApplyFilter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep filter")),
		ClearFilter: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),

		Hidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden files")),
		Sort:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
	}
}

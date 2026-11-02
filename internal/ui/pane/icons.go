package pane

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
)

// IconSet holds the glyphs used in a listing.
type IconSet struct {
	Dir, File, Link, Exec, Parent, Mark string
}

// Icons returns the glyphs for a config.Icons value, defaulting to Unicode.
func Icons(name string) IconSet {
	var s IconSet
	switch name {
	case config.IconsNerd:
		s = IconSet{Dir: "", File: "", Link: "", Exec: "", Parent: "", Mark: ""}
	case config.IconsEmoji:
		s = IconSet{Dir: "📁", File: "📄", Link: "🔗", Exec: "🚀", Parent: "🔼", Mark: "✅"}
	case config.IconsASCII:
		s = IconSet{Dir: "/", File: " ", Link: "@", Exec: "*", Parent: "^", Mark: "+"}
	default:
		s = IconSet{Dir: "▸", File: "·", Link: "↪", Exec: "∗", Parent: "↰", Mark: "●"}
	}
	return s.padded()
}

// padded right-pads every glyph to the widest one so columns line up.
func (s IconSet) padded() IconSet {
	fields := []*string{&s.Dir, &s.File, &s.Link, &s.Exec, &s.Parent, &s.Mark}
	w := 0
	for _, f := range fields {
		w = max(w, lipgloss.Width(*f))
	}
	for _, f := range fields {
		*f += strings.Repeat(" ", w-lipgloss.Width(*f))
	}
	return s
}

// Width is the display width of every glyph in the set.
func (s IconSet) Width() int { return lipgloss.Width(s.Dir) }

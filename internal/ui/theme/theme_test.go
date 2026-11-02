package theme

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPalettesAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range palettes {
		if p.Name == "" {
			t.Fatal("palette with empty name")
		}
		if seen[p.Name] {
			t.Errorf("duplicate palette %q", p.Name)
		}
		seen[p.Name] = true
		for field, c := range map[string]any{
			"Primary": p.Primary, "Secondary": p.Secondary, "Accent": p.Accent,
			"Text": p.Text, "Muted": p.Muted, "Subtle": p.Subtle, "Border": p.Border,
			"Highlight": p.Highlight, "Success": p.Success, "Warning": p.Warning, "Error": p.Error,
		} {
			if c == nil {
				t.Errorf("%s: %s is nil", p.Name, field)
			}
		}
	}
}

func TestDefaultThemeExists(t *testing.T) {
	// Keep in sync with config.DefaultTheme.
	if _, ok := Get("charm"); !ok {
		t.Fatal("default theme \"charm\" is missing")
	}
}

func TestNextCyclesThroughAll(t *testing.T) {
	name := Names()[0]
	for range len(palettes) {
		name = Next(name).Name
	}
	if name != Names()[0] {
		t.Errorf("cycling %d times ended on %q, want %q", len(palettes), name, Names()[0])
	}
	if Next("does-not-exist").Name != Names()[0] {
		t.Error("Next of an unknown theme should return the first theme")
	}
}

func TestGradientPreservesWidth(t *testing.T) {
	p, _ := Get("charm")
	for _, s := range []string{"", "x", "✦ sftpterm", "────────────"} {
		got := Gradient(lipgloss.NewStyle().Bold(true), s, p.Primary, p.Secondary)
		if w, want := lipgloss.Width(got), lipgloss.Width(s); w != want {
			t.Errorf("Gradient(%q) width = %d, want %d", s, w, want)
		}
	}
}

func ansiStrip(s string) string { return ansi.Strip(s) }

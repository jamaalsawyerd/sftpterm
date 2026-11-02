package pane

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// fixture builds:
//
//	root/
//	  .dotfile      (3 bytes)
//	  alpha.txt     (10 bytes, oldest)
//	  Beta.txt      (300 bytes)
//	  zeta.bin      (20 bytes, newest)
//	  docs/
//	    readme.md
//	  music/
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name string, size int, age time.Duration) {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	write(".dotfile", 3, time.Hour)
	write("alpha.txt", 10, 3*time.Hour)
	write("Beta.txt", 300, 2*time.Hour)
	write("zeta.bin", 20, time.Minute)
	for _, d := range []string{"docs", "music"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pin directory times (after writing into them) so sorting by date is
	// deterministic on every OS: docs is newer than music.
	for d, age := range map[string]time.Duration{"docs": 4 * time.Hour, "music": 5 * time.Hour} {
		mt := time.Now().Add(-age)
		if err := os.Chtimes(filepath.Join(root, d), mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newPane(t *testing.T, dir string) Model {
	t.Helper()
	p, _ := theme.Get("charm")
	m := New(vfs.NewLocal(), Options{Styles: theme.NewStyles(p)})
	m.SetFocused(true)
	m.SetSize(60, 12)
	return run(t, m, m.Navigate(dir, ""))
}

// run executes cmd synchronously and feeds resulting messages back into the
// pane until nothing is left (ignoring cursor blinks and ErrorMsgs).
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		switch msg.(type) {
		case LoadedMsg:
			m, cmd = m.Update(msg)
		default:
			return m
		}
	}
	return m
}

func press(t *testing.T, m Model, k string) Model {
	t.Helper()
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+a":
		msg = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	default:
		r := []rune(k)
		if len(r) != 1 {
			t.Fatalf("unsupported key %q", k)
		}
		msg = tea.KeyPressMsg{Code: r[0], Text: k}
	}
	m, cmd := m.Update(msg)
	return run(t, m, cmd)
}

func names(m Model) []string {
	out := make([]string, len(m.rows))
	for i, r := range m.rows {
		out[i] = r.entry.Name
	}
	return out
}

func assertNames(t *testing.T, m Model, want ...string) {
	t.Helper()
	if got := names(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestListingSortsDirsFirstAndHidesDotfiles(t *testing.T) {
	m := newPane(t, fixture(t))
	assertNames(t, m, "..", "docs", "music", "alpha.txt", "Beta.txt", "zeta.bin")

	m = press(t, m, ".")
	assertNames(t, m, "..", "docs", "music", ".dotfile", "alpha.txt", "Beta.txt", "zeta.bin")
}

func TestSortModes(t *testing.T) {
	m := newPane(t, fixture(t))
	m = press(t, m, "s")
	assertNames(t, m, "..", "docs", "music", "Beta.txt", "zeta.bin", "alpha.txt")
	m = press(t, m, "s")
	if m.sortMode != SortModified {
		t.Fatalf("sort mode = %v", m.sortMode)
	}
	assertNames(t, m, "..", "docs", "music", "zeta.bin", "Beta.txt", "alpha.txt")
	m = press(t, m, "s")
	if m.sortMode != SortName {
		t.Fatalf("sort mode should wrap to name, got %v", m.sortMode)
	}
}

func TestOpenDirAndReturnToParentRestoresCursor(t *testing.T) {
	root := fixture(t)
	m := newPane(t, root)
	m = press(t, m, "j") // docs
	m = press(t, m, "enter")
	if m.Path() != filepath.Join(root, "docs") {
		t.Fatalf("path = %q", m.Path())
	}
	assertNames(t, m, "..", "readme.md")

	m = press(t, m, "backspace")
	if m.Path() != root {
		t.Fatalf("path = %q, want %q", m.Path(), root)
	}
	if got := m.currentName(); got != "docs" {
		t.Errorf("cursor on %q, want docs", got)
	}
}

func TestEnterOnParentRowGoesUp(t *testing.T) {
	root := fixture(t)
	m := newPane(t, filepath.Join(root, "docs"))
	m = press(t, m, "enter") // cursor starts on ".."
	if m.Path() != root {
		t.Errorf("path = %q, want %q", m.Path(), root)
	}
}

func TestFilter(t *testing.T) {
	m := newPane(t, fixture(t))
	m = press(t, m, "/")
	if !m.Filtering() {
		t.Fatal("expected filtering mode")
	}
	for _, r := range "txt" {
		m = press(t, m, string(r))
	}
	assertNames(t, m, "alpha.txt", "Beta.txt")

	// Typing "q" while filtering must go to the input, not anywhere else.
	m = press(t, m, "q")
	assertNames(t, m)

	m = press(t, m, "backspace")
	m = press(t, m, "enter") // keep the filter
	if m.Filtering() {
		t.Error("enter should leave filtering mode")
	}
	assertNames(t, m, "alpha.txt", "Beta.txt")

	m = press(t, m, "esc")
	assertNames(t, m, "..", "docs", "music", "alpha.txt", "Beta.txt", "zeta.bin")
}

func TestMarking(t *testing.T) {
	m := newPane(t, fixture(t))
	m = press(t, m, "space") // on "..": ignored, cursor moves to docs
	m = press(t, m, "space") // mark docs
	m = press(t, m, "j")     // skip music
	m = press(t, m, "space") // mark alpha.txt
	if got := len(m.Marked()); got != 2 {
		t.Fatalf("marked %d, want 2", got)
	}

	m = press(t, m, "ctrl+a")
	if got := len(m.Marked()); got != 5 {
		t.Fatalf("after mark all: %d marked, want 5", got)
	}
	m = press(t, m, "ctrl+a")
	if got := len(m.Marked()); got != 0 {
		t.Fatalf("after unmark all: %d marked, want 0", got)
	}
}

func TestMarksClearedOnDirectoryChange(t *testing.T) {
	m := newPane(t, fixture(t))
	m = press(t, m, "j")
	m = press(t, m, "space")
	m = press(t, m, "k")
	m = press(t, m, "k")
	m = press(t, m, "enter") // into docs
	m = press(t, m, "backspace")
	if len(m.Marked()) != 0 {
		t.Error("marks should be cleared after leaving the directory")
	}
}

func TestRefreshKeepsMarksAndCursor(t *testing.T) {
	root := fixture(t)
	m := newPane(t, root)
	m = press(t, m, "G") // zeta.bin
	m = press(t, m, "space")
	m = press(t, m, "k") // Beta.txt
	if err := os.WriteFile(filepath.Join(root, "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.Navigate(m.Path(), "")
	m = run(t, m, cmd)
	if m.currentName() != "Beta.txt" {
		t.Errorf("cursor on %q, want Beta.txt", m.currentName())
	}
	if len(m.Marked()) != 1 {
		t.Errorf("marks lost on refresh")
	}
	assertNames(t, m, "..", "docs", "music", "alpha.txt", "Beta.txt", "new.txt", "zeta.bin")
}

func TestStaleLoadIgnored(t *testing.T) {
	root := fixture(t)
	m := newPane(t, root)
	stale := m.Navigate(filepath.Join(root, "docs"), "")
	fresh := m.Navigate(filepath.Join(root, "music"), "")
	m, _ = m.Update(stale())
	if m.Path() != root {
		t.Fatalf("stale load applied: path = %q", m.Path())
	}
	m, _ = m.Update(fresh())
	if m.Path() != filepath.Join(root, "music") {
		t.Fatalf("fresh load not applied: path = %q", m.Path())
	}
}

func TestLoadErrorKeepsListingAndReportsError(t *testing.T) {
	root := fixture(t)
	m := newPane(t, root)
	nope := filepath.Join(root, "nope")
	m, cmd := m.Update(LoadedMsg{paneID: m.id, seq: m.seq, path: nope, err: &fs.PathError{Op: "open", Path: nope, Err: os.ErrPermission}})
	if m.Path() != root {
		t.Errorf("path changed on error: %q", m.Path())
	}
	if cmd == nil {
		t.Fatal("expected an ErrorMsg command")
	}
	em, ok := cmd().(ErrorMsg)
	if !ok || !errors.Is(em.Err, os.ErrPermission) {
		t.Fatalf("got %#v", cmd())
	}
	if want := "can't open nope: permission denied"; em.Err.Error() != want {
		t.Errorf("error = %q, want %q", em.Err, want)
	}
}

func TestInitialLoadErrorShownInPane(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	m := newPane(t, missing)
	if !errors.Is(m.err, fs.ErrNotExist) {
		t.Fatalf("pane error = %v, want not-exist", m.err)
	}
	if !strings.Contains(m.View(), "can't open missing") {
		t.Errorf("error not shown:\n%s", m.View())
	}
	// Going up from a broken directory still works.
	m = press(t, m, "backspace")
	if m.Path() != filepath.Dir(missing) {
		t.Errorf("path = %q, want %q", m.Path(), filepath.Dir(missing))
	}
}

func TestUnfocusedPaneIgnoresKeys(t *testing.T) {
	m := newPane(t, fixture(t))
	m.SetFocused(false)
	m = press(t, m, "j")
	if m.cursor != 0 {
		t.Error("unfocused pane moved its cursor")
	}
}

func TestScrollingKeepsCursorVisible(t *testing.T) {
	root := t.TempDir()
	for i := range 50 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newPane(t, root)
	h := m.listHeight()
	for range 30 {
		m = press(t, m, "j")
	}
	if m.cursor < m.offset || m.cursor >= m.offset+h {
		t.Errorf("cursor %d not within [%d, %d)", m.cursor, m.offset, m.offset+h)
	}
	if !strings.Contains(m.View(), m.currentName()) {
		t.Error("selected row not rendered")
	}
	m = press(t, m, "g")
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("g: cursor=%d offset=%d", m.cursor, m.offset)
	}
}

func TestViewExactSize(t *testing.T) {
	root := fixture(t)
	for _, icons := range []string{"unicode", "nerd", "emoji", "ascii"} {
		for _, sz := range [][2]int{{60, 12}, {25, 8}, {100, 30}, {12, 4}} {
			t.Run(fmt.Sprintf("%s/%dx%d", icons, sz[0], sz[1]), func(t *testing.T) {
				p, _ := theme.Get("charm")
				m := New(vfs.NewLocal(), Options{Styles: theme.NewStyles(p), Icons: icons, ShowHidden: true})
				m.SetFocused(true)
				m.SetSize(sz[0], sz[1])
				m = run(t, m, m.Navigate(root, ""))
				m = press(t, m, "space")
				m = press(t, m, "space")
				out := m.View()
				if h := lipgloss.Height(out); h != sz[1] {
					t.Errorf("height = %d, want %d", h, sz[1])
				}
				for i, line := range strings.Split(out, "\n") {
					if w := lipgloss.Width(line); w != sz[0] {
						t.Errorf("line %d width = %d, want %d: %q", i, w, sz[0], line)
					}
				}
			})
		}
	}
}

func TestDisplayPathAbbreviatesHome(t *testing.T) {
	m := Model{home: "/home/jsd"}
	for path, want := range map[string]string{
		"/home/jsd":          "~",
		"/home/jsd/Projects": "~/Projects",
		"/home/jsdx":         "/home/jsdx",
		"/srv":               "/srv",
	} {
		m.path = path
		if got := m.displayPath(); got != want {
			t.Errorf("displayPath(%q) = %q, want %q", path, got, want)
		}
	}
}

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
)

// localHarness starts the app on dir with the local pane focused.
func localHarness(t *testing.T, files map[string]string) (*harness, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(Options{Config: config.Default(), StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)
	h.until("listing", func(m Model) bool { return m.local.Path() == dir })
	h.key(tea.KeyPressMsg{Code: tea.KeyTab})
	if h.m.focus != leftSide {
		t.Fatal("expected local focus")
	}
	return h, dir
}

func (h *harness) enter() { h.key(tea.KeyPressMsg{Code: tea.KeyEnter}) }

func (h *harness) selectName(name string) {
	h.t.Helper()
	h.typeText("g")
	for range 50 {
		if e, ok := h.m.local.Selected(); ok && e.Name == name {
			return
		}
		h.typeText("j")
	}
	h.t.Fatalf("%s not found in listing", name)
}

func TestNewFolder(t *testing.T) {
	h, dir := localHarness(t, nil)
	h.typeText("n")
	if h.m.input == nil {
		t.Fatal("n should open the new folder dialog")
	}
	h.typeText("a/b")
	h.enter()
	if h.m.input == nil || !strings.Contains(ansi.Strip(h.m.View().Content), "can't be used") {
		t.Fatal("a name with a slash should be refused in the dialog")
	}
	for range 3 {
		h.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	h.typeText("photos")
	h.enter()
	h.until("folder created and selected", func(m Model) bool {
		e, ok := m.local.Selected()
		return ok && e.Name == "photos" && e.IsDir
	})
	if st, err := os.Stat(filepath.Join(dir, "photos")); err != nil || !st.IsDir() {
		t.Fatal("folder not created on disk")
	}
}

func TestRenameNeverClobbers(t *testing.T) {
	h, dir := localHarness(t, map[string]string{"a.txt": "A", "b.txt": "B"})
	h.selectName("a.txt")
	h.typeText("r")
	if h.m.input == nil || h.m.input.input.Value() != "a.txt" {
		t.Fatal("rename should prefill the current name")
	}
	for range len("a.txt") {
		h.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	h.typeText("b.txt")
	h.enter()
	h.until("error notice", func(m Model) bool { return m.noticeKind == noticeError && strings.Contains(m.notice, "exists") })
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "B" {
		t.Fatal("rename overwrote an existing file")
	}

	h.typeText("r")
	for range len("a.txt") {
		h.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	h.typeText("c.txt")
	h.enter()
	h.until("renamed", func(m Model) bool {
		e, ok := m.local.Selected()
		return ok && e.Name == "c.txt"
	})
	if b, _ := os.ReadFile(filepath.Join(dir, "c.txt")); string(b) != "A" {
		t.Fatal("renamed file has the wrong contents")
	}
}

func TestDeleteAsksAndKeepsMarksOnNo(t *testing.T) {
	h, dir := localHarness(t, map[string]string{"x.txt": "x", "y.txt": "y"})
	if err := os.MkdirAll(filepath.Join(dir, "tree", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "deep", "f"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	h.until("tree listed", func(m Model) bool { return strings.Contains(ansi.Strip(m.local.View()), "tree/") })

	h.selectName("tree")
	h.typeText(" ")
	h.selectName("x.txt")
	h.typeText(" ")
	h.typeText("d")
	if h.m.confirm == nil || !strings.Contains(h.m.confirm.body, "2 items") {
		t.Fatalf("expected a delete confirmation for 2 items, got %+v", h.m.confirm)
	}
	h.typeText("n")
	if len(h.m.local.Marked()) != 2 {
		t.Fatal("answering no should keep the marks")
	}
	h.typeText("d")
	h.typeText("y")
	h.until("deleted", func(m Model) bool {
		_, e1 := os.Stat(filepath.Join(dir, "tree"))
		_, e2 := os.Stat(filepath.Join(dir, "x.txt"))
		return os.IsNotExist(e1) && os.IsNotExist(e2) && !strings.Contains(ansi.Strip(m.local.View()), "tree/")
	})
	if _, err := os.Stat(filepath.Join(dir, "y.txt")); err != nil {
		t.Fatal("an unmarked file was deleted")
	}
}

func TestPreviewSanitizesContent(t *testing.T) {
	evil := "line one\n\x1b]52;c;ZXZpbA==\x07clipboard hijack\n\x1b[2Jclear screen\n"
	h, _ := localHarness(t, map[string]string{"notes.txt": evil, "blob.bin": "PK\x03\x04\x00\x00\x00binary"})
	h.selectName("notes.txt")
	h.enter()
	h.until("preview open", func(m Model) bool { return m.preview != nil })
	view := h.m.preview.view(h.m.styles)
	if strings.Contains(view, "\x1b]52") || strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x07") {
		t.Fatal("preview passed a terminal escape sequence through")
	}
	if !strings.Contains(ansi.Strip(view), "clipboard hijack") {
		t.Error("printable text missing from preview")
	}
	h.typeText("q") // closes the preview, does not quit
	if h.m.preview != nil {
		t.Fatal("q should close the preview")
	}

	h.selectName("blob.bin")
	h.typeText("v")
	h.until("binary preview", func(m Model) bool { return m.preview != nil })
	if !strings.Contains(ansi.Strip(h.m.preview.view(h.m.styles)), "binary") {
		t.Error("binary file not labelled as binary")
	}
}

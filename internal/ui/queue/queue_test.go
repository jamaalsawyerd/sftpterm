package queue

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/transfer"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

func styles() theme.Styles {
	p, _ := theme.Get("charm")
	return theme.NewStyles(p)
}

func TestRowsFitWidth(t *testing.T) {
	items := []transfer.ItemSnapshot{
		{Name: "a-very-long-file-name-that-goes-on-and-on.iso", State: transfer.Active, Done: 50, Total: 100, Rate: 1 << 20},
		{Name: "photos", IsDir: true, State: transfer.Active, Done: 10, Total: 100, Files: 40, FilesOK: 4, Current: "IMG_0001.jpg", Dir: transfer.Download},
		{Name: "done.txt", State: transfer.Done, Done: 1000, Total: 1000, Elapsed: 2 * time.Second, Rate: 500},
		{Name: "bad.bin", State: transfer.Failed, Err: errors.New(strings.Repeat("permission denied ", 10))},
		{Name: "later.bin", State: transfer.Queued},
		{Name: "stop.bin", State: transfer.Cancelled},
		{Name: "seventh", State: transfer.Queued},
	}
	for _, w := range []int{40, 80, 160} {
		m := New(nil, styles())
		m.snap = transfer.Snapshot{Items: items, Active: 2, Queued: 2}
		m.SetWidth(w)
		m.SetFocused(true)
		out := m.View()
		if got, want := lipgloss.Height(out), MaxRows+2; got != want || m.Height() != want {
			t.Errorf("width %d: height = %d (Height() %d), want %d", w, got, m.Height(), want)
		}
		for i, line := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(line); lw != w {
				t.Errorf("width %d: line %d is %d wide", w, i, lw)
			}
		}
	}
}

func TestHiddenWhenEmpty(t *testing.T) {
	m := New(nil, styles())
	if m.Height() != 0 || m.View() != "" || m.HasItems() {
		t.Error("an empty queue should take no space")
	}
}

func TestConflictKeys(t *testing.T) {
	tests := map[string]transfer.Resolution{
		"o":   {Action: transfer.Overwrite},
		"S":   {Action: transfer.Skip, ApplyToAll: true},
		"k":   {Action: transfer.KeepBoth},
		"K":   {Action: transfer.KeepBoth, ApplyToAll: true},
		"esc": {Action: transfer.CancelItem},
	}
	for k, want := range tests {
		reply := make(chan transfer.Resolution, 1)
		d := &conflictDialog{ev: transfer.ConflictEvent{Path: "/srv/a.txt", Reply: reply, Src: vfs.Entry{Size: 1}, Dst: vfs.Entry{Size: 2}}}
		msg := tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		if k == "esc" {
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		}
		if !d.update(msg) {
			t.Errorf("%q did not answer", k)
			continue
		}
		if got := <-reply; got != want {
			t.Errorf("%q answered %+v, want %+v", k, got, want)
		}
	}
	d := &conflictDialog{ev: transfer.ConflictEvent{Reply: make(chan transfer.Resolution, 1)}}
	if d.update(tea.KeyPressMsg{Code: 'q', Text: "q"}) {
		t.Error("an unrelated key answered the dialog")
	}
}

func TestNewerIgnoresSubSecondDifferences(t *testing.T) {
	a := time.Date(2026, 1, 1, 12, 0, 0, 900_000_000, time.UTC)
	b := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if newer(a, b) || newer(b, a) {
		t.Error("sub-second differences should not count as newer")
	}
	if !newer(a.Add(time.Second), b) {
		t.Error("a full second later should count as newer")
	}
}

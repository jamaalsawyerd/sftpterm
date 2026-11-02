package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/state"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

// openDir moves the given pane's cursor to name and opens it.
func (h *harness) openDir(remote bool, name string) {
	h.t.Helper()
	h.typeText("g")
	for range 50 {
		p := h.m.local
		if remote {
			p = h.m.remote
		}
		if e, ok := p.Selected(); ok && e.Name == name {
			before := p.Path()
			h.enter()
			h.until("opened "+name, func(m Model) bool {
				q := m.local
				if remote {
					q = m.remote
				}
				return q.Path() != before
			})
			return
		}
		h.typeText("j")
	}
	h.t.Fatalf("%s not found", name)
}

func TestBookmarksAndLastLocation(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "bookmarks.json")
	store, err := state.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	h, srv, local := connectedWith(t, nil, func(o *Options) { o.State = store })
	for _, d := range []string{filepath.Join(local, "music"), filepath.Join(srv.Root, "media")} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	h.until("music listed", func(m Model) bool { return strings.Contains(ansi.Strip(m.local.View()), "music/") })
	h.openDir(false, "music")
	h.key(tea.KeyPressMsg{Code: tea.KeyTab})
	h.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	h.until("media listed", func(m Model) bool { return strings.Contains(ansi.Strip(m.remote.View()), "media/") })
	h.openDir(true, "media")

	// b saves both directories under a name defaulting to the remote dir.
	h.typeText("b")
	if h.m.input == nil || h.m.input.input.Value() != "media" {
		t.Fatalf("bookmark dialog = %+v", h.m.input)
	}
	h.enter()
	h.until("bookmark saved", func(m Model) bool { return strings.Contains(m.notice, "bookmarked media") })
	key := h.m.connKey()
	bms := store.Bookmarks(key)
	if len(bms) != 1 || bms[0].Local != filepath.Join(local, "music") || bms[0].Remote != filepath.ToSlash(filepath.Join(srv.Root, "media")) {
		t.Fatalf("bookmarks = %+v", bms)
	}
	if reloaded, _ := state.Load(storePath); len(reloaded.Bookmarks(key)) != 1 {
		t.Fatal("bookmark not written to disk")
	}

	// Wander off, then jump back with '.
	h.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	h.key(tea.KeyPressMsg{Code: tea.KeyTab}) // → queue is empty → local
	h.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	h.until("both moved up", func(m Model) bool { return m.local.Path() == local && m.remote.Path() != bms[0].Remote })
	h.typeText("'")
	if h.m.bookmarks == nil {
		t.Fatal("' should open the bookmark list")
	}
	h.enter()
	h.until("jumped to bookmark", func(m Model) bool {
		return m.local.Path() == bms[0].Local && m.remote.Path() == bms[0].Remote
	})

	// Disconnecting remembers where we were, and the connect screen lists
	// the host (as recent) with its bookmark.
	h.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	last, ok := store.Last(key)
	if !ok || last.Remote != bms[0].Remote || last.Local != bms[0].Local {
		t.Fatalf("last location = %+v, %v", last, ok)
	}
	screen := ansi.Strip(h.m.View().Content)
	if !strings.Contains(screen, "recent") || !strings.Contains(screen, "↳ media") {
		t.Errorf("connect screen doesn't list the recent host and bookmark:\n%s", screen)
	}

	// A fresh session to the same host opens where we left off.
	h.m.local.Navigate(local, "")
	m, err := New(Options{
		Config: testConfig(), StartDir: local, SSHConfig: sshx.EmptySSHConfig(), State: store,
		Destination: &sshx.Destination{User: srv.User, Host: srv.Host, Port: srv.Port},
	})
	if err != nil {
		t.Fatal(err)
	}
	h2 := newHarness(t, m)
	h2.until("password prompt", func(m Model) bool { return m.connect.HasDialog() })
	h2.typeText("pw")
	h2.enter()
	h2.until("restored", func(m Model) bool {
		return m.conn != nil && m.remote.Path() == last.Remote && m.local.Path() == last.Local
	})
}

func TestSettingsSavePerHost(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("# hand-written\ntheme = \"charm\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _, _ := connectedWith(t, nil, func(o *Options) { o.ConfigPath = cfgPath })
	alias := h.m.conn.Target.Alias

	h.typeText("S")
	if h.m.settings == nil {
		t.Fatal("S should open settings")
	}
	f := h.m.settings
	f.vals.theme = "nord"
	f.vals.concurrency = "2"
	f.vals.upload = "1MB/s"
	f.vals.scope = scopeHost
	h.deliver(settingsSubmittedMsg{})

	if h.m.cfg.Theme != "nord" || h.m.transfers.Concurrency != 2 || h.m.transfers.UploadLimit != units.Rate(units.MiB) {
		t.Fatalf("settings not applied: theme %s, transfers %+v", h.m.cfg.Theme, h.m.transfers)
	}
	saved, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Theme != "nord" {
		t.Errorf("theme not saved: %q", saved.Theme)
	}
	if tr := saved.TransfersFor(alias); tr.Concurrency != 2 || tr.UploadLimit != units.Rate(units.MiB) {
		t.Errorf("host override not saved: %+v", tr)
	}
	if saved.Transfers.Concurrency != config.DefaultConcurrency {
		t.Errorf("a host-only change leaked into the defaults: %+v", saved.Transfers)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "# hand-written") {
		t.Errorf("comment lost:\n%s", b)
	}

	// After disconnecting, the defaults apply again.
	h.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if h.m.transfers.Concurrency != config.DefaultConcurrency {
		t.Errorf("host settings still active after disconnect: %+v", h.m.transfers)
	}
}

package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/sshx/sshtest"
)

// harness runs commands concurrently like the Bubble Tea runtime does and
// feeds their messages back into the model, so asynchronous flows (like
// connecting) can be tested end to end.
type harness struct {
	t    *testing.T
	m    Model
	msgs chan tea.Msg
}

func newHarness(t *testing.T, m Model) *harness {
	h := &harness{t: t, m: m, msgs: make(chan tea.Msg, 256)}
	h.m, _ = update(t, h.m, tea.WindowSizeMsg{Width: 120, Height: 32})
	h.exec(m.Init())
	t.Cleanup(func() { _, _ = h.m.quit() }) // abandon attempts, close connections
	return h
}

// screen is the rendered view without styling, for text assertions.
func (h *harness) screen() string { return ansi.Strip(h.m.View().Content) }

func (h *harness) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() { h.msgs <- cmd() }()
}

func (h *harness) deliver(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			h.exec(c)
		}
	default:
		var cmd tea.Cmd
		h.m, cmd = update(h.t, h.m, msg)
		h.exec(cmd)
	}
}

func (h *harness) key(msg tea.KeyPressMsg) { h.deliver(msg) }

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (h *harness) until(what string, cond func(Model) bool) {
	h.t.Helper()
	deadline := time.After(15 * time.Second)
	for !cond(h.m) {
		select {
		case msg := <-h.msgs:
			h.deliver(msg)
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s; screen:\n%s", what, h.screen())
		}
	}
}

func TestConnectFlowEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test SFTP server exposes Windows paths")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")

	srv := sshtest.New(t, sshtest.Options{Password: "letmein"})
	if err := os.WriteFile(filepath.Join(srv.Root, "remote-file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := New(Options{
		Config:      config.Default(),
		StartDir:    t.TempDir(),
		SSHConfig:   sshx.EmptySSHConfig(),
		Destination: &sshx.Destination{User: srv.User, Host: srv.Host, Port: srv.Port},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)

	h.until("host key prompt", func(m Model) bool {
		return m.connect.HasDialog() && strings.Contains(ansi.Strip(m.View().Content), "New host")
	})
	if !strings.Contains(h.screen(), "SHA256:") {
		t.Error("fingerprint not shown in host key dialog")
	}
	h.typeText("y")

	h.until("password prompt", func(m Model) bool {
		return m.connect.HasDialog() && strings.Contains(ansi.Strip(m.View().Content), "Password for")
	})
	if strings.Contains(h.screen(), "letmein") {
		t.Fatal("password visible before typing")
	}
	h.typeText("letmein")
	if strings.Contains(h.screen(), "letmein") {
		t.Fatal("password echoed on screen")
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})

	h.until("remote listing", func(m Model) bool {
		return m.conn != nil && strings.Contains(ansi.Strip(m.View().Content), "remote-file.txt")
	})
	if !strings.Contains(ansi.Strip(h.m.headerView()), srv.User+"@") {
		t.Errorf("header doesn't show the connection: %q", h.m.headerView())
	}
	kh, _ := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if len(kh) == 0 {
		t.Error("trusted host key was not saved")
	}

	h.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if h.m.conn != nil {
		t.Fatal("ctrl+d did not disconnect")
	}
	if !strings.Contains(h.screen(), "Where to?") {
		t.Error("connect screen not shown after disconnect")
	}
}

func TestCancelPasswordPromptReturnsToPicker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test SFTP server exposes Windows paths")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})

	m, err := New(Options{
		Config:      config.Default(),
		StartDir:    t.TempDir(),
		SSHConfig:   sshx.EmptySSHConfig(),
		Destination: &sshx.Destination{User: srv.User, Host: srv.Host, Port: srv.Port},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)
	h.until("host key prompt", func(m Model) bool { return m.connect.HasDialog() })
	h.typeText("y")
	h.until("password prompt", func(m Model) bool {
		return m.connect.HasDialog() && strings.Contains(ansi.Strip(m.View().Content), "Password")
	})
	h.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	h.until("back at picker", func(m Model) bool {
		return !m.connect.Connecting() && !m.connect.HasDialog()
	})
	if h.m.conn != nil {
		t.Fatal("connected despite cancelling")
	}
	if strings.Contains(h.screen(), "cancelled") {
		t.Error("a user cancel should not be shown as an error")
	}
}

// connected returns a harness already connected to a fresh test server, with
// focus on the local pane.
func connected(t *testing.T, localFiles map[string]string) (*harness, *sshtest.Server, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test SFTP server exposes Windows paths")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	local := t.TempDir()
	for name, body := range localFiles {
		if err := os.WriteFile(filepath.Join(local, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(Options{
		Config: config.Default(), StartDir: local, SSHConfig: sshx.EmptySSHConfig(),
		Destination: &sshx.Destination{User: srv.User, Host: srv.Host, Port: srv.Port},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)
	h.until("host key prompt", func(m Model) bool { return m.connect.HasDialog() })
	h.typeText("y")
	h.until("password prompt", func(m Model) bool {
		return m.connect.HasDialog() && strings.Contains(ansi.Strip(m.View().Content), "Password")
	})
	h.typeText("pw")
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	h.until("connected", func(m Model) bool { return m.conn != nil && m.remote.Path() != "" })
	h.key(tea.KeyPressMsg{Code: tea.KeyTab}) // right → queue is empty, so local
	h.until("local focus", func(m Model) bool { return m.focus == leftSide })
	return h, srv, local
}

func TestUploadWithConflictEndToEnd(t *testing.T) {
	h, srv, _ := connected(t, map[string]string{"hello.txt": "hello world"})

	h.typeText("j") // past ".." to hello.txt
	h.typeText("c")
	remoteFile := filepath.Join(srv.Root, "hello.txt")
	h.until("upload finished", func(m Model) bool {
		b, err := os.ReadFile(remoteFile)
		return err == nil && string(b) == "hello world" && strings.Contains(ansi.Strip(m.View().Content), "✓ hello.txt")
	})
	h.until("remote pane refreshed", func(m Model) bool {
		return strings.Contains(ansi.Strip(m.remote.View()), "hello.txt")
	})

	// Upload again: the file exists now, so we get asked.
	h.typeText("c")
	h.until("conflict dialog", func(m Model) bool {
		return m.queue.HasDialog() && strings.Contains(ansi.Strip(m.View().Content), "Already exists")
	})
	h.typeText("q") // not a valid answer: must not quit or dismiss
	if !h.m.queue.HasDialog() {
		t.Fatal("dialog dismissed by an unrelated key")
	}
	h.typeText("k")
	h.until("kept both", func(m Model) bool {
		_, err := os.Stat(filepath.Join(srv.Root, "hello (1).txt"))
		return err == nil && !m.engine.Busy()
	})

	// The queue panel is reachable with tab and can be cleared.
	h.key(tea.KeyPressMsg{Code: tea.KeyTab})
	h.key(tea.KeyPressMsg{Code: tea.KeyTab})
	if h.m.focus != queueSide {
		t.Fatalf("focus = %v, want queue", h.m.focus)
	}
	h.typeText("c") // clear finished
	if h.m.queue.HasItems() || h.m.focus == queueSide {
		t.Error("clearing finished items should empty the queue and move focus away")
	}
}

func TestQuitWhileBusyAsksFirst(t *testing.T) {
	h, _, local := connected(t, nil)
	if err := os.WriteFile(filepath.Join(local, "big.bin"), make([]byte, 4<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	h.m.applyTransferSettings(config.Transfers{Concurrency: 1, UploadLimit: 64 << 10})
	h.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}) // refresh to see big.bin
	h.until("big.bin listed", func(m Model) bool { return strings.Contains(ansi.Strip(m.local.View()), "big.bin") })
	h.typeText("j")
	h.typeText("c")
	h.until("transfer running", func(m Model) bool { return m.engine != nil && m.engine.Busy() })

	h.typeText("q")
	if h.m.confirm == nil {
		t.Fatal("quit with an active transfer should ask first")
	}
	h.typeText("n")
	if h.m.confirm != nil || h.m.engine == nil {
		t.Fatal("answering no should keep everything running")
	}
}

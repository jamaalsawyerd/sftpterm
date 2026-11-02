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

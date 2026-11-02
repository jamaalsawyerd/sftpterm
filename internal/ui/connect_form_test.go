package ui

import (
	"net"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/sshx/sshtest"
)

// field waits until the huh form has focused the field with this title.
// Moving between fields happens through a command, so typing straight after
// enter would race it.
func (h *harness) field(title string) {
	h.t.Helper()
	h.until("form field "+title, func(m Model) bool {
		return strings.Contains(ansi.Strip(m.View().Content), "┃ "+title)
	})
}

func TestConnectViaForm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test SFTP server exposes Windows paths")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})

	m, err := New(Options{Config: testConfig(), StartDir: t.TempDir(), SSHConfig: sshx.EmptySSHConfig()})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)
	h.typeText("n")
	h.until("form", func(m Model) bool { return strings.Contains(ansi.Strip(m.View().Content), "Directory") })
	// "user@host" in the host field; the port in its own field.
	h.field("Host")
	h.typeText(srv.User + "@" + srv.Host)
	h.enter()
	h.field("User") // left blank: taken from the host field
	h.enter()
	h.field("Port")
	h.typeText(strconv.Itoa(srv.Port))
	h.enter()
	h.field("Directory")
	h.enter() // submit
	h.until("host key prompt", func(m Model) bool { return m.connect.HasDialog() })
	h.typeText("y")
	h.until("password prompt", func(m Model) bool { return strings.Contains(ansi.Strip(m.View().Content), "Password for "+srv.User) })
	h.typeText("pw")
	h.enter()
	h.until("connected", func(m Model) bool { return m.conn != nil })
	if got := h.m.conn.Target; got.User != srv.User || got.Port != srv.Port {
		t.Errorf("target = %+v", got)
	}
}

func TestFormRejectsBadPortAndCancels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, err := New(Options{Config: testConfig(), StartDir: t.TempDir(), SSHConfig: sshx.EmptySSHConfig()})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)
	h.typeText("n")
	h.field("Host")
	h.enter() // empty host
	h.until("host error", func(m Model) bool { return strings.Contains(ansi.Strip(m.View().Content), "host is required") })
	h.typeText("example.com")
	h.enter()
	h.field("User")
	h.enter()
	h.field("Port")
	h.typeText("99999")
	h.enter()
	h.until("port error", func(m Model) bool { return strings.Contains(ansi.Strip(m.View().Content), "port must be") })
	h.typeText("q") // typed into the form, not a quit
	if !h.m.connect.CapturesKeys() {
		t.Fatal("form lost focus")
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	h.until("back at picker", func(m Model) bool { return !m.connect.CapturesKeys() })
	if h.m.connect.Connecting() {
		t.Error("cancelling the form should not connect")
	}
}

func TestEscCancelsSlowConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // accept and say nothing, like a stuck server
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	m, err := New(Options{
		Config: testConfig(), StartDir: t.TempDir(), SSHConfig: sshx.EmptySSHConfig(),
		Destination: &sshx.Destination{User: "x", Host: "127.0.0.1", Port: port},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, m)
	h.until("connecting", func(m Model) bool {
		return m.connect.Connecting() && strings.Contains(ansi.Strip(m.View().Content), "Connecting to")
	})
	start := time.Now()
	h.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if h.m.connect.Connecting() {
		t.Fatal("esc should cancel immediately")
	}
	if time.Since(start) > time.Second {
		t.Error("cancelling blocked the UI")
	}
	// Late events from the abandoned attempt must not resurrect it.
	time.Sleep(200 * time.Millisecond)
	for len(h.msgs) > 0 {
		h.deliver(<-h.msgs)
	}
	if h.m.connect.Connecting() || h.m.conn != nil || strings.Contains(ansi.Strip(h.m.View().Content), "canceled") {
		t.Error("the cancelled attempt leaked back into the UI")
	}
}

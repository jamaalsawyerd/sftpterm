package ui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs/vfstest"
)

// Run `go test ./internal/ui -run Golden -update` after intentional UI
// changes, then review the diff in testdata/.
var updateGolden = flag.Bool("update", false, "rewrite golden files")

var goldenTime = time.Date(2024, 3, 5, 10, 30, 0, 0, time.UTC)

func goldenLocal() *vfstest.MemFS {
	fs := vfstest.New("Local", "/home/jsd")
	fs.MkdirAll("/home/jsd/Music", goldenTime)
	fs.MkdirAll("/home/jsd/Photos", goldenTime)
	fs.WriteFile("/home/jsd/.bashrc", []byte("export EDITOR=vim\n"), 0o644, goldenTime)
	fs.WriteFile("/home/jsd/deploy.sh", []byte("#!/bin/sh\necho deploying\n"), 0o755, goldenTime)
	fs.WriteFile("/home/jsd/notes.txt", []byte("buy milk\nback up the NAS\ncall mom\n"), 0o644, goldenTime)
	fs.WriteFile("/home/jsd/song.flac", make([]byte, 3<<20), 0o644, goldenTime)
	return fs
}

func goldenRemote() *vfstest.MemFS {
	fs := vfstest.New("jsd@nas", "/srv/media")
	fs.MkdirAll("/srv/media/movies", goldenTime)
	fs.WriteFile("/srv/media/README", []byte("media share\n"), 0o644, goldenTime)
	fs.WriteFile("/srv/media/trailer.mp4", make([]byte, 12<<20), 0o644, goldenTime)
	return fs
}

const goldenSSHConfig = `
Host nas
    HostName 192.168.1.5
    User jsd
Host pi
    HostName raspberrypi.local
    Port 2222
    User pi
`

// goldenModel builds a deterministic app at a fixed size.
func goldenModel(t *testing.T, width, height int) Model {
	t.Helper()
	sshcfg, err := sshx.ParseSSHConfig([]byte(goldenSSHConfig), "/home/jsd")
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Config: testConfig(), StartDir: "/home/jsd", SSHConfig: sshcfg, LocalFS: goldenLocal()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	return loadPane(t, m, m.local.Navigate("/home/jsd", ""))
}

// loadPane runs a pane load command synchronously.
func loadPane(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	m, _ = update(t, m, cmd())
	return m
}

// fakeConnect puts a remote pane on the right without a real connection.
// Only rendering is safe on the result: there is no SSH client behind it.
func fakeConnect(t *testing.T, m Model) Model {
	t.Helper()
	m.conn = &sshx.Conn{Target: sshx.Target{Alias: "nas", Host: "192.168.1.5", Port: 22, User: "jsd"}}
	m.remote = pane.New(goldenRemote(), paneOptions(m.cfg, m.styles))
	m.setFocus(rightSide)
	m.layout()
	return loadPane(t, m, m.remote.Navigate("/srv/media", ""))
}

func assertGolden(t *testing.T, name string, m Model) {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	got := strings.Join(lines, "\n") + "\n"

	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("%s changed; run `go test ./internal/ui -run Golden -update` if intended.\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

func TestGoldenStartup(t *testing.T) {
	assertGolden(t, "startup", goldenModel(t, 100, 26))
}

func TestGoldenFullHelp(t *testing.T) {
	m := goldenModel(t, 100, 26)
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // help for the local pane
	m, _ = press(t, m, '?')
	assertGolden(t, "full-help", m)
}

func TestGoldenConnected(t *testing.T) {
	assertGolden(t, "connected", fakeConnect(t, goldenModel(t, 100, 26)))
}

func TestGoldenDeleteConfirm(t *testing.T) {
	m := goldenModel(t, 100, 26)
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(t, m, 'G') // song.flac
	m, _ = press(t, m, 'd')
	assertGolden(t, "delete-confirm", m)
}

func TestGoldenPreview(t *testing.T) {
	m := goldenModel(t, 100, 26)
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(t, m, 'G')
	m, _ = press(t, m, 'k') // notes.txt
	m, cmd := press(t, m, 'v')
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok { // preview load + "opening" notice
		m, cmd = update(t, m, msg)
		_ = cmd
		for _, c := range batch {
			if pm, ok := runQuick(c).(previewMsg); ok {
				m, _ = update(t, m, pm)
			}
		}
	}
	if m.preview == nil {
		t.Fatal("preview did not open")
	}
	assertGolden(t, "preview", m)
}

func TestGoldenTooSmall(t *testing.T) {
	assertGolden(t, "too-small", goldenModel(t, 40, 10))
}

// runQuick runs a command, giving up on timers.
func runQuick(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

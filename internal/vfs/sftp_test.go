package vfs_test

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx/sshtest"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

func newSFTP(t *testing.T) (*vfs.SFTP, string) {
	t.Helper()
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	client, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User:            srv.User,
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKey.PublicKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	sc, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sc.Close() })
	fsys, err := vfs.NewSFTP(sc, "tester@test")
	if err != nil {
		t.Fatal(err)
	}
	return fsys, srv.Root
}

// remote converts a local path under the server root to the path the SFTP
// server reports (the server serves the real file system).
func remote(p string) string { return filepath.ToSlash(p) }

func TestSFTPHomeAndListing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test server exposes Windows paths, which SFTP clients don't expect")
	}
	fsys, root := newSFTP(t)
	home, err := fsys.Home()
	if err != nil || home != remote(root) {
		t.Fatalf("Home() = %q, %v; want %q", home, err, root)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dir"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	entries, err := fsys.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]vfs.Entry{}
	for _, e := range entries {
		got[e.Name] = e
	}
	if e := got["a.txt"]; e.IsDir || e.Size != 5 {
		t.Errorf("a.txt = %+v", e)
	}
	if !got["dir"].IsDir {
		t.Errorf("dir = %+v", got["dir"])
	}
	if e := got["link"]; !e.IsLink || !e.IsDir {
		t.Errorf("link = %+v, want link to dir", e)
	}
}

func TestSFTPFileOps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test server exposes Windows paths, which SFTP clients don't expect")
	}
	fsys, _ := newSFTP(t)
	home, _ := fsys.Home()

	p := fsys.Join(home, "up.txt")
	w, err := fsys.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "over the wire"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := fsys.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "over the wire" {
		t.Errorf("read back %q", b)
	}

	d := fsys.Join(home, "sub")
	if err := fsys.Mkdir(d); err != nil {
		t.Fatal(err)
	}
	moved := fsys.Join(d, "moved.txt")
	if err := fsys.Rename(p, moved); err != nil {
		t.Fatal(err)
	}
	if e, err := fsys.Stat(moved); err != nil || e.Size != 13 {
		t.Errorf("Stat(moved) = %+v, %v", e, err)
	}
	if err := fsys.Remove(moved); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Remove(d); err != nil {
		t.Fatal(err)
	}
	if !fsys.IsRoot("/") || fsys.IsRoot(home) || fsys.Dir("/a/b") != "/a" || fsys.Base("/a/b") != "b" {
		t.Error("path helpers misbehave")
	}
}

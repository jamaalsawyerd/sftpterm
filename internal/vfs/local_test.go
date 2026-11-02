package vfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocalReadDir(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	must(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644))

	var lfs Local
	entries, err := lfs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Name] = e
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %v", len(got), entries)
	}
	if e := got["a.txt"]; e.IsDir || e.Size != 5 || e.Hidden() {
		t.Errorf("a.txt = %+v", e)
	}
	if e := got["sub"]; !e.IsDir {
		t.Errorf("sub should be a dir: %+v", e)
	}
	if !got[".hidden"].Hidden() {
		t.Error(".hidden should be hidden")
	}
}

func TestLocalSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	dir := t.TempDir()
	must(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("12345678"), 0o644))
	must(t, os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "dirlink")))
	must(t, os.Symlink(filepath.Join(dir, "file"), filepath.Join(dir, "filelink")))
	must(t, os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "broken")))

	var lfs Local
	entries, err := lfs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Name] = e
	}
	if e := got["dirlink"]; !e.IsLink || !e.IsDir {
		t.Errorf("dirlink = %+v, want link to dir", e)
	}
	if e := got["filelink"]; !e.IsLink || e.IsDir || e.Size != 8 {
		t.Errorf("filelink = %+v, want link to 8-byte file", e)
	}
	if e := got["broken"]; !e.IsLink || e.IsDir {
		t.Errorf("broken = %+v, want non-dir link", e)
	}

	st, err := lfs.Stat(filepath.Join(dir, "dirlink"))
	if err != nil || !st.IsLink || !st.IsDir {
		t.Errorf("Stat(dirlink) = %+v, %v", st, err)
	}
}

func TestLocalFileOps(t *testing.T) {
	dir := t.TempDir()
	var lfs Local

	p := lfs.Join(dir, "new.txt")
	w, err := lfs.Create(p)
	must(t, err)
	_, err = io.WriteString(w, "sftpterm")
	must(t, err)
	must(t, w.Close())

	r, err := lfs.Open(p)
	must(t, err)
	b, err := io.ReadAll(r)
	must(t, err)
	must(t, r.Close())
	if string(b) != "sftpterm" {
		t.Errorf("read %q", b)
	}

	must(t, lfs.Mkdir(lfs.Join(dir, "d")))
	moved := lfs.Join(dir, "d", "moved.txt")
	must(t, lfs.Rename(p, moved))
	if _, err := lfs.Stat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("old path still exists: %v", err)
	}
	must(t, lfs.Remove(moved))
	must(t, lfs.Remove(lfs.Join(dir, "d")))

	entries, err := lfs.ReadDir(dir)
	must(t, err)
	if len(entries) != 0 {
		t.Errorf("dir not empty: %v", entries)
	}
}

func TestLocalPathHelpers(t *testing.T) {
	var lfs Local
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	if !lfs.IsRoot(root) {
		t.Errorf("IsRoot(%q) = false", root)
	}
	p := lfs.Join(root, "a", "b")
	if lfs.IsRoot(p) || lfs.Base(p) != "b" || lfs.Base(lfs.Dir(p)) != "a" {
		t.Errorf("path helpers misbehave for %q", p)
	}
	if lfs.Label() != "Local" {
		t.Error("unexpected label")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

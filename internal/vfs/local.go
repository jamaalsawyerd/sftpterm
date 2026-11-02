package vfs

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Local is the machine's own file system.
type Local struct{}

var _ FS = Local{}

// NewLocal returns the local file system.
func NewLocal() Local { return Local{} }

// Label implements FS.
func (Local) Label() string { return "Local" }

// Home implements FS.
func (Local) Home() (string, error) { return os.UserHomeDir() }

// ReadDir implements FS. Entries that vanish while listing are skipped.
func (Local) ReadDir(path string) ([]Entry, error) {
	des, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, FromFileInfo(info, filepath.Join(path, de.Name()), os.Stat))
	}
	return out, nil
}

// Stat implements FS.
func (Local) Stat(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	return FromFileInfo(info, path, os.Stat), nil
}

// Open implements FS.
func (Local) Open(path string) (io.ReadCloser, error) { return os.Open(path) }

// Create implements FS.
func (Local) Create(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
}

// Mkdir implements FS.
func (Local) Mkdir(path string) error { return os.Mkdir(path, 0o777) }

// Remove implements FS.
func (Local) Remove(path string) error { return os.Remove(path) }

// Rename implements FS.
func (Local) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

// Chtimes implements FS.
func (Local) Chtimes(path string, mtime time.Time) error { return os.Chtimes(path, mtime, mtime) }

// Chmod implements FS.
func (Local) Chmod(path string, mode fs.FileMode) error { return os.Chmod(path, mode) }

// Join implements FS.
func (Local) Join(elem ...string) string { return filepath.Join(elem...) }

// Dir implements FS.
func (Local) Dir(path string) string { return filepath.Dir(path) }

// Base implements FS.
func (Local) Base(path string) string { return filepath.Base(path) }

// IsRoot implements FS.
func (Local) IsRoot(path string) bool { return filepath.Dir(path) == path }

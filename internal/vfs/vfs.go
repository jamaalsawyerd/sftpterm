// Package vfs defines the file system abstraction that lets the UI browse and
// transfer between local and remote locations with the same code.
package vfs

import (
	"io"
	"io/fs"
	"time"
)

// Entry describes one item in a directory listing.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	// IsDir is true for directories and for symlinks that point to one, so
	// links to directories can be navigated into.
	IsDir  bool
	IsLink bool
}

// Hidden reports whether the entry is a dotfile.
func (e Entry) Hidden() bool { return len(e.Name) > 0 && e.Name[0] == '.' }

// Executable reports whether any execute bit is set on a regular file.
func (e Entry) Executable() bool { return !e.IsDir && e.Mode&0o111 != 0 }

// FS is a browsable file system. Paths are absolute and use the file system's
// own conventions (local paths on Windows use backslashes, SFTP paths always
// use forward slashes), so callers must use the FS's path helpers rather than
// path or path/filepath directly.
type FS interface {
	// Label is a short human-readable name, e.g. "Local" or "jsd@nas".
	Label() string
	// Home returns the starting directory for this file system.
	Home() (string, error)

	ReadDir(path string) ([]Entry, error)
	// Stat describes path without following a final symlink, except to
	// fill in IsDir for links to directories.
	Stat(path string) (Entry, error)

	Open(path string) (io.ReadCloser, error)
	// Create creates or truncates a file for writing.
	Create(path string) (io.WriteCloser, error)
	Mkdir(path string) error
	// Remove deletes a file or an empty directory.
	Remove(path string) error
	Rename(oldPath, newPath string) error

	Join(elem ...string) string
	Dir(path string) string
	Base(path string) string
	IsRoot(path string) bool
}

// FromFileInfo converts info (from an lstat-style call) into an Entry. For
// symlinks, stat is used to resolve the target so IsDir and Size describe what
// the link points to; a broken link is reported as a non-directory.
func FromFileInfo(info fs.FileInfo, fullPath string, stat func(string) (fs.FileInfo, error)) Entry {
	e := Entry{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		e.IsLink = true
		if target, err := stat(fullPath); err == nil {
			e.IsDir = target.IsDir()
			if !e.IsDir {
				e.Size = target.Size()
			}
		}
	}
	return e
}

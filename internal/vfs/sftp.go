package vfs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
)

// SFTP is a remote file system over an SFTP session. Paths always use
// forward slashes.
type SFTP struct {
	c     *sftp.Client
	label string
	home  string
}

var _ FS = (*SFTP)(nil)

// NewSFTP wraps an SFTP client. The label is shown in the pane title, e.g.
// "jsd@nas".
func NewSFTP(c *sftp.Client, label string) (*SFTP, error) {
	home, err := c.Getwd()
	if err != nil {
		return nil, fmt.Errorf("finding remote home directory: %w", err)
	}
	return &SFTP{c: c, label: label, home: home}, nil
}

// Label implements FS.
func (s *SFTP) Label() string { return s.label }

// Home implements FS. It is the directory the server starts sessions in.
func (s *SFTP) Home() (string, error) { return s.home, nil }

// ReadDir implements FS.
func (s *SFTP) ReadDir(p string) ([]Entry, error) {
	infos, err := s.c.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(infos))
	for _, info := range infos {
		out = append(out, FromFileInfo(info, path.Join(p, info.Name()), s.c.Stat))
	}
	return out, nil
}

// Stat implements FS.
func (s *SFTP) Stat(p string) (Entry, error) {
	info, err := s.c.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	return FromFileInfo(info, p, s.c.Stat), nil
}

// Open implements FS.
func (s *SFTP) Open(p string) (io.ReadCloser, error) { return s.c.Open(p) }

// Create implements FS.
func (s *SFTP) Create(p string) (io.WriteCloser, error) {
	return s.c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

// Mkdir implements FS.
func (s *SFTP) Mkdir(p string) error { return s.c.Mkdir(p) }

// Remove implements FS.
func (s *SFTP) Remove(p string) error { return s.c.Remove(p) }

// Rename implements FS. It prefers the POSIX rename extension, which replaces
// an existing target atomically like a local rename does.
func (s *SFTP) Rename(oldPath, newPath string) error {
	if _, ok := s.c.HasExtension("posix-rename@openssh.com"); ok {
		return s.c.PosixRename(oldPath, newPath)
	}
	return s.c.Rename(oldPath, newPath)
}

// Chtimes implements FS.
func (s *SFTP) Chtimes(p string, mtime time.Time) error { return s.c.Chtimes(p, mtime, mtime) }

// Chmod implements FS.
func (s *SFTP) Chmod(p string, mode fs.FileMode) error { return s.c.Chmod(p, mode) }

// Join implements FS.
func (*SFTP) Join(elem ...string) string { return path.Join(elem...) }

// Dir implements FS.
func (*SFTP) Dir(p string) string { return path.Dir(p) }

// Base implements FS.
func (*SFTP) Base(p string) string { return path.Base(p) }

// IsRoot implements FS.
func (*SFTP) IsRoot(p string) bool { return p == "/" }

// Package vfstest provides an in-memory vfs.FS for deterministic tests.
package vfstest

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

type node struct {
	dir   bool
	data  []byte
	mode  fs.FileMode
	mtime time.Time
}

// MemFS is a forward-slash file system held in memory. It is safe for
// concurrent use.
type MemFS struct {
	label string
	home  string
	mu    sync.Mutex
	nodes map[string]*node
}

var _ vfs.FS = (*MemFS)(nil)

// New returns an empty file system containing only "/" and home.
func New(label, home string) *MemFS {
	m := &MemFS{label: label, home: home, nodes: map[string]*node{"/": {dir: true, mode: fs.ModeDir | 0o755}}}
	m.MkdirAll(home, time.Time{})
	return m
}

// MkdirAll creates a directory and its parents.
func (m *MemFS) MkdirAll(p string, mtime time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	for cur := p; cur != "/"; cur = path.Dir(cur) {
		if _, ok := m.nodes[cur]; !ok {
			m.nodes[cur] = &node{dir: true, mode: fs.ModeDir | 0o755, mtime: mtime}
		}
	}
}

// WriteFile creates a file (and its parent directories).
func (m *MemFS) WriteFile(p string, data []byte, mode fs.FileMode, mtime time.Time) {
	m.MkdirAll(path.Dir(p), mtime)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[path.Clean(p)] = &node{data: slices.Clone(data), mode: mode, mtime: mtime}
}

// ReadFile returns a file's contents.
func (m *MemFS) ReadFile(p string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[path.Clean(p)]
	if !ok || n.dir {
		return nil, &fs.PathError{Op: "read", Path: p, Err: fs.ErrNotExist}
	}
	return slices.Clone(n.data), nil
}

func (m *MemFS) entry(p string, n *node) vfs.Entry {
	return vfs.Entry{Name: path.Base(p), Size: int64(len(n.data)), Mode: n.mode, ModTime: n.mtime, IsDir: n.dir}
}

// Label implements vfs.FS.
func (m *MemFS) Label() string { return m.label }

// Home implements vfs.FS.
func (m *MemFS) Home() (string, error) { return m.home, nil }

// ReadDir implements vfs.FS.
func (m *MemFS) ReadDir(p string) ([]vfs.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	if n, ok := m.nodes[p]; !ok || !n.dir {
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: fs.ErrNotExist}
	}
	var out []vfs.Entry
	for q, n := range m.nodes {
		if q != "/" && path.Dir(q) == p {
			out = append(out, m.entry(q, n))
		}
	}
	slices.SortFunc(out, func(a, b vfs.Entry) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Stat implements vfs.FS.
func (m *MemFS) Stat(p string) (vfs.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[path.Clean(p)]
	if !ok {
		return vfs.Entry{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}
	return m.entry(path.Clean(p), n), nil
}

// Open implements vfs.FS.
func (m *MemFS) Open(p string) (io.ReadCloser, error) {
	data, err := m.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type memWriter struct {
	m   *MemFS
	p   string
	buf bytes.Buffer
}

func (w *memWriter) Write(b []byte) (int, error) { return w.buf.Write(b) }

func (w *memWriter) Close() error {
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	w.m.nodes[w.p] = &node{data: w.buf.Bytes(), mode: 0o644, mtime: time.Now()}
	return nil
}

// Create implements vfs.FS.
func (m *MemFS) Create(p string) (io.WriteCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	if parent, ok := m.nodes[path.Dir(p)]; !ok || !parent.dir {
		return nil, &fs.PathError{Op: "create", Path: p, Err: fs.ErrNotExist}
	}
	m.nodes[p] = &node{mode: 0o644, mtime: time.Now()}
	return &memWriter{m: m, p: p}, nil
}

// Mkdir implements vfs.FS.
func (m *MemFS) Mkdir(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	if _, ok := m.nodes[p]; ok {
		return &fs.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	if parent, ok := m.nodes[path.Dir(p)]; !ok || !parent.dir {
		return &fs.PathError{Op: "mkdir", Path: p, Err: fs.ErrNotExist}
	}
	m.nodes[p] = &node{dir: true, mode: fs.ModeDir | 0o755, mtime: time.Now()}
	return nil
}

// Remove implements vfs.FS.
func (m *MemFS) Remove(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	n, ok := m.nodes[p]
	if !ok {
		return &fs.PathError{Op: "remove", Path: p, Err: fs.ErrNotExist}
	}
	if n.dir {
		for q := range m.nodes {
			if path.Dir(q) == p && q != p {
				return &fs.PathError{Op: "remove", Path: p, Err: fmt.Errorf("directory not empty")}
			}
		}
	}
	delete(m.nodes, p)
	return nil
}

// Rename implements vfs.FS. Like POSIX rename, it replaces a file target.
func (m *MemFS) Rename(oldPath, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldPath, newPath = path.Clean(oldPath), path.Clean(newPath)
	n, ok := m.nodes[oldPath]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldPath, Err: fs.ErrNotExist}
	}
	for q, c := range m.nodes {
		if q == oldPath || strings.HasPrefix(q, oldPath+"/") {
			delete(m.nodes, q)
			m.nodes[newPath+strings.TrimPrefix(q, oldPath)] = c
		}
	}
	m.nodes[newPath] = n
	return nil
}

// Chtimes implements vfs.FS.
func (m *MemFS) Chtimes(p string, mtime time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[path.Clean(p)]; ok {
		n.mtime = mtime
		return nil
	}
	return &fs.PathError{Op: "chtimes", Path: p, Err: fs.ErrNotExist}
}

// Chmod implements vfs.FS.
func (m *MemFS) Chmod(p string, mode fs.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[path.Clean(p)]; ok {
		n.mode = n.mode&fs.ModeType | mode.Perm()
		return nil
	}
	return &fs.PathError{Op: "chmod", Path: p, Err: fs.ErrNotExist}
}

// Join implements vfs.FS.
func (*MemFS) Join(elem ...string) string { return path.Join(elem...) }

// Dir implements vfs.FS.
func (*MemFS) Dir(p string) string { return path.Dir(p) }

// Base implements vfs.FS.
func (*MemFS) Base(p string) string { return path.Base(p) }

// IsRoot implements vfs.FS.
func (*MemFS) IsRoot(p string) bool { return p == "/" }

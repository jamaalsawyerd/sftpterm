// Package state stores what sftpterm remembers between runs: bookmarks and the
// last directories used for each connection. Unlike the config file, it is
// written by the app and not meant for hand editing.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Bookmark remembers a pair of directories for one connection.
type Bookmark struct {
	Name    string    `json:"name"`
	Local   string    `json:"local"`
	Remote  string    `json:"remote"`
	Created time.Time `json:"created"`
}

// Location is where the panes were.
type Location struct {
	Local  string    `json:"local"`
	Remote string    `json:"remote"`
	Time   time.Time `json:"time"`
}

// Host is everything remembered for one connection, keyed by user@host[:port].
type Host struct {
	Last      *Location  `json:"last,omitempty"`
	Bookmarks []Bookmark `json:"bookmarks,omitempty"`
}

// Store is the persisted state. The zero value is empty and usable.
type Store struct {
	Version int              `json:"version"`
	Hosts   map[string]*Host `json:"hosts"`

	path string
}

const version = 1

// DefaultPath is bookmarks.json next to the config file.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config directory: %w", err)
	}
	return filepath.Join(dir, "sftpterm", "bookmarks.json"), nil
}

// Load reads the store at path; a missing file is an empty store.
func Load(path string) (*Store, error) {
	s := &Store{Version: version, Hosts: map[string]*Host{}, path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("reading bookmarks: %w", err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return &Store{Version: version, Hosts: map[string]*Host{}, path: path},
			fmt.Errorf("bookmarks file %s is corrupt (starting fresh; the file is kept until the next save): %w", path, err)
	}
	if s.Hosts == nil {
		s.Hosts = map[string]*Host{}
	}
	s.path = path
	return s, nil
}

// Save writes the store atomically. A store without a path (e.g. in tests)
// is not written.
func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	s.Version = version
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".bookmarks-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func (s *Store) host(key string) *Host {
	if s.Hosts == nil {
		s.Hosts = map[string]*Host{}
	}
	h, ok := s.Hosts[key]
	if !ok {
		h = &Host{}
		s.Hosts[key] = h
	}
	return h
}

// Last returns where the panes were for a connection.
func (s *Store) Last(key string) (Location, bool) {
	if h, ok := s.Hosts[key]; ok && h.Last != nil {
		return *h.Last, true
	}
	return Location{}, false
}

// SetLast records where the panes are now.
func (s *Store) SetLast(key, local, remote string) {
	if local == "" || remote == "" {
		return
	}
	s.host(key).Last = &Location{Local: local, Remote: remote, Time: time.Now()}
}

// Bookmarks lists a connection's bookmarks, sorted by name.
func (s *Store) Bookmarks(key string) []Bookmark {
	h, ok := s.Hosts[key]
	if !ok {
		return nil
	}
	out := slices.Clone(h.Bookmarks)
	slices.SortFunc(out, func(a, b Bookmark) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

// AddBookmark saves a bookmark, replacing any with the same name.
func (s *Store) AddBookmark(key string, b Bookmark) {
	h := s.host(key)
	if b.Created.IsZero() {
		b.Created = time.Now()
	}
	h.Bookmarks = slices.DeleteFunc(h.Bookmarks, func(x Bookmark) bool { return x.Name == b.Name })
	h.Bookmarks = append(h.Bookmarks, b)
}

// RemoveBookmark deletes a bookmark by name.
func (s *Store) RemoveBookmark(key, name string) {
	if h, ok := s.Hosts[key]; ok {
		h.Bookmarks = slices.DeleteFunc(h.Bookmarks, func(x Bookmark) bool { return x.Name == name })
	}
}

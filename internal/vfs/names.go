package vfs

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrUnsafeName is returned for names that could escape their directory or
// that the local system can't store.
var ErrUnsafeName = errors.New("unsafe file name")

// CheckName validates a single path element, as listed by a (possibly
// hostile) server or typed by the user. It rejects anything that could
// traverse directories ("..", separators), NUL bytes, and control characters.
func CheckName(name string) error {
	switch {
	case name == "", name == ".", name == "..":
		return fmt.Errorf("%w: %q", ErrUnsafeName, name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("%w: %q contains a path separator", ErrUnsafeName, Sanitize(name))
	case !utf8.ValidString(name):
		return fmt.Errorf("%w: %q is not valid UTF-8", ErrUnsafeName, Sanitize(name))
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %q contains control characters", ErrUnsafeName, Sanitize(name))
		}
	}
	return nil
}

// Sanitize makes untrusted text safe to print to a terminal: control
// characters (which could carry escape sequences) are replaced with a visible
// placeholder, and invalid UTF-8 is repaired. Tabs become spaces.
func Sanitize(s string) string {
	clean := true
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case unicode.IsControl(r):
			b.WriteRune('␛') // a visible stand-in; never emit the raw byte
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RemoveAll deletes path and, for directories, everything inside it.
// Directory symlinks are removed as links, never followed.
func RemoveAll(fsys FS, path string) error {
	e, err := fsys.Stat(path)
	if err != nil {
		return err
	}
	if e.IsDir && !e.IsLink {
		entries, err := fsys.ReadDir(path)
		if err != nil {
			return err
		}
		for _, child := range entries {
			if err := RemoveAll(fsys, fsys.Join(path, child.Name)); err != nil {
				return err
			}
		}
	}
	return fsys.Remove(path)
}

// ReadHead reads up to n bytes from the start of a file, reporting whether
// the file was longer.
func ReadHead(fsys FS, path string, n int) (data []byte, truncated bool, err error) {
	r, err := fsys.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	buf := make([]byte, n+1)
	got, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if got > n {
		return buf[:n], true, nil
	}
	return buf[:got], false, nil
}

// LooksBinary guesses whether data is binary rather than text.
func LooksBinary(data []byte) bool {
	sample := data[:min(len(data), 8192)]
	if len(sample) == 0 {
		return false
	}
	if strings.ContainsRune(string(sample), 0) {
		return true
	}
	// Mostly-invalid UTF-8 is binary. Allow a cut-off rune at the end.
	bad := 0
	for i := 0; i < len(sample); {
		r, size := utf8.DecodeRune(sample[i:])
		if r == utf8.RuneError && size <= 1 && len(sample)-i >= utf8.UTFMax {
			bad++
		}
		i += size
	}
	return bad*20 > len(sample) // more than 5% garbage
}

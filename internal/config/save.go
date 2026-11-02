package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Save writes cfg to path by editing the existing file in place: only the
// values that changed are rewritten, so comments, ordering, and keys sftpterm
// doesn't know about survive. Settings still at their defaults are only
// written if the file already mentions them.
func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	orig, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading config: %w", err)
	}

	ed := parseDoc(string(orig))
	def := Default()
	set := func(table []string, key, value string, isDefault bool) {
		if !isDefault || ed.has(table, key) {
			ed.set(table, key, value)
		}
	}
	root, transfers := []string{}, []string{"transfers"}
	set(root, "theme", quote(cfg.Theme), cfg.Theme == def.Theme)
	set(root, "icons", quote(cfg.Icons), cfg.Icons == def.Icons)
	set(root, "show_hidden", strconv.FormatBool(cfg.ShowHidden), cfg.ShowHidden == def.ShowHidden)
	set(root, "animations", strconv.FormatBool(cfg.Animations), cfg.Animations == def.Animations)
	set(root, "bell", strconv.FormatBool(cfg.Bell), cfg.Bell == def.Bell)
	t, dt := cfg.Transfers, def.Transfers
	set(transfers, "concurrency", strconv.Itoa(t.Concurrency), t.Concurrency == dt.Concurrency)
	set(transfers, "upload_limit", quote(t.UploadLimit.String()), t.UploadLimit == dt.UploadLimit)
	set(transfers, "download_limit", quote(t.DownloadLimit.String()), t.DownloadLimit == dt.DownloadLimit)

	aliases := make([]string, 0, len(cfg.Hosts))
	for a := range cfg.Hosts {
		aliases = append(aliases, a)
	}
	slices.Sort(aliases)
	for _, alias := range aliases {
		h, table := cfg.Hosts[alias], []string{"hosts", alias}
		if h.Concurrency != nil {
			ed.set(table, "concurrency", strconv.Itoa(*h.Concurrency))
		}
		if h.UploadLimit != nil {
			ed.set(table, "upload_limit", quote(h.UploadLimit.String()))
		}
		if h.DownloadLimit != nil {
			ed.set(table, "download_limit", quote(h.DownloadLimit.String()))
		}
	}

	out := ed.String()
	// Never write something we can't read back.
	var check Config
	if _, err := toml.Decode(out, &check); err != nil {
		return fmt.Errorf("internal error: edited config does not parse: %w", err)
	}
	return writeAtomic(path, []byte(out))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), st.Mode().Perm())
	}
	return os.Rename(tmp.Name(), path)
}

// quote writes a TOML basic string. strconv.Quote is not suitable: Go
// escapes like \x41 or \a aren't valid TOML.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r) // invalid UTF-8 was rejected by Validate
		}
	}
	b.WriteByte('"')
	return b.String()
}

// doc is a line-oriented view of a TOML file, good enough to update simple
// key = value pairs in place.
type doc struct {
	lines   []string
	headers []int          // line index of every table or array-of-tables header
	keys    map[string]int // table+key → line index
	tables  map[string]int // table → header line index (-1 for root)
	ends    map[string]int // table → last key line index
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tableID names a table unambiguously: the segment count is included so the
// root table (no segments) can't collide with a table named "".
func tableID(table []string) string {
	return strconv.Itoa(len(table)) + ":" + strings.Join(table, "\x00")
}

var rootID = tableID(nil)

// lookupID is the table ID used to find existing tables. The TOML decoder
// matches struct fields case-insensitively ([Transfers] fills Transfers), so
// the editor must too, or it would add a duplicate that the old one shadows.
// Map keys, like host aliases in [hosts.<alias>], stay case-sensitive.
func lookupID(table []string) string {
	t := slices.Clone(table)
	if len(t) > 0 {
		t[0] = strings.ToLower(t[0])
	}
	return tableID(t)
}

func lookupKey(id, key string) string { return id + "\x02" + strings.ToLower(key) }

func parseDoc(src string) *doc {
	d := &doc{keys: map[string]int{}, tables: map[string]int{rootID: -1}, ends: map[string]int{}}
	if src != "" {
		d.lines = strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	}
	d.index()
	return d
}

func (d *doc) index() {
	clear(d.keys)
	clear(d.ends)
	d.headers = d.headers[:0]
	d.tables = map[string]int{rootID: -1}
	current := rootID
	for i, line := range d.lines {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "" || strings.HasPrefix(trim, "#"):
		case strings.HasPrefix(trim, "[["):
			current = "\x01array" // arrays of tables: leave alone
			d.headers = append(d.headers, i)
		case strings.HasPrefix(trim, "["):
			end := strings.Index(trim, "]")
			if end < 0 {
				continue
			}
			d.headers = append(d.headers, i)
			current = lookupID(splitKeyPath(trim[1:end]))
			if _, seen := d.tables[current]; !seen {
				d.tables[current] = i
			}
		default:
			k, _, ok := strings.Cut(trim, "=")
			if !ok {
				continue
			}
			path := splitKeyPath(k)
			if len(path) != 1 {
				continue // dotted keys: not something we write
			}
			d.keys[lookupKey(current, path[0])] = i
			d.ends[current] = i
		}
	}
}

func (d *doc) has(table []string, key string) bool {
	_, ok := d.keys[lookupKey(lookupID(table), key)]
	return ok
}

func (d *doc) set(table []string, key, value string) {
	id := lookupID(table)
	if i, ok := d.keys[lookupKey(id, key)]; ok {
		d.lines[i] = replaceValue(d.lines[i], value)
		return
	}
	line := formatKey(key) + " = " + value
	switch hdr, ok := d.tables[id]; {
	case ok && id == rootID:
		// Root keys must come before the first table header.
		at := 0
		if end, ok := d.ends[rootID]; ok {
			at = end + 1
		} else if first := d.firstHeader(); first >= 0 {
			at = first
			line += "\n"
		} else {
			at = len(d.lines)
		}
		d.insert(at, line)
	case ok:
		at := hdr + 1
		if end, ok := d.ends[id]; ok {
			at = end + 1
		}
		d.insert(at, line)
	default:
		if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, "["+formatTable(table)+"]", line)
	}
	d.index()
}

// firstHeader is the line of the first header of any kind, or -1.
func (d *doc) firstHeader() int {
	if len(d.headers) == 0 {
		return -1
	}
	return d.headers[0]
}

func (d *doc) insert(at int, text string) {
	d.lines = slices.Insert(d.lines, at, strings.Split(text, "\n")...)
}

func (d *doc) String() string {
	if len(d.lines) == 0 {
		return ""
	}
	return strings.Join(d.lines, "\n") + "\n"
}

// replaceValue swaps the value in a key = value line, keeping indentation,
// the key as written, and any trailing comment.
func replaceValue(line, value string) string {
	eq := strings.Index(line, "=")
	comment := ""
	if c := commentStart(line[eq+1:]); c >= 0 {
		comment = " " + strings.TrimSpace(line[eq+1+c:])
	}
	return strings.TrimRight(line[:eq], " \t") + " = " + value + comment
}

// commentStart finds a # that isn't inside a string.
func commentStart(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == '\\' && quote == '"':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == '#':
			return i
		}
	}
	return -1
}

// splitKeyPath splits a dotted TOML key, honoring quoted segments.
func splitKeyPath(s string) []string {
	var parts []string
	var cur bytes.Buffer
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == '\\' && quote == '"' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, cur.String())
			cur.Reset()
		case c == ' ' || c == '\t':
		default:
			cur.WriteByte(c)
		}
	}
	return append(parts, cur.String())
}

func formatKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return quote(k)
}

func formatTable(table []string) string {
	parts := make([]string, len(table))
	for i, p := range table {
		parts[i] = formatKey(p)
	}
	return strings.Join(parts, ".")
}

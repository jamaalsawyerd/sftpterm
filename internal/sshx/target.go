// Package sshx establishes SFTP sessions: it resolves hosts from OpenSSH
// config, authenticates with the agent, keys, or passwords, and verifies host
// keys against known_hosts.
package sshx

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// DefaultPort is the standard SSH port.
const DefaultPort = 22

// Target is everything needed to connect to one host.
type Target struct {
	// Alias is the name the user chose, e.g. "nas" from ~/.ssh/config.
	Alias string
	// Host is the address to dial (HostName in ssh config).
	Host string
	Port int
	User string
	// Path is an optional starting directory on the server.
	Path string

	IdentityFiles  []string
	IdentitiesOnly bool
	// KnownHostsFiles are checked for host keys; new keys are appended to
	// the first one.
	KnownHostsFiles []string
}

// Addr is the host:port to dial.
func (t Target) Addr() string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

// String renders the target as user@alias, plus the port when non-standard.
func (t Target) String() string {
	s := t.User + "@" + t.Alias
	if t.Port != DefaultPort {
		s += ":" + strconv.Itoa(t.Port)
	}
	return s
}

// Destination is a parsed command-line destination such as "jsd@nas:/srv" or
// "sftp://jsd@nas:2222/srv". Zero values mean "not specified".
type Destination struct {
	User string
	Host string
	Port int
	Path string
}

// ParseDestination parses [user@]host[:path] (scp style, IPv6 hosts in
// brackets) or sftp://[user@]host[:port][/path].
func ParseDestination(s string) (Destination, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Destination{}, errors.New("empty destination")
	}
	if strings.HasPrefix(s, "sftp://") || strings.HasPrefix(s, "ssh://") {
		return parseURL(s)
	}

	orig := s
	var d Destination
	// The user is everything before an "@" that comes before any ":".
	if at, colon := strings.Index(s, "@"), strings.Index(s, ":"); at >= 0 && (colon < 0 || at < colon) {
		d.User, s = s[:at], s[at+1:]
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return Destination{}, fmt.Errorf("invalid destination %q: unclosed [", s)
		}
		d.Host, s = s[1:end], s[end+1:]
		if s != "" && !strings.HasPrefix(s, ":") {
			return Destination{}, fmt.Errorf("invalid destination: unexpected %q after host", s)
		}
		d.Path = strings.TrimPrefix(s, ":")
	} else if host, path, ok := strings.Cut(s, ":"); ok {
		d.Host, d.Path = host, path
	} else {
		d.Host = s
	}
	if d.Host == "" {
		return Destination{}, fmt.Errorf("invalid destination %q: missing host", orig)
	}
	return d, nil
}

func parseURL(s string) (Destination, error) {
	u, err := url.Parse(s)
	if err != nil {
		return Destination{}, fmt.Errorf("invalid destination: %w", err)
	}
	d := Destination{Host: u.Hostname(), Path: u.Path}
	if u.User != nil {
		d.User = u.User.Username()
	}
	if p := u.Port(); p != "" {
		if d.Port, err = parsePort(p); err != nil {
			return Destination{}, err
		}
	}
	if d.Host == "" {
		return Destination{}, fmt.Errorf("invalid destination %q: missing host", s)
	}
	return d, nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return p, nil
}

// SSHConfig is a parsed OpenSSH client config.
type SSHConfig struct {
	cfg  *ssh_config.Config
	home string
}

// DefaultSSHConfigPath is ~/.ssh/config.
func DefaultSSHConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

// LoadSSHConfig reads an OpenSSH config file. A missing file yields an empty
// config.
func LoadSSHConfig(path string) (*SSHConfig, error) {
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		b, err = nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading ssh config: %w", err)
	}
	return parseSSHConfig(b, home)
}

// EmptySSHConfig is a config with no hosts, for when ~/.ssh/config can't be
// read.
func EmptySSHConfig() *SSHConfig {
	home, _ := os.UserHomeDir()
	c, _ := parseSSHConfig(nil, home)
	return c
}

// ParseSSHConfig parses config text, expanding ~ against home.
func ParseSSHConfig(b []byte, home string) (*SSHConfig, error) { return parseSSHConfig(b, home) }

func parseSSHConfig(b []byte, home string) (*SSHConfig, error) {
	cfg, err := ssh_config.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parsing ssh config: %w", err)
	}
	return &SSHConfig{cfg: cfg, home: home}, nil
}

// Hosts lists the concrete (non-wildcard) host aliases in config order.
func (c *SSHConfig) Hosts() []string {
	var out []string
	for _, h := range c.cfg.Hosts {
		for _, p := range h.Patterns {
			name := p.String()
			if strings.ContainsAny(name, "*?!") || slices.Contains(out, name) {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

func (c *SSHConfig) get(alias, key string) string {
	v, _ := c.cfg.Get(alias, key)
	return strings.TrimSpace(v)
}

// Resolve builds a Target for alias using the config, with OpenSSH defaults
// for anything unset.
func (c *SSHConfig) Resolve(alias string) Target {
	t := Target{Alias: alias, Host: alias, Port: DefaultPort, User: currentUser()}
	if h := c.get(alias, "HostName"); h != "" {
		t.Host = strings.ReplaceAll(h, "%h", alias)
	}
	if p, err := parsePort(c.get(alias, "Port")); err == nil {
		t.Port = p
	}
	if u := c.get(alias, "User"); u != "" {
		t.User = u
	}
	ids, _ := c.cfg.GetAll(alias, "IdentityFile")
	for _, id := range ids {
		t.IdentityFiles = append(t.IdentityFiles, c.expand(id))
	}
	t.IdentitiesOnly = strings.EqualFold(c.get(alias, "IdentitiesOnly"), "yes")
	if files := strings.Fields(c.get(alias, "UserKnownHostsFile")); len(files) > 0 {
		for _, f := range files {
			t.KnownHostsFiles = append(t.KnownHostsFiles, c.expand(f))
		}
	} else {
		t.KnownHostsFiles = []string{filepath.Join(c.home, ".ssh", "known_hosts")}
	}
	return t
}

// TargetFor resolves a destination: the host is looked up as an alias, and
// anything given explicitly (user, port, path) overrides the config.
func (c *SSHConfig) TargetFor(d Destination) Target {
	t := c.Resolve(d.Host)
	if d.User != "" {
		t.User = d.User
	}
	if d.Port != 0 {
		t.Port = d.Port
	}
	t.Path = d.Path
	return t
}

// expand handles the ~ and %d (home directory) tokens used in ssh config paths.
func (c *SSHConfig) expand(p string) string {
	p = strings.Trim(p, `"`)
	p = strings.ReplaceAll(p, "%d", c.home)
	if p == "~" {
		return c.home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(c.home, p[2:])
	}
	return p
}

func currentUser() string {
	if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndex(name, `\`); i >= 0 { // DOMAIN\user on Windows
			name = name[i+1:]
		}
		return name
	}
	return os.Getenv("USER")
}

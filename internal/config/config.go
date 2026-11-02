// Package config loads sftpterm's user settings from a TOML file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

// Icon sets for file listings.
const (
	IconsUnicode = "unicode" // plain Unicode glyphs; works everywhere (default)
	IconsNerd    = "nerd"    // Nerd Font glyphs; requires a patched font
	IconsEmoji   = "emoji"
	IconsASCII   = "ascii"
)

// IconSets lists the accepted values for Config.Icons.
var IconSets = []string{IconsUnicode, IconsNerd, IconsEmoji, IconsASCII}

// Limits for Transfers.Concurrency.
const (
	MinConcurrency     = 1
	MaxConcurrency     = 16
	DefaultConcurrency = 3
)

// DefaultTheme is used when no theme is configured.
const DefaultTheme = "charm"

// Config holds all user-tunable settings.
type Config struct {
	Theme      string    `toml:"theme"`
	Icons      string    `toml:"icons"`
	ShowHidden bool      `toml:"show_hidden"`
	Transfers  Transfers `toml:"transfers"`
	// Hosts holds per-host overrides keyed by ssh alias, e.g. [hosts.nas].
	Hosts map[string]HostSettings `toml:"hosts"`
}

// HostSettings override Transfers for one host. Nil fields inherit.
type HostSettings struct {
	Concurrency   *int        `toml:"concurrency"`
	UploadLimit   *units.Rate `toml:"upload_limit"`
	DownloadLimit *units.Rate `toml:"download_limit"`
}

// TransfersFor returns the transfer settings for a host alias, with its
// overrides applied.
func (c Config) TransfersFor(alias string) Transfers {
	t := c.Transfers
	h, ok := c.Hosts[alias]
	if !ok {
		return t
	}
	if h.Concurrency != nil {
		t.Concurrency = *h.Concurrency
	}
	if h.UploadLimit != nil {
		t.UploadLimit = *h.UploadLimit
	}
	if h.DownloadLimit != nil {
		t.DownloadLimit = *h.DownloadLimit
	}
	return t
}

// SetHostTransfers stores t as the override for alias.
func (c *Config) SetHostTransfers(alias string, t Transfers) {
	if c.Hosts == nil {
		c.Hosts = map[string]HostSettings{}
	}
	n, up, down := t.Concurrency, t.UploadLimit, t.DownloadLimit
	c.Hosts[alias] = HostSettings{Concurrency: &n, UploadLimit: &up, DownloadLimit: &down}
}

// Transfers configures the transfer queue.
type Transfers struct {
	// Concurrency is how many files transfer at the same time.
	Concurrency int `toml:"concurrency"`
	// UploadLimit and DownloadLimit cap total throughput across all
	// transfers in that direction. Zero means unlimited.
	UploadLimit   units.Rate `toml:"upload_limit"`
	DownloadLimit units.Rate `toml:"download_limit"`
}

// Default returns the built-in settings.
func Default() Config {
	return Config{
		Theme: DefaultTheme,
		Icons: IconsUnicode,
		Transfers: Transfers{
			Concurrency: DefaultConcurrency,
		},
	}
}

// DefaultPath returns the default config file location, e.g.
// ~/.config/sftpterm/config.toml on Linux.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config directory: %w", err)
	}
	return filepath.Join(dir, "sftpterm", "config.toml"), nil
}

// Load reads the config file at path on top of the defaults. A missing file is
// not an error. Unknown keys are reported as warnings rather than failures so
// that an older binary can still read a newer config.
func Load(path string) (cfg Config, warnings []string, err error) {
	cfg = Default()
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil, nil
	}
	if err != nil {
		return Config{}, nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		warnings = append(warnings, fmt.Sprintf("unknown config key %q", k.String()))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, warnings, nil
}

// Validate checks that every setting is within its accepted range.
func (c Config) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Theme) == "" {
		errs = append(errs, errors.New("theme must not be empty"))
	}
	if !slices.Contains(IconSets, c.Icons) {
		errs = append(errs, fmt.Errorf("icons must be one of %s, got %q", strings.Join(IconSets, ", "), c.Icons))
	}
	if n := c.Transfers.Concurrency; n < MinConcurrency || n > MaxConcurrency {
		errs = append(errs, fmt.Errorf("transfers.concurrency must be between %d and %d, got %d", MinConcurrency, MaxConcurrency, n))
	}
	for alias, h := range c.Hosts {
		if h.Concurrency != nil && (*h.Concurrency < MinConcurrency || *h.Concurrency > MaxConcurrency) {
			errs = append(errs, fmt.Errorf("hosts.%s.concurrency must be between %d and %d, got %d", alias, MinConcurrency, MaxConcurrency, *h.Concurrency))
		}
	}
	return errors.Join(errs...)
}

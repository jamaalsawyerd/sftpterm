// Package units formats and parses human-friendly byte sizes and transfer rates.
//
// All units are binary (1 KiB = 1024 bytes). When parsing, "K", "KB" and "KiB"
// are treated the same, since that is what people usually mean when they type
// a speed limit.
package units

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	KiB int64 = 1 << (10 * (iota + 1))
	MiB
	GiB
	TiB
)

var suffixes = []string{"B", "KiB", "MiB", "GiB", "TiB"}

// FormatBytes renders n as a short human-readable size, e.g. "512 B" or "1.5 MiB".
func FormatBytes(n int64) string {
	if n < 0 {
		return "-" + FormatBytes(-n)
	}
	if n < KiB {
		return strconv.FormatInt(n, 10) + " B"
	}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(suffixes)-1 {
		v /= 1024
		i++
	}
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + " " + suffixes[i]
}

// ParseBytes parses sizes such as "512", "10K", "1.5 MiB" or "2gb".
func ParseBytes(s string) (int64, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return 0, errors.New("empty size")
	}

	split := strings.IndexFunc(t, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' })
	num, unit := t, ""
	if split >= 0 {
		num, unit = t[:split], strings.TrimSpace(t[split:])
	}

	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, fmt.Errorf("invalid size %q", s)
	}

	var mult int64
	switch unit {
	case "", "b":
		mult = 1
	case "k", "kb", "kib":
		mult = KiB
	case "m", "mb", "mib":
		mult = MiB
	case "g", "gb", "gib":
		mult = GiB
	case "t", "tb", "tib":
		mult = TiB
	default:
		return 0, fmt.Errorf("invalid size %q: unknown unit %q", s, unit)
	}

	v := f * float64(mult)
	if v >= math.MaxInt64 {
		return 0, fmt.Errorf("invalid size %q: too large", s)
	}
	return int64(v), nil
}

// Rate is a transfer speed in bytes per second. Zero means unlimited.
type Rate int64

// ParseRate parses speed limits such as "10MB/s", "500K", "1.5 MiB/s",
// "unlimited", "off" or "0".
func ParseRate(s string) (Rate, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimSpace(strings.TrimSuffix(t, "/s"))
	switch t {
	case "", "0", "unlimited", "off", "none":
		return 0, nil
	}
	n, err := ParseBytes(t)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q", s)
	}
	if n == 0 {
		return 0, fmt.Errorf("invalid rate %q: rounds down to 0 bytes/s", s)
	}
	return Rate(n), nil
}

// Unlimited reports whether the rate imposes no limit.
func (r Rate) Unlimited() bool { return r <= 0 }

// String renders the rate as e.g. "10 MiB/s" or "unlimited".
func (r Rate) String() string {
	if r.Unlimited() {
		return "unlimited"
	}
	return FormatBytes(int64(r)) + "/s"
}

// MarshalText implements encoding.TextMarshaler.
func (r Rate) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (r *Rate) UnmarshalText(b []byte) error {
	v, err := ParseRate(string(b))
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// FormatDuration renders d compactly: "45s", "3m05s", "2h07m".
func FormatDuration(d time.Duration) string {
	switch s := int(d.Round(time.Second).Seconds()); {
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	case s < 60:
		return strconv.Itoa(s) + "s"
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	default:
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	}
}

// FormatRate renders bytes/second as e.g. "12.3 MiB/s".
func FormatRate(bps float64) string { return FormatBytes(int64(bps)) + "/s" }

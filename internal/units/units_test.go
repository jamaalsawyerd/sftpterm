package units

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "1.5 KiB"},
		{10 * MiB, "10 MiB"},
		{GiB + GiB/4, "1.2 GiB"},
		{3 * TiB, "3 TiB"},
		{-2048, "-2 KiB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"512", 512, false},
		{"10K", 10 * KiB, false},
		{"10 kb", 10 * KiB, false},
		{"1.5MiB", MiB + MiB/2, false},
		{" 2GB ", 2 * GiB, false},
		{"1t", TiB, false},
		{"", 0, true},
		{"abc", 0, true},
		{"10 parsecs", 0, true},
		{"-5K", 0, true},
		{"1.2.3M", 0, true},
		{"99999999999T", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseBytes(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseBytes(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseRate(t *testing.T) {
	tests := []struct {
		in      string
		want    Rate
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"unlimited", 0, false},
		{"OFF", 0, false},
		{"10MB/s", Rate(10 * MiB), false},
		{"500K", Rate(500 * KiB), false},
		{"1.5 MiB/s", Rate(MiB + MiB/2), false},
		{"0.1", 0, true},
		{"fast", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseRate(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseRate(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRate(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestRateRoundTrip(t *testing.T) {
	for _, r := range []Rate{0, 1, Rate(500 * KiB), Rate(10 * MiB), Rate(3 * GiB)} {
		b, err := r.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Rate
		if err := got.UnmarshalText(b); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", b, err)
		}
		if got != r {
			t.Errorf("round trip %d -> %q -> %d", r, b, got)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		250 * time.Millisecond:                "250ms",
		45 * time.Second:                      "45s",
		3*time.Minute + 5*time.Second:         "3m05s",
		2*time.Hour + 7*time.Minute:           "2h07m",
		59*time.Second + 600*time.Millisecond: "1m00s",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

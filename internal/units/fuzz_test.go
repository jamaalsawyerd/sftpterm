package units

import (
	"math"
	"testing"
)

func FuzzParseRate(f *testing.F) {
	for _, s := range []string{"10MB/s", "500K", "1.5 MiB/s", "unlimited", "0", "", "9e9T", "-1", "1.2.3", ".5k"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := ParseRate(s)
		if err != nil || r.Unlimited() {
			return
		}
		// What we print must parse back to (nearly) the same rate; printing
		// rounds to one decimal place.
		back, err := ParseRate(r.String())
		if err != nil {
			t.Fatalf("%q -> %d -> %q does not parse: %v", s, r, r.String(), err)
		}
		if diff := math.Abs(float64(back-r)) / float64(r); diff > 0.05 {
			t.Fatalf("%q -> %d -> %q -> %d (%.1f%% off)", s, r, r.String(), back, diff*100)
		}
	})
}

func FuzzParseBytes(f *testing.F) {
	for _, s := range []string{"512", "10K", "1.5MiB", "2gb", "", "x", "1e400"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if n, err := ParseBytes(s); err == nil && n < 0 {
			t.Fatalf("ParseBytes(%q) = %d", s, n)
		}
	})
}

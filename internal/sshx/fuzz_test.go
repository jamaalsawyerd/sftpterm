package sshx

import "testing"

func FuzzParseDestination(f *testing.F) {
	for _, s := range []string{"nas", "jsd@nas:/srv", "me@[::1]:/tmp", "sftp://u@h:22/p", "ssh://[fe80::1]", "@", "[", "a@b@c:d:e", "sftp://:0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDestination(s)
		if err != nil {
			return
		}
		if d.Host == "" {
			t.Fatalf("accepted %q with an empty host", s)
		}
		if d.Port < 0 || d.Port > 65535 {
			t.Fatalf("accepted %q with port %d", s, d.Port)
		}
	})
}

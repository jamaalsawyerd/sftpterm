package vfs

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{"plain", "a\x1b[31mb", "\x1b]52;c;aGk=\a", "tab\there", "\xff\xfe", "c1\u009b", "emoji 🎉"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Sanitize(s)
		if !utf8.ValidString(out) {
			t.Fatalf("Sanitize(%q) = %q is not valid UTF-8", s, out)
		}
		for _, r := range out {
			if unicode.IsControl(r) {
				t.Fatalf("Sanitize(%q) = %q still contains control character %U", s, out, r)
			}
		}
		if again := Sanitize(out); again != out {
			t.Fatalf("Sanitize is not idempotent: %q -> %q -> %q", s, out, again)
		}
	})
}

func FuzzCheckName(f *testing.F) {
	for _, s := range []string{"a.txt", "..", ".", "a/b", `a\b`, "x\x00y", "ok name", "\x1b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if CheckName(name) != nil {
			return
		}
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			t.Fatalf("accepted a name that can escape its directory: %q", name)
		}
		if Sanitize(name) != name {
			t.Fatalf("accepted a name that needs sanitizing: %q", name)
		}
	})
}

package vfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"a.txt", ".bashrc", "Ünïcødé 名前", "with space", "..dots"} {
		if err := CheckName(ok); err != nil {
			t.Errorf("CheckName(%q) = %v, want ok", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "../../etc/passwd", "a/b", `a\b`, "nul\x00byte", "esc\x1b[31m", "bell\a", "bad\xffutf8", "c1\u009b"} {
		if err := CheckName(bad); err == nil {
			t.Errorf("CheckName(%q) accepted an unsafe name", bad)
		}
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize("plain"); got != "plain" {
		t.Errorf("Sanitize changed clean text: %q", got)
	}
	evil := "file\x1b]52;c;ZXZpbA==\a.txt"
	got := Sanitize(evil)
	if strings.ContainsAny(got, "\x1b\a") {
		t.Fatalf("Sanitize left control bytes in %q", got)
	}
	if !strings.Contains(got, "file") || !strings.Contains(got, ".txt") {
		t.Errorf("Sanitize lost the printable parts: %q", got)
	}
	if got := Sanitize("a\tb"); got != "a    b" {
		t.Errorf("tab handling: %q", got)
	}
	if got := Sanitize("bad\xffbyte"); strings.ContainsRune(got, 0xff) || !strings.Contains(got, "�") {
		t.Errorf("invalid UTF-8 not repaired: %q", got)
	}
}

func TestRemoveAll(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	must(t, os.MkdirAll(filepath.Join(tree, "a", "b"), 0o755))
	must(t, os.WriteFile(filepath.Join(tree, "a", "b", "f"), []byte("x"), 0o644))
	must(t, os.WriteFile(filepath.Join(tree, "top"), []byte("x"), 0o644))

	// A symlink inside the tree to something outside must not be followed.
	outside := filepath.Join(root, "keep")
	must(t, os.Mkdir(outside, 0o755))
	must(t, os.WriteFile(filepath.Join(outside, "precious"), []byte("x"), 0o644))
	if err := os.Symlink(outside, filepath.Join(tree, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}

	must(t, RemoveAll(NewLocal(), tree))
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("tree still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "precious")); err != nil {
		t.Error("RemoveAll followed a symlink out of the tree")
	}
}

func TestReadHeadAndLooksBinary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	must(t, os.WriteFile(p, []byte("hello world"), 0o644))
	data, trunc, err := ReadHead(NewLocal(), p, 5)
	if err != nil || string(data) != "hello" || !trunc {
		t.Errorf("ReadHead(5) = %q, %v, %v", data, trunc, err)
	}
	data, trunc, err = ReadHead(NewLocal(), p, 100)
	if err != nil || string(data) != "hello world" || trunc {
		t.Errorf("ReadHead(100) = %q, %v, %v", data, trunc, err)
	}

	if LooksBinary([]byte("just some text\nwith lines\n")) || LooksBinary(nil) {
		t.Error("text detected as binary")
	}
	if !LooksBinary([]byte("PK\x03\x04\x00\x00binary")) {
		t.Error("NUL bytes not detected as binary")
	}
	garbage := make([]byte, 400)
	for i := range garbage {
		garbage[i] = byte(0x80 + i%64)
	}
	if !LooksBinary(garbage) {
		t.Error("invalid UTF-8 not detected as binary")
	}
	if LooksBinary([]byte(strings.Repeat("é", 100)[:199])) {
		t.Error("a rune cut off at the end made text look binary")
	}
}

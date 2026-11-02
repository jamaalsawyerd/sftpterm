package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Whatever the existing file looks like, Save must either refuse or write a
// file that loads back with the new value.
func FuzzSaveRoundTrip(f *testing.F) {
	f.Add("# comment\ntheme = \"dracula\" # x\n[transfers]\nconcurrency = 2\n", "nord")
	f.Add("[hosts.\"my box\"]\nupload_limit = \"1MB/s\"\n", "a \"quoted\" theme")
	f.Add("", "charm")
	f.Add("theme='single'\n[transfers] # trailing\n", "x#y")
	f.Fuzz(func(t *testing.T, existing, theme string) {
		if theme == "" {
			return
		}
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := Load(path)
		if err != nil {
			return // not a valid config to begin with
		}
		cfg.Theme = theme
		cfg.Transfers.Concurrency = 7
		if err := Save(path, cfg); err != nil {
			return
		}
		got, _, err := Load(path)
		if err != nil {
			b, _ := os.ReadFile(path)
			t.Fatalf("saved file does not load: %v\n%s", err, b)
		}
		if got.Theme != theme || got.Transfers.Concurrency != 7 {
			b, _ := os.ReadFile(path)
			t.Fatalf("round trip lost changes: theme %q concurrency %d\n%s", got.Theme, got.Transfers.Concurrency, b)
		}
	})
}

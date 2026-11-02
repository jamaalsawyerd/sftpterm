package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, warnings, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("got %+v, want defaults %+v", cfg, Default())
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `
theme = "dracula"
show_hidden = true

[transfers]
concurrency = 5
upload_limit = "10MB/s"
`)
	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	want := Config{
		Theme:      "dracula",
		Icons:      IconsUnicode, // untouched default
		ShowHidden: true,
		Transfers: Transfers{
			Concurrency:   5,
			UploadLimit:   units.Rate(10 * units.MiB),
			DownloadLimit: 0,
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadWarnsOnUnknownKeys(t *testing.T) {
	path := writeConfig(t, "theme = \"nord\"\nsparkles = true\n")
	_, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "sparkles") {
		t.Errorf("warnings = %v, want one mentioning sparkles", warnings)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := map[string]string{
		"concurrency too high": "[transfers]\nconcurrency = 99\n",
		"concurrency zero":     "[transfers]\nconcurrency = 0\n",
		"bad icons":            "icons = \"hieroglyphs\"\n",
		"bad rate":             "[transfers]\ndownload_limit = \"warp 9\"\n",
		"empty theme":          "theme = \"\"\n",
		"malformed toml":       "theme = \n",
		"bad host override":    "[hosts.nas]\nconcurrency = 50\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load(writeConfig(t, body)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

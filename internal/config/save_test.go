package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

func roundTrip(t *testing.T, path string, cfg Config) (Config, string) {
	t.Helper()
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("saved file doesn't load: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("warnings after save: %v", warnings)
	}
	b, _ := os.ReadFile(path)
	return got, string(b)
}

func TestSaveNewFileOnlyWritesNonDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	cfg := Default()
	cfg.Theme = "nord"
	got, text := roundTrip(t, path, cfg)
	if got.Theme != "nord" {
		t.Errorf("theme = %q", got.Theme)
	}
	if strings.Contains(text, "concurrency") || strings.Contains(text, "icons") {
		t.Errorf("defaults were written:\n%s", text)
	}
}

func TestSavePreservesCommentsAndUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := `# my sftpterm config
theme = "dracula"   # spooky

future_option = 42 # from a newer version

[transfers]
# be gentle with the NAS
concurrency = 2
`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Theme = "tokyo-night"
	cfg.Transfers.Concurrency = 5
	cfg.Transfers.UploadLimit = units.Rate(10 * units.MiB)
	cfg.SetHostTransfers("nas", Transfers{Concurrency: 1, UploadLimit: units.Rate(units.MiB)})
	cfg.SetHostTransfers("my laptop", Transfers{Concurrency: 4})

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	for _, want := range []string{
		"# my sftpterm config",
		`theme = "tokyo-night" # spooky`,
		"future_option = 42 # from a newer version",
		"# be gentle with the NAS",
		"concurrency = 5",
		`upload_limit = "10 MiB/s"`,
		"[hosts.nas]",
		`[hosts."my laptop"]`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	// The new transfers key went into the existing [transfers] table.
	if strings.Count(text, "[transfers]") != 1 {
		t.Errorf("duplicated table:\n%s", text)
	}

	got, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "future_option") {
		t.Errorf("warnings = %v", warnings)
	}
	if !reflect.DeepEqual(got.Hosts, cfg.Hosts) || got.Transfers != cfg.Transfers || got.Theme != cfg.Theme {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, cfg)
	}
	if tr := got.TransfersFor("nas"); tr.Concurrency != 1 || tr.UploadLimit != units.Rate(units.MiB) {
		t.Errorf("TransfersFor(nas) = %+v", tr)
	}
	if tr := got.TransfersFor("other"); tr != got.Transfers {
		t.Errorf("TransfersFor(other) = %+v, want global", tr)
	}
}

func TestSaveAddsRootKeyBeforeTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[transfers]\nconcurrency = 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ShowHidden = true
	got, text := roundTrip(t, path, cfg)
	if !got.ShowHidden || got.Transfers.Concurrency != 4 {
		t.Errorf("got %+v\n%s", got, text)
	}
	if strings.Index(text, "show_hidden") > strings.Index(text, "[transfers]") {
		t.Errorf("root key placed inside a table:\n%s", text)
	}
}

func TestSaveRejectsInvalidConfig(t *testing.T) {
	cfg := Default()
	cfg.Transfers.Concurrency = 0
	if err := Save(filepath.Join(t.TempDir(), "c.toml"), cfg); err == nil {
		t.Error("expected an error for an invalid config")
	}
}

func TestSplitKeyPath(t *testing.T) {
	for in, want := range map[string][]string{
		`hosts.nas`:       {"hosts", "nas"},
		`hosts."my host"`: {"hosts", "my host"},
		` hosts . 'a.b' `: {"hosts", "a.b"},
		`hosts."q\"uote"`: {"hosts", `q"uote`},
	} {
		if got := splitKeyPath(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitKeyPath(%q) = %q, want %q", in, got, want)
		}
	}
}

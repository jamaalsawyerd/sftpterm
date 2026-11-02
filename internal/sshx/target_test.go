package sshx

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestParseDestination(t *testing.T) {
	tests := []struct {
		in      string
		want    Destination
		wantErr bool
	}{
		{"nas", Destination{Host: "nas"}, false},
		{"jsd@nas", Destination{User: "jsd", Host: "nas"}, false},
		{"jsd@nas:/srv/media", Destination{User: "jsd", Host: "nas", Path: "/srv/media"}, false},
		{"nas:backups", Destination{Host: "nas", Path: "backups"}, false},
		{"nas:/odd@path", Destination{Host: "nas", Path: "/odd@path"}, false},
		{"me@[::1]:/tmp", Destination{User: "me", Host: "::1", Path: "/tmp"}, false},
		{"[fe80::1]", Destination{Host: "fe80::1"}, false},
		{"sftp://jsd@nas:2222/srv", Destination{User: "jsd", Host: "nas", Port: 2222, Path: "/srv"}, false},
		{"sftp://nas", Destination{Host: "nas"}, false},
		{"ssh://[::1]:22", Destination{Host: "::1", Port: 22}, false},
		{"", Destination{}, true},
		{"jsd@", Destination{}, true},
		{"[::1", Destination{}, true},
		{"[::1]x", Destination{}, true},
		{"sftp://nas:99999", Destination{}, true},
	}
	for _, tt := range tests {
		got, err := ParseDestination(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseDestination(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDestination(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

const sampleConfig = `
Host *
    ServerAliveInterval 60

Host nas
    HostName 192.168.1.5
    User jsd
    Port 2222
    IdentityFile ~/.ssh/nas_ed25519
    IdentitiesOnly yes

Host pi pi-alias
    HostName %h.local
    UserKnownHostsFile ~/.ssh/pi_known_hosts

Host *.internal !secret.internal
    User admin

Host web-?? nas
    User ignored-because-nas-already-matched
`

func TestSSHConfigHosts(t *testing.T) {
	c, err := parseSSHConfig([]byte(sampleConfig), "/home/jsd")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nas", "pi", "pi-alias"}
	if got := c.Hosts(); !slices.Equal(got, want) {
		t.Errorf("Hosts() = %v, want %v", got, want)
	}
}

func TestSSHConfigResolve(t *testing.T) {
	home := filepath.FromSlash("/home/jsd")
	c, err := parseSSHConfig([]byte(sampleConfig), home)
	if err != nil {
		t.Fatal(err)
	}

	nas := c.Resolve("nas")
	if nas.Host != "192.168.1.5" || nas.Port != 2222 || nas.User != "jsd" || !nas.IdentitiesOnly {
		t.Errorf("nas = %+v", nas)
	}
	if want := []string{filepath.Join(home, ".ssh", "nas_ed25519")}; !slices.Equal(nas.IdentityFiles, want) {
		t.Errorf("nas identities = %v, want %v", nas.IdentityFiles, want)
	}
	if want := []string{filepath.Join(home, ".ssh", "known_hosts")}; !slices.Equal(nas.KnownHostsFiles, want) {
		t.Errorf("nas known hosts = %v, want %v", nas.KnownHostsFiles, want)
	}
	if nas.String() != "jsd@nas:2222" || nas.Addr() != "192.168.1.5:2222" {
		t.Errorf("String/Addr = %q / %q", nas.String(), nas.Addr())
	}

	pi := c.Resolve("pi")
	if pi.Host != "pi.local" || pi.Port != DefaultPort {
		t.Errorf("pi = %+v", pi)
	}
	if want := []string{filepath.Join(home, ".ssh", "pi_known_hosts")}; !slices.Equal(pi.KnownHostsFiles, want) {
		t.Errorf("pi known hosts = %v", pi.KnownHostsFiles)
	}

	if box := c.Resolve("box.internal"); box.User != "admin" {
		t.Errorf("wildcard user not applied: %+v", box)
	}

	unknown := c.Resolve("example.com")
	if unknown.Host != "example.com" || unknown.Port != DefaultPort || unknown.User == "" {
		t.Errorf("unknown host = %+v", unknown)
	}
}

func TestTargetForOverridesConfig(t *testing.T) {
	c, err := parseSSHConfig([]byte(sampleConfig), "/home/jsd")
	if err != nil {
		t.Fatal(err)
	}
	got := c.TargetFor(Destination{User: "root", Host: "nas", Port: 22, Path: "/srv"})
	if got.User != "root" || got.Port != 22 || got.Host != "192.168.1.5" || got.Path != "/srv" {
		t.Errorf("TargetFor = %+v", got)
	}
}

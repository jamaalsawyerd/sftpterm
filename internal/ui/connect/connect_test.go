package connect

import "testing"

func TestParseKey(t *testing.T) {
	tests := []struct {
		key  string
		user string
		host string
		port int
		ok   bool
	}{
		{"jsd@nas", "jsd", "nas", 0, true},
		{"jsd@nas:2222", "jsd", "nas", 2222, true},
		{"tester@127.0.0.1:42231", "tester", "127.0.0.1", 42231, true},
		{"me@::1", "me", "::1", 0, true},
		{"nobody", "", "", 0, false},
		{"@host", "", "", 0, false},
	}
	for _, tt := range tests {
		d, ok := parseKey(tt.key)
		if ok != tt.ok || d.User != tt.user || d.Host != tt.host || d.Port != tt.port {
			t.Errorf("parseKey(%q) = %+v, %v", tt.key, d, ok)
		}
	}
}

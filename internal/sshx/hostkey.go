package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// ErrHostKeyRejected means the user declined to trust an unknown host.
var ErrHostKeyRejected = errors.New("host key not trusted")

// HostKeyChangedError means the server presented a different key than the one
// in known_hosts. This is what a man-in-the-middle attack looks like, so it is
// never overridable from the UI.
type HostKeyChangedError struct {
	Host        string
	Fingerprint string
	File        string
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("HOST KEY CHANGED for %s (now %s). This could be a man-in-the-middle attack. "+
		"If the change is expected, remove the old entry from %s and reconnect.", e.Host, e.Fingerprint, e.File)
}

// hostKeyCallback verifies host keys against the target's known_hosts files,
// asking the prompter about unknown hosts and recording accepted keys in the
// first file. It also returns the host key algorithms to prefer, so a server
// with several keys presents the one we already know.
func hostKeyCallback(ctx context.Context, t Target, p Prompter) (ssh.HostKeyCallback, []string, error) {
	if len(t.KnownHostsFiles) == 0 {
		return nil, nil, errors.New("no known_hosts file configured")
	}
	primary := t.KnownHostsFiles[0]
	if err := ensureFile(primary); err != nil {
		return nil, nil, fmt.Errorf("preparing known_hosts: %w", err)
	}

	var files []string
	for _, f := range append(t.KnownHostsFiles, globalKnownHosts...) {
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		}
	}
	db, err := knownhosts.NewDB(files...)
	if err != nil {
		return nil, nil, fmt.Errorf("reading known_hosts: %w", err)
	}
	check := db.HostKeyCallback()

	cb := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		switch {
		case err == nil:
			return nil
		case knownhosts.IsHostKeyChanged(err):
			return &HostKeyChangedError{Host: t.Alias, Fingerprint: ssh.FingerprintSHA256(key), File: primary}
		case knownhosts.IsHostUnknown(err):
			ok, err := p.ConfirmHostKey(ctx, hostKeyInfo(t, hostname, remote, key, primary))
			if err != nil {
				return err
			}
			if !ok {
				return ErrHostKeyRejected
			}
			return appendKnownHost(primary, hostname, remote, key)
		default:
			return err
		}
	}
	return cb, db.HostKeyAlgorithms(t.Addr()), nil
}

var globalKnownHosts = []string{"/etc/ssh/ssh_known_hosts"}

func appendKnownHost(file, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("saving host key: %w", err)
	}
	if err := knownhosts.WriteKnownHost(f, hostname, remote, key); err != nil {
		_ = f.Close()
		return fmt.Errorf("saving host key: %w", err)
	}
	return f.Close()
}

func ensureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

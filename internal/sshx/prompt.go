package sshx

import (
	"context"
	"errors"
	"net"

	"golang.org/x/crypto/ssh"
)

// ErrCancelled is returned by a Prompter when the user dismisses a prompt.
var ErrCancelled = errors.New("cancelled")

// Question is a request for text from the user, e.g. a password.
type Question struct {
	Title  string // short heading, e.g. "Password"
	Prompt string // e.g. "Password for jsd@nas"
	Secret bool   // hide what is typed
}

// HostKeyInfo describes a host key the user is asked to trust.
type HostKeyInfo struct {
	Alias       string
	Hostname    string // host[:port] as written to known_hosts
	Remote      net.Addr
	KeyType     string
	Fingerprint string // SHA256:...
	File        string // where the key will be saved
}

// Prompter asks the user questions during a connection attempt. Methods are
// called from the connecting goroutine and may block until the user answers;
// they must return promptly when ctx is cancelled.
type Prompter interface {
	Ask(ctx context.Context, q Question) (string, error)
	ConfirmHostKey(ctx context.Context, info HostKeyInfo) (bool, error)
}

func hostKeyInfo(t Target, hostname string, remote net.Addr, key ssh.PublicKey, file string) HostKeyInfo {
	return HostKeyInfo{
		Alias:       t.Alias,
		Hostname:    hostname,
		Remote:      remote,
		KeyType:     key.Type(),
		Fingerprint: ssh.FingerprintSHA256(key),
		File:        file,
	}
}

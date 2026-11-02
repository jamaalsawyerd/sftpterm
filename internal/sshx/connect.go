package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/log/v2"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ErrAuthFailed means every authentication method was rejected.
var ErrAuthFailed = errors.New("authentication failed")

const (
	dialTimeout       = 15 * time.Second
	keepAliveInterval = 30 * time.Second
)

// Options tune Connect.
type Options struct {
	// Prompter answers passphrase, password, and host key questions. Required.
	Prompter Prompter
	// Progress, if set, receives short status updates ("connecting", ...).
	Progress func(stage string)
}

// Conn is an authenticated SSH connection with an SFTP session on top.
type Conn struct {
	Target Target
	SSH    *ssh.Client
	SFTP   *sftp.Client
}

// Close ends the SFTP session and the SSH connection.
func (c *Conn) Close() error {
	return errors.Join(c.SFTP.Close(), c.SSH.Close())
}

// Wait blocks until the connection closes, returning why.
func (c *Conn) Wait() error { return c.SSH.Wait() }

// Connect dials t, verifies its host key, authenticates, and opens SFTP.
// Cancelling ctx aborts the attempt, including any prompt in progress.
func Connect(ctx context.Context, t Target, opts Options) (*Conn, error) {
	if opts.Prompter == nil {
		return nil, errors.New("sshx: Options.Prompter is required")
	}
	progress := opts.Progress
	if progress == nil {
		progress = func(string) {}
	}

	// A cancelled prompt must abort the whole attempt. x/crypto/ssh swallows
	// errors from auth callbacks and moves on to the next method, so cancel
	// the attempt's context instead, which closes the socket.
	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	prompter := &abortingPrompter{Prompter: opts.Prompter, abort: cancel}

	hostKeys, algos, err := hostKeyCallback(ctx, t, prompter)
	if err != nil {
		return nil, err
	}

	var ag agent.Agent
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if c, err := net.Dial("unix", sock); err == nil {
			defer c.Close()
			ag = agent.NewClient(c)
		} else {
			log.Debug("ssh agent unavailable", "err", err)
		}
	}

	cfg := &ssh.ClientConfig{
		User:              t.User,
		Auth:              authMethods(ctx, t, prompter, ag),
		HostKeyCallback:   hostKeys,
		HostKeyAlgorithms: algos,
	}

	progress("connecting to " + t.Addr())
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", t.Addr())
	if err != nil {
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, err
	}
	// The handshake has no context support; closing the socket unblocks it.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	progress("authenticating as " + t.User)
	c, chans, reqs, err := ssh.NewClientConn(conn, t.Addr(), cfg)
	if err != nil {
		_ = conn.Close()
		switch {
		case prompter.cancelled.Load():
			return nil, ErrCancelled
		case parent.Err() != nil:
			return nil, parent.Err()
		}
		return nil, describeHandshakeError(err)
	}
	client := ssh.NewClient(c, chans, reqs)

	progress("starting sftp")
	sc, err := sftp.NewClient(client,
		sftp.UseConcurrentWrites(true),
		sftp.UseConcurrentReads(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("starting sftp (is the sftp subsystem enabled on the server?): %w", err)
	}

	go keepAlive(client)
	return &Conn{Target: t, SSH: client, SFTP: sc}, nil
}

// keepAlive pings the server so idle connections survive NAT timeouts and
// dead ones are noticed.
func keepAlive(c *ssh.Client) {
	t := time.NewTicker(keepAliveInterval)
	defer t.Stop()
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				log.Debug("keepalive failed", "err", err)
				_ = c.Close()
				return
			}
		}
	}
}

// describeHandshakeError unwraps the errors users care about from the
// handshake's generic wrapping.
func describeHandshakeError(err error) error {
	var changed *HostKeyChangedError
	switch {
	case errors.As(err, &changed):
		return changed
	case errors.Is(err, ErrHostKeyRejected):
		return ErrHostKeyRejected
	case errors.Is(err, ErrCancelled):
		return ErrCancelled
	case strings.Contains(err.Error(), "unable to authenticate"):
		return fmt.Errorf("%w: %s", ErrAuthFailed, strings.TrimPrefix(err.Error(), "ssh: handshake failed: ssh: "))
	}
	return err
}

// abortingPrompter cancels the connection attempt when the user dismisses a
// prompt.
type abortingPrompter struct {
	Prompter
	abort     context.CancelFunc
	cancelled atomic.Bool
}

func (p *abortingPrompter) Ask(ctx context.Context, q Question) (string, error) {
	a, err := p.Prompter.Ask(ctx, q)
	if errors.Is(err, ErrCancelled) {
		p.cancelled.Store(true)
		p.abort()
	}
	return a, err
}

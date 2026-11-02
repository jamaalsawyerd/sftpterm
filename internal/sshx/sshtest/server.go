// Package sshtest runs an in-process SSH server with an SFTP subsystem for
// tests, so connection and transfer code can be exercised without Docker or a
// real host.
package sshtest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Options configure which authentication methods the server accepts.
type Options struct {
	User string // defaults to "tester"
	// Password enables password auth when non-empty.
	Password string
	// AuthorizedKeys enables public key auth for these keys.
	AuthorizedKeys []ssh.PublicKey
	// KeyboardInteractive enables a one-question challenge whose answer is
	// Password.
	KeyboardInteractive bool
	// Root is the SFTP working directory (defaults to a temp dir).
	Root string
}

// Server is a running test server.
type Server struct {
	Addr    string
	Host    string
	Port    int
	User    string
	Root    string
	HostKey ssh.Signer

	ln   net.Listener
	quit chan struct{}
	wg   sync.WaitGroup
}

// New starts a server that is shut down when the test ends.
func New(t testing.TB, opts Options) *Server {
	t.Helper()
	if opts.User == "" {
		opts.User = "tester"
	}
	if opts.Root == "" {
		opts.Root = t.TempDir()
	}

	cfg := &ssh.ServerConfig{}
	if opts.Password != "" {
		cfg.PasswordCallback = func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == opts.User && string(pass) == opts.Password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}
	if len(opts.AuthorizedKeys) > 0 {
		cfg.PublicKeyCallback = func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, k := range opts.AuthorizedKeys {
				if c.User() == opts.User && bytes.Equal(k.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, errors.New("key not authorized")
		}
	}
	if opts.KeyboardInteractive {
		cfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, ch ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := ch("", "", []string{"Verification code: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if c.User() == opts.User && len(answers) == 1 && answers[0] == opts.Password {
				return nil, nil
			}
			return nil, errors.New("wrong answer")
		}
	}

	hostKey := NewSigner(t)
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	s := &Server{
		Addr: ln.Addr().String(), Host: addr.IP.String(), Port: addr.Port,
		User: opts.User, Root: opts.Root, HostKey: hostKey, ln: ln,
		quit: make(chan struct{}),
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.serve(conn, cfg)
			}()
		}
	}()
	t.Cleanup(func() {
		close(s.quit)
		_ = ln.Close()
		s.wg.Wait()
	})
	return s
}

func (s *Server) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	defer conn.Close()
	// Drop the connection when the server shuts down, even mid-handshake,
	// so cleanup can't hang on a client that never finishes.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-s.quit:
			_ = conn.Close()
		}
	}()

	sc, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go s.replyKeepAlives(reqs)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.session(ch, chReqs)
	}
}

func (s *Server) replyKeepAlives(reqs <-chan *ssh.Request) {
	for r := range reqs {
		if r.WantReply {
			_ = r.Reply(r.Type == "keepalive@openssh.com", nil)
		}
	}
}

func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for r := range reqs {
		if r.Type == "subsystem" && len(r.Payload) > 4 && string(r.Payload[4:]) == "sftp" {
			_ = r.Reply(true, nil)
			srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(s.Root))
			if err != nil {
				return
			}
			if err := srv.Serve(); err != nil && !errors.Is(err, io.EOF) {
				_ = srv.Close()
			}
			return
		}
		if r.WantReply {
			_ = r.Reply(false, nil)
		}
	}
}

// NewSigner generates a fresh ed25519 key.
func NewSigner(t testing.TB) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

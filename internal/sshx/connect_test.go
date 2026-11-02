package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx/sshtest"
)

// scriptedPrompter answers prompts from a queue and records what was asked.
type scriptedPrompter struct {
	mu        sync.Mutex
	answers   []string
	trust     bool
	asked     []Question
	hostKeys  []HostKeyInfo
	blockUser bool // block until ctx is cancelled
}

func (p *scriptedPrompter) Ask(ctx context.Context, q Question) (string, error) {
	p.mu.Lock()
	p.asked = append(p.asked, q)
	block := p.blockUser
	var a string
	ok := len(p.answers) > 0
	if ok {
		a, p.answers = p.answers[0], p.answers[1:]
	}
	p.mu.Unlock()
	if block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if !ok {
		return "", ErrCancelled
	}
	return a, nil
}

func (p *scriptedPrompter) ConfirmHostKey(_ context.Context, info HostKeyInfo) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hostKeys = append(p.hostKeys, info)
	return p.trust, nil
}

// isolate keeps tests away from the developer's real agent and keys.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func targetFor(t *testing.T, srv *sshtest.Server, home string) Target {
	t.Helper()
	return Target{
		Alias:           "test",
		Host:            srv.Host,
		Port:            srv.Port,
		User:            srv.User,
		IdentitiesOnly:  true,
		KnownHostsFiles: []string{filepath.Join(home, ".ssh", "known_hosts")},
	}
}

func connect(t *testing.T, tgt Target, p Prompter) (*Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Connect(ctx, tgt, Options{Prompter: p})
	if err == nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	return c, err
}

func TestConnectWithPasswordAndTrustOnFirstUse(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "hunter2"})
	tgt := targetFor(t, srv, home)

	p := &scriptedPrompter{answers: []string{"wrong", "hunter2"}, trust: true}
	c, err := connect(t, tgt, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.hostKeys) != 1 {
		t.Fatalf("host key prompts = %d, want 1", len(p.hostKeys))
	}
	if got, want := p.hostKeys[0].Fingerprint, ssh.FingerprintSHA256(srv.HostKey.PublicKey()); got != want {
		t.Errorf("fingerprint = %s, want %s", got, want)
	}
	if len(p.asked) != 2 || !p.asked[0].Secret {
		t.Errorf("password prompts = %+v", p.asked)
	}
	wd, err := c.SFTP.Getwd()
	if err != nil || wd == "" {
		t.Fatalf("sftp Getwd = %q, %v", wd, err)
	}

	// The key was saved, so reconnecting doesn't ask again.
	kh, err := os.ReadFile(tgt.KnownHostsFiles[0])
	if err != nil || !strings.Contains(string(kh), srv.HostKey.PublicKey().Type()) {
		t.Fatalf("known_hosts = %q, %v", kh, err)
	}
	p2 := &scriptedPrompter{answers: []string{"hunter2"}}
	if _, err := connect(t, tgt, p2); err != nil {
		t.Fatal(err)
	}
	if len(p2.hostKeys) != 0 {
		t.Error("asked to trust an already known host")
	}
}

func TestRejectingUnknownHostKey(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	tgt := targetFor(t, srv, home)

	_, err := connect(t, tgt, &scriptedPrompter{answers: []string{"pw"}, trust: false})
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("err = %v, want ErrHostKeyRejected", err)
	}
	if kh, _ := os.ReadFile(tgt.KnownHostsFiles[0]); len(kh) != 0 {
		t.Errorf("rejected key was saved: %q", kh)
	}
}

func TestChangedHostKeyIsFatal(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	tgt := targetFor(t, srv, home)

	// Pretend we previously saw a different key for this address.
	other := sshtest.NewSigner(t).PublicKey()
	line := "[" + srv.Host + "]:" + itoa(srv.Port) + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(other)))
	if err := os.MkdirAll(filepath.Dir(tgt.KnownHostsFiles[0]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tgt.KnownHostsFiles[0], []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &scriptedPrompter{answers: []string{"pw"}, trust: true}
	_, err := connect(t, tgt, p)
	var changed *HostKeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("err = %v, want HostKeyChangedError", err)
	}
	if len(p.hostKeys) != 0 {
		t.Error("user was offered to trust a changed key")
	}
}

func writeEncryptedKey(t *testing.T, dir, passphrase string) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_test")
	if err := os.WriteFile(path, pemEncode(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return path, signer.PublicKey()
}

func TestEncryptedKeyAsksForPassphraseWithRetry(t *testing.T) {
	home := isolate(t)
	keyPath, pub := writeEncryptedKey(t, home, "open sesame")
	srv := sshtest.New(t, sshtest.Options{AuthorizedKeys: []ssh.PublicKey{pub}})
	tgt := targetFor(t, srv, home)
	tgt.IdentityFiles = []string{keyPath}

	p := &scriptedPrompter{answers: []string{"nope", "open sesame"}, trust: true}
	if _, err := connect(t, tgt, p); err != nil {
		t.Fatal(err)
	}
	if len(p.asked) != 2 || !strings.Contains(p.asked[1].Prompt, "Wrong passphrase") {
		t.Errorf("prompts = %+v", p.asked)
	}
}

func TestPassphraseNotRequestedForUnacceptedKey(t *testing.T) {
	home := isolate(t)
	keyPath, _ := writeEncryptedKey(t, home, "secret")
	srv := sshtest.New(t, sshtest.Options{Password: "pw"}) // key not authorized
	tgt := targetFor(t, srv, home)
	tgt.IdentityFiles = []string{keyPath}

	p := &scriptedPrompter{answers: []string{"pw"}, trust: true}
	if _, err := connect(t, tgt, p); err != nil {
		t.Fatal(err)
	}
	for _, q := range p.asked {
		if strings.Contains(q.Prompt, "Passphrase") {
			t.Errorf("asked for a passphrase of a key the server never accepted: %+v", q)
		}
	}
}

func TestKeyboardInteractive(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "123456", KeyboardInteractive: true})
	tgt := targetFor(t, srv, home)

	// Password auth is also enabled; answer it wrongly 3 times so the client
	// moves on to keyboard-interactive.
	p := &scriptedPrompter{answers: []string{"x", "x", "x", "123456"}, trust: true}
	if _, err := connect(t, tgt, p); err != nil {
		t.Fatal(err)
	}
	last := p.asked[len(p.asked)-1]
	if !strings.Contains(last.Prompt, "Verification code") || !last.Secret {
		t.Errorf("last prompt = %+v", last)
	}
}

func TestAuthFailure(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "right"})
	tgt := targetFor(t, srv, home)

	_, err := connect(t, tgt, &scriptedPrompter{answers: []string{"a", "b", "c"}, trust: true})
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}

func TestCancelledPromptAbortsConnection(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	tgt := targetFor(t, srv, home)

	_, err := connect(t, tgt, &scriptedPrompter{trust: true}) // no answers → ErrCancelled
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestContextCancelDuringPrompt(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	tgt := targetFor(t, srv, home)

	ctx, cancel := context.WithCancel(context.Background())
	p := &scriptedPrompter{trust: true, blockUser: true}
	errc := make(chan error, 1)
	go func() {
		_, err := Connect(ctx, tgt, Options{Prompter: p})
		errc <- err
	}()
	time.AfterFunc(200*time.Millisecond, cancel)
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Connect did not return after cancel")
	}
}

// Regression: x/crypto/ssh swallows auth callback errors and moves on to the
// next method, so a cancel after a failed attempt used to surface as a
// protocol error instead of ErrCancelled.
func TestCancelAfterWrongPassword(t *testing.T) {
	home := isolate(t)
	srv := sshtest.New(t, sshtest.Options{Password: "pw", KeyboardInteractive: true})
	tgt := targetFor(t, srv, home)

	p := &scriptedPrompter{answers: []string{"wrong"}, trust: true}
	_, err := connect(t, tgt, p)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if len(p.asked) != 2 {
		t.Errorf("asked %d times after cancelling, want 2 (wrong, then cancel)", len(p.asked))
	}
}

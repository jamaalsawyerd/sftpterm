package sshx

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"charm.land/log/v2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// defaultIdentities are tried when the config doesn't restrict identities,
// mirroring OpenSSH.
var defaultIdentities = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

const maxPassphraseAttempts = 3

// authMethods builds the auth chain: public keys (agent first, then key
// files), then password, then keyboard-interactive. x/crypto/ssh tries each
// method type at most once, so all keys share one publickey method.
func authMethods(ctx context.Context, t Target, p Prompter, ag agent.Agent) []ssh.AuthMethod {
	signers := func() ([]ssh.Signer, error) { return collectSigners(ctx, t, p, ag), nil }
	password := func() (string, error) {
		return p.Ask(ctx, Question{Title: "Password", Prompt: "Password for " + t.String(), Secret: true})
	}
	interactive := func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i, q := range questions {
			title := name
			if title == "" {
				title = "Authentication"
			}
			prompt := q
			if instruction != "" {
				prompt = instruction + "\n" + q
			}
			a, err := p.Ask(ctx, Question{Title: title, Prompt: prompt, Secret: !echos[i]})
			if err != nil {
				return nil, err
			}
			answers[i] = a
		}
		return answers, nil
	}
	return []ssh.AuthMethod{
		ssh.PublicKeysCallback(signers),
		ssh.RetryableAuthMethod(ssh.PasswordCallback(password), 3),
		ssh.RetryableAuthMethod(ssh.KeyboardInteractive(interactive), 3),
	}
}

func collectSigners(ctx context.Context, t Target, p Prompter, ag agent.Agent) []ssh.Signer {
	var out []ssh.Signer
	seen := map[string]bool{}
	add := func(s ssh.Signer) {
		fp := ssh.FingerprintSHA256(s.PublicKey())
		if !seen[fp] {
			seen[fp] = true
			out = append(out, s)
		}
	}

	if ag != nil {
		if ss, err := ag.Signers(); err == nil {
			for _, s := range ss {
				add(s)
			}
		} else {
			log.Debug("ssh agent signers", "err", err)
		}
	}

	for _, path := range identityFiles(t) {
		pem, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Debug("reading identity", "path", path, "err", err)
			}
			continue
		}
		s, err := ssh.ParsePrivateKey(pem)
		var missing *ssh.PassphraseMissingError
		switch {
		case err == nil:
			add(s)
		case errors.As(err, &missing):
			pub := missing.PublicKey
			if pub == nil {
				pub = readPublicKey(path + ".pub")
			}
			if pub == nil {
				log.Debug("encrypted key without public half; skipping", "path", path)
				continue
			}
			add(&lazySigner{ctx: ctx, pub: pub, pem: pem, path: path, prompter: p})
		default:
			log.Debug("parsing identity", "path", path, "err", err)
		}
	}
	return out
}

func identityFiles(t Target) []string {
	files := slices.Clone(t.IdentityFiles)
	if !t.IdentitiesOnly || len(files) == 0 {
		home, _ := os.UserHomeDir()
		for _, name := range defaultIdentities {
			files = append(files, filepath.Join(home, ".ssh", name))
		}
	}
	seen := map[string]bool{}
	return slices.DeleteFunc(files, func(f string) bool {
		dup := seen[f]
		seen[f] = true
		return dup
	})
}

func readPublicKey(path string) ssh.PublicKey {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(b)
	if err != nil {
		return nil
	}
	return pub
}

// lazySigner wraps an encrypted private key and only asks for its passphrase
// when the server has accepted the public key and a signature is needed.
type lazySigner struct {
	ctx      context.Context
	pub      ssh.PublicKey
	pem      []byte
	path     string
	prompter Prompter

	once   sync.Once
	signer ssh.Signer
	err    error
}

var _ ssh.AlgorithmSigner = (*lazySigner)(nil)

func (s *lazySigner) PublicKey() ssh.PublicKey { return s.pub }

func (s *lazySigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	signer, err := s.load()
	if err != nil {
		return nil, err
	}
	return signer.Sign(rand, data)
}

func (s *lazySigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	signer, err := s.load()
	if err != nil {
		return nil, err
	}
	as, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("key %s does not support algorithm %s", s.path, algorithm)
	}
	return as.SignWithAlgorithm(rand, data, algorithm)
}

func (s *lazySigner) load() (ssh.Signer, error) {
	s.once.Do(func() {
		prompt := "Passphrase for " + s.path
		for range maxPassphraseAttempts {
			pass, err := s.prompter.Ask(s.ctx, Question{Title: "Key passphrase", Prompt: prompt, Secret: true})
			if err != nil {
				s.err = err
				return
			}
			signer, err := ssh.ParsePrivateKeyWithPassphrase(s.pem, []byte(pass))
			if errors.Is(err, x509.IncorrectPasswordError) {
				prompt = "Wrong passphrase. Try again for " + s.path
				continue
			}
			if err == nil && !bytes.Equal(signer.PublicKey().Marshal(), s.pub.Marshal()) {
				err = fmt.Errorf("%s does not match its public key", s.path)
			}
			s.signer, s.err = signer, err
			return
		}
		s.err = fmt.Errorf("too many wrong passphrases for %s", s.path)
	})
	return s.signer, s.err
}

package credstore

// Real OS-keyring backends, per the Secret-Handling Standard §1.4 and
// §2.1. Together with linuxfallback.go, this file isolates the
// github.com/byteness/keyring import — CLIs depend on credstore, never
// on the library directly. Backend selection has already happened
// (selectBackend); this file just constructs and adapts the chosen one.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/byteness/keyring"
)

// osKeyringBackend adapts a keyring.Keyring to the internal backend
// interface. backendKind is the resolved Backend (named so it does not
// collide with the kind() method).
type osKeyringBackend struct {
	kr          keyring.Keyring
	backendKind Backend
}

// openOSBackend opens exactly the selected backend. AllowedBackends is
// pinned to the single chosen type so the library never re-prioritizes
// after our §1.4 selection. Per-backend construction (file dir +
// passphrase, pass per-service prefix) lives in buildKeyringConfig.
func openOSBackend(kind Backend, service string, opts *Options, getenv func(string) string) (backend, error) {
	if err := preflightOSBackend(kind); err != nil {
		return nil, fmt.Errorf("credstore: opening %s backend for service %q: %w", kind, service, err)
	}
	cfg, err := buildKeyringConfig(kind, service, opts, getenv)
	if err != nil {
		return nil, err
	}
	kr, err := keyring.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("credstore: opening %s backend for service %q: %w", kind, service, err)
	}
	return &osKeyringBackend{kr: kr, backendKind: kind}, nil
}

// preflightOSBackend runs cheap, actionable pre-construction checks for
// backends whose ByteNess opener returns a useful error message that
// keyring.Open then swallows. ByteNess's Open iterates AllowedBackends
// and `continue`s past any opener that returns an error, so a useful
// "pass program is not available" from the pass opener is lost and the
// user sees a generic "Specified keyring backend not available". Catch
// the common cases here so users get actionable messages.
//
// When adding a new Backend constant, decide whether it needs a
// preflight check (cheap pre-construction validation that produces a
// better error than ByteNess's generic one), or whether ByteNess's own
// opener error is already actionable enough.
func preflightOSBackend(kind Backend) error {
	switch kind {
	case BackendPass:
		// byteness/keyring/pass.go is `//go:build !windows`. Catch the
		// Windows case before LookPath: a Windows user with a `pass`
		// shim on PATH (Git-for-Windows, WSL bridge) would otherwise
		// pass preflight and then hit a generic ByteNess "backend not
		// available" message from keyring.Open. Surface the platform
		// constraint directly with no install hints (those are Linux/
		// macOS-specific and would be misleading on Windows).
		if runtime.GOOS == "windows" {
			return fmt.Errorf("the pass backend is not supported on Windows; use --backend wincred (default) or --backend file")
		}
		if _, err := exec.LookPath("pass"); err != nil {
			return fmt.Errorf("the pass(1) CLI is not on $PATH; install it (e.g. `apt install pass` / `brew install pass`) and run `pass init <gpg-key-id>` before selecting --backend pass")
		}
	case BackendKeychain, BackendWinCred, BackendSecretService, BackendFile, BackendMemory:
		// No preflight check — ByteNess's openers (or store.Open's
		// memory short-circuit) already return actionable errors.
	}
	return nil
}

// buildKeyringConfig assembles the keyring.Config for the chosen backend.
// Extracted from openOSBackend so per-backend wiring is unit-testable
// without opening a real keyring.
func buildKeyringConfig(kind Backend, service string, opts *Options, getenv func(string) string) (keyring.Config, error) {
	cfg := keyring.Config{
		ServiceName:     service,
		AllowedBackends: []keyring.BackendType{keyring.BackendType(kind)},
	}
	switch kind {
	case BackendFile:
		dir, err := fileKeyringDir(service, getenv)
		if err != nil {
			return keyring.Config{}, err
		}
		cfg.FileDir = dir
		pwFunc, err := filePasswordFunc(service, opts, getenv)
		if err != nil {
			return keyring.Config{}, err
		}
		cfg.FilePasswordFunc = pwFunc
	case BackendPass:
		// ByteNess's pass backend ignores ServiceName — items are
		// stored at filepath.Join(PassDir, PassPrefix, key) + ".gpg"
		// (see byteness/keyring/pass.go). Without a per-service
		// PassPrefix every cli-common consumer would share a flat
		// ~/.password-store namespace and collide on identical key
		// names. Scope the prefix to the service so each CLI gets
		// its own subtree.
		cfg.PassPrefix = service
	case BackendKeychain, BackendWinCred, BackendSecretService:
		// No additional construction — ByteNess uses ServiceName
		// directly for these.
	case BackendMemory:
		// BackendMemory short-circuits in store.Open before reaching
		// openOSBackend; this arm exists only to keep the exhaustive
		// switch honest. If a future change starts routing memory
		// through here, no extra construction is needed.
	}
	return cfg, nil
}

// fileKeyringDir is the encrypted-file backend location and the test
// isolation seam. XDG Base Directory: $XDG_DATA_HOME/<service>/keyring,
// else $HOME/.local/share/<service>/keyring. Tests set XDG_DATA_HOME to
// t.TempDir() so a file-backend write never touches a real home dir; no
// public Options field is needed for that. Fail-closed: if neither
// XDG_DATA_HOME nor a home directory resolves, error rather than write
// an encrypted secret store under the process's working directory.
func fileKeyringDir(service string, getenv func(string) string) (string, error) {
	base := getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("credstore: cannot resolve a file keyring directory; set XDG_DATA_HOME: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, service, "keyring"), nil
}

// filePasswordFunc resolves the file backend passphrase: the per-service
// <SERVICE>_KEYRING_PASSPHRASE env var (the one sanctioned runtime
// secret-material env var, §1.4), else opts.FilePassphrase, else
// fail-closed with an actionable, value-free error.
func filePasswordFunc(service string, opts *Options, getenv func(string) string) (keyring.PromptFunc, error) {
	envVar := envServicePrefix(service) + "_KEYRING_PASSPHRASE"
	if v := getenv(envVar); v != "" {
		return keyring.FixedStringPrompt(v), nil
	}
	if opts != nil && opts.FilePassphrase != nil {
		fn := opts.FilePassphrase
		return func(string) (string, error) {
			s, err := fn()
			if err != nil {
				return "", fmt.Errorf("credstore: file keyring passphrase prompt failed: %w", err)
			}
			return s, nil
		}, nil
	}
	return nil, fmt.Errorf("%w: set %s or supply Options.FilePassphrase", ErrFilePassphraseRequired, envVar)
}

func (b *osKeyringBackend) kind() Backend { return b.backendKind }

func (b *osKeyringBackend) get(itemKey string) (string, error) {
	it, err := b.kr.Get(itemKey)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("credstore: %s get %q: %w", b.backendKind, itemKey, err)
	}
	return string(it.Data), nil
}

// set has no atomic compare-and-swap: the underlying keyring library
// exposes none, so the no-overwrite guard is a best-effort
// Get-then-Set. Under a concurrent cross-process writer the check can
// race (§1.5.1 "practical atomicity") — exact only for the in-memory
// backend.
func (b *osKeyringBackend) set(itemKey, value string, overwrite bool) error {
	if !overwrite {
		switch _, err := b.kr.Get(itemKey); {
		case err == nil:
			return ErrExists
		case errors.Is(err, keyring.ErrKeyNotFound):
			// not present — fall through to write
		default:
			return fmt.Errorf("credstore: %s set %q (overwrite pre-check): %w", b.backendKind, itemKey, err)
		}
	}
	if err := b.kr.Set(keyring.Item{Key: itemKey, Data: []byte(value)}); err != nil {
		return fmt.Errorf("credstore: %s set %q: %w", b.backendKind, itemKey, err)
	}
	return nil
}

func (b *osKeyringBackend) delete(itemKey string) error {
	if err := b.kr.Remove(itemKey); err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("credstore: %s delete %q: %w", b.backendKind, itemKey, err)
	}
	return nil
}

func (b *osKeyringBackend) exists(itemKey string) (bool, error) {
	if _, err := b.kr.Get(itemKey); err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("credstore: %s exists %q: %w", b.backendKind, itemKey, err)
	}
	return true, nil
}

func (b *osKeyringBackend) listKeys() ([]string, error) {
	keys, err := b.kr.Keys()
	if err != nil {
		return nil, fmt.Errorf("credstore: %s listKeys: %w", b.backendKind, err)
	}
	return keys, nil
}

// close is a no-op: the underlying keyring library exposes no Close.
// The Store still best-effort clears the in-memory backend; OS
// keyrings own their own lifecycle and there is nothing to release
// here.
func (b *osKeyringBackend) close() error { return nil }

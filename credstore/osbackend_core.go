package credstore

// Real OS-keyring backends, per the Secret-Handling Standard §1.4 and
// §2.1. Backend selection has already happened (selectBackend); this
// file adapts the selected backend to Store. Build-tagged companions
// provide the actual backend opener: ByteNess keyring for CGO/Windows,
// and direct lower-level backends for affected static Unix builds.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var errKeyringItemNotFound = errors.New("credstore: keyring item not found")

type promptFunc func(string) (string, error)

type keyringItem struct {
	key                         string
	data                        []byte
	label                       string
	description                 string
	keychainNotTrustApplication bool
	keychainNotSynchronizable   bool
}

type keyringBackend interface {
	get(itemKey string) (keyringItem, error)
	set(keyringItem) error
	remove(itemKey string) error
	keys() ([]string, error)
}

type backendConfig struct {
	serviceName              string
	allowedBackend           Backend
	keychainTrustApplication bool
	fileDir                  string
	filePasswordFunc         promptFunc
	passDir                  string
	passCmd                  string
	passPrefix               string
	opTimeout                time.Duration
	opVaultID                string
	opItemTitlePrefix        string
	opItemTag                string
	opItemFieldTitle         string
	opConnectHost            string
	opConnectTokenEnv        string
	opTokenEnv               string
	opDesktopAccountID       string
}

// osKeyringBackend adapts a platform keyring implementation to the
// internal backend interface. backendKind is the resolved Backend
// (named so it does not collide with the kind() method).
type osKeyringBackend struct {
	kr          keyringBackend
	backendKind Backend
	service     string
}

// openOSBackend opens exactly the selected backend. The opener is
// build-tagged so static Unix builds do not import ByteNess keyring and
// therefore do not compile its unused 1Password backends.
func openOSBackend(kind Backend, service string, opts *Options, getenv func(string) string) (backend, error) {
	if err := unsupportedOnePasswordBackendInBuild(kind); err != nil {
		return nil, fmt.Errorf("credstore: opening %s backend for service %q: %w", kind, service, err)
	}
	if err := preflightOSBackend(kind); err != nil {
		return nil, fmt.Errorf("credstore: opening %s backend for service %q: %w", kind, service, err)
	}
	cfg, err := buildKeyringConfig(kind, service, opts, getenv)
	if err != nil {
		return nil, err
	}
	kr, err := openKeyringBackend(kind, cfg)
	if err != nil {
		return nil, fmt.Errorf("credstore: opening %s backend for service %q: %w", kind, service, err)
	}
	return &osKeyringBackend{kr: kr, backendKind: kind, service: service}, nil
}

// preflightOSBackend runs cheap, actionable pre-construction checks for
// backends whose opener may otherwise return a generic availability
// error. When adding a new Backend constant, decide whether it needs a
// preflight check or whether the opener error is actionable enough.
func preflightOSBackend(kind Backend) error {
	switch kind {
	case BackendPass:
		// ByteNess's pass backend is `//go:build !windows`; the direct
		// no-CGO backend keeps the same platform contract.
		if runtime.GOOS == "windows" {
			return fmt.Errorf("the pass backend is not supported on Windows; use --backend wincred (default) or --backend file")
		}
		if _, err := exec.LookPath("pass"); err != nil {
			return fmt.Errorf("the pass(1) CLI is not on $PATH; install it (e.g. `apt install pass` / `brew install pass`) and run `pass init <gpg-key-id>` before selecting --backend pass")
		}
	case BackendKeychain, BackendWinCred, BackendSecretService, BackendFile, BackendOP, BackendOPConnect, BackendOPDesktop, BackendMemory:
		// No preflight check — the openers (or store.Open's memory
		// short-circuit) already return actionable errors.
	}
	return nil
}

// buildKeyringConfig assembles the selected backend's construction
// config. Extracted from openOSBackend so per-backend wiring is
// unit-testable without opening a real keyring.
func buildKeyringConfig(kind Backend, service string, opts *Options, getenv func(string) string) (backendConfig, error) {
	cfg := backendConfig{
		serviceName:    service,
		allowedBackend: kind,
	}
	switch kind {
	case BackendFile:
		dir, err := fileKeyringDir(service, getenv)
		if err != nil {
			return backendConfig{}, err
		}
		cfg.fileDir = dir
		pwFunc, err := filePasswordFunc(service, opts, getenv)
		if err != nil {
			return backendConfig{}, err
		}
		cfg.filePasswordFunc = pwFunc
	case BackendPass:
		// ByteNess's pass backend ignores ServiceName — items are
		// stored at filepath.Join(PassDir, PassPrefix, key) + ".gpg".
		// Without a per-service PassPrefix every cli-common consumer
		// would share a flat ~/.password-store namespace and collide on
		// identical key names. Scope the prefix to the service so each
		// CLI gets its own subtree.
		cfg.passPrefix = service
	case BackendKeychain, BackendWinCred, BackendSecretService:
		// All three use ServiceName directly. On macOS, trust the current
		// signed application by default so newly-written items do not show
		// the generic "allow access?" dialog for the same binary.
		if kind == BackendKeychain {
			cfg.keychainTrustApplication = true
		}
	case BackendOP, BackendOPConnect, BackendOPDesktop:
		cfg.opItemTitlePrefix = service
		cfg.opItemTag = service
		// ByteNess requires a non-zero OPTimeout for OP and OPDesktop.
		// OPConnect does not consult OPTimeout during construction.
		if kind == BackendOP || kind == BackendOPDesktop {
			cfg.opTimeout = DefaultOnePasswordTimeout
		}
		if opts != nil && opts.OnePassword != nil {
			if opts.OnePassword.Timeout != 0 {
				cfg.opTimeout = opts.OnePassword.Timeout
			}
			cfg.opVaultID = opts.OnePassword.VaultID
			if opts.OnePassword.ItemTitlePrefix != "" {
				cfg.opItemTitlePrefix = opts.OnePassword.ItemTitlePrefix
			}
			if opts.OnePassword.ItemTag != "" {
				cfg.opItemTag = opts.OnePassword.ItemTag
			}
			cfg.opItemFieldTitle = opts.OnePassword.ItemFieldTitle
			cfg.opConnectHost = opts.OnePassword.ConnectHost
			cfg.opConnectTokenEnv = opts.OnePassword.ConnectTokenEnv
			cfg.opTokenEnv = opts.OnePassword.ServiceTokenEnv
			cfg.opDesktopAccountID = opts.OnePassword.DesktopAccountID
		}
	case BackendMemory:
		// BackendMemory short-circuits in store.Open before reaching
		// openOSBackend; this arm exists only to keep the exhaustive
		// switch honest.
	}
	return cfg, nil
}

func keyringItemForWrite(service, itemKey string, value []byte) keyringItem {
	return keyringItem{
		key:         itemKey,
		data:        value,
		label:       keyringItemLabel(service, itemKey),
		description: keyringItemDescription(service, itemKey),
	}
}

func keyringItemLabel(service, itemKey string) string {
	return service + " " + itemKey
}

func keyringItemDescription(service, itemKey string) string {
	return "Credential for " + service + " " + itemKey
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
func filePasswordFunc(service string, opts *Options, getenv func(string) string) (promptFunc, error) {
	envVar := envServicePrefix(service) + "_KEYRING_PASSPHRASE"
	if v := getenv(envVar); v != "" {
		return fixedStringPrompt(v), nil
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

func fixedStringPrompt(value string) promptFunc {
	return func(string) (string, error) {
		return value, nil
	}
}

func (b *osKeyringBackend) kind() Backend { return b.backendKind }

func (b *osKeyringBackend) get(itemKey string) (string, error) {
	it, err := b.kr.get(itemKey)
	if err != nil {
		if errors.Is(err, errKeyringItemNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("credstore: %s get %q: %w", b.backendKind, itemKey, err)
	}
	return string(it.data), nil
}

// set has no atomic compare-and-swap: the underlying keyring backends
// expose none, so the no-overwrite guard is a best-effort Get-then-Set.
// Under a concurrent cross-process writer the check can race (§1.5.1
// "practical atomicity") — exact only for the in-memory backend.
func (b *osKeyringBackend) set(itemKey, value string, overwrite bool) error {
	if !overwrite {
		switch _, err := b.kr.get(itemKey); {
		case err == nil:
			return ErrExists
		case errors.Is(err, errKeyringItemNotFound):
			// not present — fall through to write
		default:
			return fmt.Errorf("credstore: %s set %q (overwrite pre-check): %w", b.backendKind, itemKey, err)
		}
	}
	if err := b.kr.set(keyringItemForWrite(b.service, itemKey, []byte(value))); err != nil {
		return fmt.Errorf("credstore: %s set %q: %w", b.backendKind, itemKey, err)
	}
	return nil
}

func (b *osKeyringBackend) delete(itemKey string) error {
	if err := b.kr.remove(itemKey); err != nil {
		if errors.Is(err, errKeyringItemNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("credstore: %s delete %q: %w", b.backendKind, itemKey, err)
	}
	return nil
}

func (b *osKeyringBackend) exists(itemKey string) (bool, error) {
	if _, err := b.kr.get(itemKey); err != nil {
		if errors.Is(err, errKeyringItemNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("credstore: %s exists %q: %w", b.backendKind, itemKey, err)
	}
	return true, nil
}

func (b *osKeyringBackend) listKeys() ([]string, error) {
	keys, err := b.kr.keys()
	if err != nil {
		return nil, fmt.Errorf("credstore: %s listKeys: %w", b.backendKind, err)
	}
	return keys, nil
}

// close is a no-op: the underlying keyring implementations expose no
// Close. The Store still best-effort clears the in-memory backend; OS
// keyrings own their own lifecycle and there is nothing to release here.
func (b *osKeyringBackend) close() error { return nil }

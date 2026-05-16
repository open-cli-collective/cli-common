package credstore

// This file implements the service-scoped Store, its lifecycle
// (Open/Close/Backend), and single-key operations, per the Open CLI
// Collective Secret-Handling Standard §2.1 (API surface), §1.3 (the
// <service>/<profile>/<key> -> ServiceName + <profile>/<key> mapping),
// and §1.5/§1.5.2 (overwrite semantics, allowed-key enforcement).
//
// Only the in-memory backend (memory.go) is wired in this unit. Real OS
// backends and automatic/env/config selection are a later INT-310 unit;
// requesting them fails closed (ErrBackendNotImplemented) rather than
// silently degrading to an in-memory store.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Backend identifies which credential backend a Store is using.
type Backend string

const (
	BackendKeychain      Backend = "keychain"       // macOS — later unit
	BackendWinCred       Backend = "wincred"        // Windows — later unit
	BackendSecretService Backend = "secret-service" // Linux — later unit
	BackendFile          Backend = "file"           // encrypted file — later unit
	BackendMemory        Backend = "memory"         // tests/CI, no disk
)

// Source describes how the backend was selected.
type Source string

const (
	SourceAuto     Source = "auto"     // OS default — wired later unit
	SourceEnv      Source = "env"      // <SERVICE>_KEYRING_BACKEND — later
	SourceConfig   Source = "config"   // keyring.backend in config — later
	SourceExplicit Source = "explicit" // Options.Backend set by caller
)

// Options configures Open.
type Options struct {
	// AllowedKeys is the CLI's allowed-key allowlist (§2.1/§1.5.2). When
	// non-empty, Set and Delete reject any key not in this set. When empty,
	// only key syntax is validated (useful for tooling/tests).
	AllowedKeys []string
	// Backend forces a specific backend. In this unit only BackendMemory
	// is selectable; any other value (including the zero value) fails
	// closed with ErrBackendNotImplemented.
	Backend Backend
}

// Sentinels and typed errors. All are matchable via errors.Is.
var (
	ErrNotFound              = errors.New("credstore: credential not found")
	ErrExists                = errors.New("credstore: credential already exists (use WithOverwrite to replace)")
	ErrStoreClosed           = errors.New("credstore: store is closed")
	ErrBackendNotImplemented = errors.New("credstore: backend not implemented")
)

// KeyError is returned by Set/Delete when a key is not in the Store's
// allowed-key set (§1.5.2). The key name is not secret. Allowed is the
// sorted allowed set so messages are deterministic.
type KeyError struct {
	Key     string
	Allowed []string
}

func (e *KeyError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("credstore: key %q is not allowed", e.Key)
	}
	return fmt.Sprintf("credstore: key %q is not in the allowed set [%s]", e.Key, strings.Join(e.Allowed, ", "))
}

// Is matches any *KeyError so callers can write
// errors.Is(err, credstore.ErrKeyNotAllowed).
func (e *KeyError) Is(target error) bool {
	_, ok := target.(*KeyError)
	return ok
}

// ErrKeyNotAllowed is the sentinel for errors.Is against *KeyError. Its
// fields must not be mutated by callers — it is a type sentinel, not a
// value carrier; the actual key/allowed set live on the returned error.
var ErrKeyNotAllowed = &KeyError{}

// backend is the internal storage abstraction. These core methods should
// not need reshaping for the later real OS backends; the bundle unit may
// extend this interface with list/prefix methods.
type backend interface {
	get(itemKey string) (string, error)
	// set writes value at itemKey. When overwrite is false and an entry
	// already exists, it returns ErrExists without modifying anything; the
	// check and write are atomic under the backend's own lock.
	set(itemKey, value string, overwrite bool) error
	delete(itemKey string) error
	exists(itemKey string) (bool, error)
	// listKeys returns every stored Item.Key ("<profile>/<key>"). Added
	// for the bundle operations (§2.1); the bundle unit is the interface
	// extension INT-430's review anticipated.
	listKeys() ([]string, error)
	kind() Backend
	close() error
}

// Store is a service-scoped credential store. It is safe for concurrent
// use.
type Store struct {
	service     string
	src         Source
	kind        Backend
	allowed     map[string]struct{} // nil => syntax-only (no allowlist)
	allowedList []string            // sorted; reported in KeyError

	mu     sync.Mutex
	closed bool
	be     backend
}

// Open returns a service-scoped Store. The service segment must satisfy
// the §1.3 ref grammar. Backend selection fails closed: only
// BackendMemory is implemented in this unit.
func Open(service string, opts *Options) (*Store, error) {
	if service == "" {
		return nil, &RefError{Kind: RefErrorEmpty, Segment: "service"}
	}
	if !validSegment(service) {
		return nil, &RefError{Kind: RefErrorInvalidChar, Segment: "service", Ref: service}
	}
	if opts == nil {
		opts = &Options{}
	}

	switch opts.Backend {
	case BackendMemory:
		// ok
	case BackendKeychain, BackendWinCred, BackendSecretService, BackendFile:
		return nil, fmt.Errorf("%w: backend %q is not implemented yet; pass Options.Backend = credstore.BackendMemory (OS backends are a later INT-310 unit)", ErrBackendNotImplemented, opts.Backend)
	case "":
		return nil, fmt.Errorf("%w: no backend specified; pass Options.Backend = credstore.BackendMemory (OS backend auto-selection is a later INT-310 unit)", ErrBackendNotImplemented)
	default:
		return nil, fmt.Errorf("%w: unknown backend %q", ErrBackendNotImplemented, opts.Backend)
	}

	allowed, allowedList, err := normalizeAllowedKeys(opts.AllowedKeys)
	if err != nil {
		return nil, err
	}

	return &Store{
		service:     service,
		src:         SourceExplicit,
		kind:        BackendMemory,
		allowed:     allowed,
		allowedList: allowedList,
		be:          newMemoryBackend(),
	}, nil
}

// normalizeAllowedKeys syntax-checks, copies, dedupes, and sorts the
// caller's allowlist. An empty list means syntax-only validation.
func normalizeAllowedKeys(keys []string) (map[string]struct{}, []string, error) {
	if len(keys) == 0 {
		return nil, nil, nil
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			return nil, nil, &RefError{Kind: RefErrorEmpty, Segment: "key"}
		}
		if !validSegment(k) {
			return nil, nil, &RefError{Kind: RefErrorInvalidChar, Segment: "key", Ref: k}
		}
		set[k] = struct{}{}
	}
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	return set, list, nil
}

// joinItemKey validates profile and key and returns the §1.3 Item.Key
// (<profile>/<key>). service is the ServiceName half, held on the Store.
func joinItemKey(profile, key string) (string, error) {
	if profile == "" {
		return "", &RefError{Kind: RefErrorEmpty, Segment: "profile"}
	}
	if !validSegment(profile) {
		return "", &RefError{Kind: RefErrorInvalidChar, Segment: "profile", Ref: profile}
	}
	if key == "" {
		return "", &RefError{Kind: RefErrorEmpty, Segment: "key"}
	}
	if !validSegment(key) {
		return "", &RefError{Kind: RefErrorInvalidChar, Segment: "key", Ref: key}
	}
	return profile + "/" + key, nil
}

func (s *Store) checkAllowed(key string) error {
	if s.allowed == nil { // syntax-only
		return nil
	}
	if _, ok := s.allowed[key]; ok {
		return nil
	}
	// Copy: KeyError.Allowed is exported; a caller mutating it must not be
	// able to corrupt the Store's future error messages.
	allowed := append([]string(nil), s.allowedList...)
	return &KeyError{Key: key, Allowed: allowed}
}

type setOptions struct{ overwrite bool }

// SetOpt configures Set.
type SetOpt func(*setOptions)

// WithOverwrite allows Set to replace an existing entry (§1.5). Without
// it, Set on an existing entry returns ErrExists.
func WithOverwrite() SetOpt { return func(o *setOptions) { o.overwrite = true } }

// Backend reports the selected backend and how it was selected. It is
// metadata only — no error, and it remains valid after Close.
func (s *Store) Backend() (Backend, Source) { return s.kind, s.src }

// Close releases the backend and best-effort clears stored values. It is
// idempotent and safe to call repeatedly. Note: Go string secrets cannot
// be guaranteed zeroized in memory; this drops references and clears the
// backing store, which is the best a Go library can do.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	// Close the backend first; only mark the store closed on success. A
	// future OS backend whose close() fails must not leave the store in a
	// terminal ErrStoreClosed state with leaked resources and no retry
	// path. The in-memory backend always succeeds, so this is purely
	// forward-compat hardening.
	if err := s.be.close(); err != nil {
		return err
	}
	s.closed = true
	// Drop the backend so any future guard-bypass fails fast (nil deref)
	// instead of silently using a closed backend. The four op methods
	// already return ErrStoreClosed before touching s.be, and Backend()
	// reads s.kind/s.src, so this is safe today and defensive for later.
	s.be = nil
	return nil
}

// Get returns the value at (profile, key). Missing entry → ErrNotFound.
// Read paths are syntax-validated but intentionally not allowlist-gated
// (§1.5.2 gates Set/Delete only): a key written before an allowlist
// change must stay readable.
func (s *Store) Get(profile, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", ErrStoreClosed
	}
	itemKey, err := joinItemKey(profile, key)
	if err != nil {
		return "", err
	}
	return s.be.get(itemKey)
}

// Set writes value at (profile, key). Enforces the allowlist (§1.5.2).
// Without WithOverwrite, an existing entry → ErrExists.
func (s *Store) Set(profile, key, value string, opts ...SetOpt) error {
	var so setOptions
	for _, o := range opts {
		o(&so)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	itemKey, err := joinItemKey(profile, key)
	if err != nil {
		return err
	}
	if err := s.checkAllowed(key); err != nil {
		return err
	}
	return s.be.set(itemKey, value, so.overwrite)
}

// Delete removes the entry at (profile, key). Enforces the allowlist
// (§1.5.2). Missing entry → ErrNotFound.
func (s *Store) Delete(profile, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	itemKey, err := joinItemKey(profile, key)
	if err != nil {
		return err
	}
	if err := s.checkAllowed(key); err != nil {
		return err
	}
	return s.be.delete(itemKey)
}

// Exists reports whether (profile, key) is present. Missing → (false,
// nil). Syntax and closed-state errors are still returned. Not
// allowlist-gated (read path, §1.5.2).
func (s *Store) Exists(profile, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, ErrStoreClosed
	}
	itemKey, err := joinItemKey(profile, key)
	if err != nil {
		return false, err
	}
	return s.be.exists(itemKey)
}

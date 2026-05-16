package credstore

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func openMem(t *testing.T, allowed ...string) *Store {
	t.Helper()
	s, err := Open("test-service", &Options{Backend: BackendMemory, AllowedKeys: allowed})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenBackendSelection(t *testing.T) {
	tests := []struct {
		name    string
		service string
		opts    *Options
		wantErr error // sentinel; nil = success
	}{
		{"memory ok", "test-service", &Options{Backend: BackendMemory}, nil},
		{"nil opts fails closed", "svc", nil, ErrBackendNotImplemented},
		{"unset backend fails closed", "svc", &Options{}, ErrBackendNotImplemented},
		{"keychain not implemented", "svc", &Options{Backend: BackendKeychain}, ErrBackendNotImplemented},
		{"wincred not implemented", "svc", &Options{Backend: BackendWinCred}, ErrBackendNotImplemented},
		{"secret-service not implemented", "svc", &Options{Backend: BackendSecretService}, ErrBackendNotImplemented},
		{"file not implemented", "svc", &Options{Backend: BackendFile}, ErrBackendNotImplemented},
		{"unknown backend", "svc", &Options{Backend: Backend("bogus")}, ErrBackendNotImplemented},
		{"empty service", "", &Options{Backend: BackendMemory}, ErrRefEmpty},
		{"invalid service", "bad.svc", &Options{Backend: BackendMemory}, ErrRefInvalidChar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Open(tt.service, tt.opts)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Open err = %v, want errors.Is %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open unexpected err = %v", err)
			}
			_ = s.Close()
		})
	}
}

func TestOpenInvalidAllowedKey(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		kind RefErrorKind
	}{
		{"invalid char", []string{"ok", "bad key"}, RefErrorInvalidChar},
		{"empty string", []string{""}, RefErrorEmpty},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Open("svc", &Options{Backend: BackendMemory, AllowedKeys: c.keys})
			var re *RefError
			if !errors.As(err, &re) || re.Segment != "key" || re.Kind != c.kind {
				t.Fatalf("Open: err = %v, want *RefError Segment=key Kind=%v", err, c.kind)
			}
		})
	}
}

func TestSingleKeyRoundTrip(t *testing.T) {
	s := openMem(t)
	if ok, err := s.Exists("default", "api_token"); err != nil || ok {
		t.Fatalf("Exists before set = (%v,%v), want (false,nil)", ok, err)
	}
	if err := s.Set("default", "api_token", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := s.Get("default", "api_token"); err != nil || v != "v1" {
		t.Fatalf("Get = (%q,%v), want (\"v1\",nil)", v, err)
	}
	if ok, err := s.Exists("default", "api_token"); err != nil || !ok {
		t.Fatalf("Exists after set = (%v,%v), want (true,nil)", ok, err)
	}
	if err := s.Delete("default", "api_token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("default", "api_token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
	if ok, err := s.Exists("default", "api_token"); err != nil || ok {
		t.Fatalf("Exists after delete = (%v,%v), want (false,nil)", ok, err)
	}
}

func TestSetOverwrite(t *testing.T) {
	s := openMem(t)
	if err := s.Set("default", "k", "v1"); err != nil {
		t.Fatalf("Set v1: %v", err)
	}
	if err := s.Set("default", "k", "v2"); !errors.Is(err, ErrExists) {
		t.Fatalf("Set existing w/o overwrite err = %v, want ErrExists", err)
	}
	if v, _ := s.Get("default", "k"); v != "v1" {
		t.Fatalf("value after failed Set = %q, want unchanged v1", v)
	}
	if err := s.Set("default", "k", "v2", WithOverwrite()); err != nil {
		t.Fatalf("Set with overwrite: %v", err)
	}
	if v, _ := s.Get("default", "k"); v != "v2" {
		t.Fatalf("value after overwrite = %q, want v2", v)
	}
}

func TestDeleteAndExistsMissing(t *testing.T) {
	s := openMem(t)
	if err := s.Delete("default", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing err = %v, want ErrNotFound", err)
	}
	if ok, err := s.Exists("default", "nope"); err != nil || ok {
		t.Fatalf("Exists missing = (%v,%v), want (false,nil)", ok, err)
	}
}

func TestAllowlistEnforcement(t *testing.T) {
	s := openMem(t, "bot_token", "api_token") // intentionally unsorted

	// Disallowed but syntactically valid key: Set/Delete blocked.
	err := s.Set("default", "user_token", "v")
	if !errors.Is(err, ErrKeyNotAllowed) {
		t.Fatalf("Set disallowed err = %v, want ErrKeyNotAllowed", err)
	}
	var ke *KeyError
	if !errors.As(err, &ke) {
		t.Fatalf("err not *KeyError: %v", err)
	}
	if msg := ke.Error(); !strings.Contains(msg, "[api_token, bot_token]") {
		t.Fatalf("KeyError msg = %q, want sorted allowed set", msg)
	}
	if err := s.Delete("default", "user_token"); !errors.Is(err, ErrKeyNotAllowed) {
		t.Fatalf("Delete disallowed err = %v, want ErrKeyNotAllowed", err)
	}

	// Read paths are NOT allowlist-gated (§1.5.2): a disallowed-but-valid
	// key reads as not-found / false, never KeyError.
	if _, err := s.Get("default", "user_token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get disallowed key err = %v, want ErrNotFound (not gated)", err)
	}
	if ok, err := s.Exists("default", "user_token"); err != nil || ok {
		t.Fatalf("Exists disallowed key = (%v,%v), want (false,nil)", ok, err)
	}

	// Allowed key works.
	if err := s.Set("default", "api_token", "v"); err != nil {
		t.Fatalf("Set allowed key: %v", err)
	}
}

func TestEmptyAllowlistIsSyntaxOnly(t *testing.T) {
	s := openMem(t) // no AllowedKeys
	if err := s.Set("default", "any_valid_key", "v"); err != nil {
		t.Fatalf("Set with empty allowlist should accept any valid key, got %v", err)
	}
}

func TestSyntaxErrors(t *testing.T) {
	s := openMem(t)
	cases := []struct {
		name, profile, key, seg string
	}{
		{"space in key", "default", "bad key", "key"},
		{"dot in key", "default", "k.v", "key"},
		{"slash in key", "default", "a/b", "key"},
		{"empty key", "default", "", "key"},
		{"space in profile", "pro file", "k", "profile"},
		{"empty profile", "", "k", "profile"},
	}
	// Each of Set/Get/Delete/Exists validates independently via
	// joinItemKey, so all four entry points must reject bad refs.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ops := []struct {
				name string
				fn   func() error
			}{
				{"Set", func() error { return s.Set(c.profile, c.key, "v") }},
				{"Get", func() error { _, e := s.Get(c.profile, c.key); return e }},
				{"Delete", func() error { return s.Delete(c.profile, c.key) }},
				{"Exists", func() error { _, e := s.Exists(c.profile, c.key); return e }},
			}
			for _, o := range ops {
				op, fn := o.name, o.fn
				err := fn()
				var re *RefError
				if !errors.As(err, &re) {
					t.Fatalf("%s(%q,%q) err = %v, want *RefError", op, c.profile, c.key, err)
				}
				if re.Segment != c.seg {
					t.Fatalf("%s Segment = %q, want %q", op, re.Segment, c.seg)
				}
			}
		})
	}
}

func TestCloseContract(t *testing.T) {
	s, err := Open("svc", &Options{Backend: BackendMemory})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s.Set("default", "k", "v")

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close (idempotent) = %v, want nil", err)
	}

	if _, err := s.Get("default", "k"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Get after Close = %v, want ErrStoreClosed", err)
	}
	if err := s.Set("default", "k", "v"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Set after Close = %v, want ErrStoreClosed", err)
	}
	if err := s.Delete("default", "k"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Delete after Close = %v, want ErrStoreClosed", err)
	}
	if _, err := s.Exists("default", "k"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Exists after Close = %v, want ErrStoreClosed", err)
	}

	// Backend() is metadata: still valid after Close, no error.
	if b, src := s.Backend(); b != BackendMemory || src != SourceExplicit {
		t.Fatalf("Backend() after Close = (%v,%v), want (memory,explicit)", b, src)
	}
}

func TestMemoryBackendClosedPaths(t *testing.T) {
	// Exercise the backend's defensive nil-map branches directly: the
	// Store.closed guard normally prevents these from being reached, but
	// later units may call backends without that guard.
	b := newMemoryBackend()
	if b.kind() != BackendMemory {
		t.Fatalf("kind = %v, want memory", b.kind())
	}
	if err := b.set("p/k", "v", false); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := b.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := b.close(); err != nil {
		t.Fatalf("close (idempotent): %v", err)
	}
	if _, err := b.get("p/k"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("get after close = %v, want ErrStoreClosed", err)
	}
	if err := b.set("p/k", "v", true); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("set after close = %v, want ErrStoreClosed", err)
	}
	if err := b.delete("p/k"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("delete after close = %v, want ErrStoreClosed", err)
	}
	if _, err := b.exists("p/k"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("exists after close = %v, want ErrStoreClosed", err)
	}
}

func TestMemoryBackendConcurrentSet(t *testing.T) {
	// Drive memoryBackend.set concurrently *without* Store.mu so the
	// backend's own b.mu is genuinely contended — the path later units
	// will exercise when backends gain direct callers.
	b := newMemoryBackend()
	const n = 64
	var successes, exists int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			switch err := b.set("p/k", "v", false); {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case errors.Is(err, ErrExists):
				atomic.AddInt64(&exists, 1)
			default:
				t.Errorf("unexpected backend set err: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 || exists != n-1 {
		t.Fatalf("contended backend set: successes=%d exists=%d, want 1 and %d", successes, exists, n-1)
	}
}

func TestConcurrentSetGet(t *testing.T) {
	s := openMem(t)
	_ = s.Set("default", "k", "seed")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := s.Set("default", "k", "v", WithOverwrite()); err != nil {
				t.Errorf("concurrent Set: unexpected err %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.Get("default", "k"); err != nil {
				t.Errorf("concurrent Get: unexpected err %v", err)
			}
		}()
	}
	wg.Wait()
}

// Validates the observable no-overwrite contract under contention at the
// Store level (exactly one success). Note: Store.mu serializes ops before
// the backend, so this does not exercise the backend's own lock — that
// defensive layer is covered structurally, not by this test.
func TestAtomicPreWriteUnderContention(t *testing.T) {
	s := openMem(t)
	const n = 64
	var successes, exists int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			switch err := s.Set("default", "k", "v"); {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case errors.Is(err, ErrExists):
				atomic.AddInt64(&exists, 1)
			default:
				t.Errorf("unexpected Set err: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 || exists != n-1 {
		t.Fatalf("contended Set: successes=%d exists=%d, want 1 and %d", successes, exists, n-1)
	}
}

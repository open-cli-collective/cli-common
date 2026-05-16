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
	_, err := Open("svc", &Options{Backend: BackendMemory, AllowedKeys: []string{"ok", "bad key"}})
	var re *RefError
	if !errors.As(err, &re) || re.Segment != "key" {
		t.Fatalf("Open with invalid allowed key: err = %v, want *RefError Segment=key", err)
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.Set(c.profile, c.key, "v")
			var re *RefError
			if !errors.As(err, &re) {
				t.Fatalf("Set(%q,%q) err = %v, want *RefError", c.profile, c.key, err)
			}
			if re.Segment != c.seg {
				t.Fatalf("Segment = %q, want %q", re.Segment, c.seg)
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

func TestConcurrentSetGet(t *testing.T) {
	s := openMem(t)
	_ = s.Set("default", "k", "seed")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = s.Set("default", "k", "v", WithOverwrite()) }()
		go func() { defer wg.Done(); _, _ = s.Get("default", "k") }()
	}
	wg.Wait()
}

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

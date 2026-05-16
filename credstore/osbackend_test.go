package credstore

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/99designs/keyring"
)

// TestFileBackendRoundTrip exercises the real encrypted-file backend
// end-to-end. It runs in CI on every OS: the file backend is pure Go,
// XDG_DATA_HOME routes every write into t.TempDir() (the fileKeyringDir
// seam — nothing touches a real home dir), and the passphrase comes
// from the per-service env var. It proves the osKeyringBackend adapter
// satisfies the same Store/bundle contract the memory backend does.
func TestFileBackendRoundTrip(t *testing.T) {
	const svc = "credstore-filetest"
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CREDSTORE_FILETEST_KEYRING_PASSPHRASE", "test-passphrase")

	s, err := Open(svc, &Options{Backend: BackendFile, AllowedKeys: []string{"tok"}})
	if err != nil {
		t.Fatalf("Open file backend: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if b, src := s.Backend(); b != BackendFile || src != SourceExplicit {
		t.Fatalf("Backend() = (%q,%q), want (file,explicit)", b, src)
	}

	// Absent.
	if _, err := s.Get("default", "tok"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
	if ok, err := s.Exists("default", "tok"); err != nil || ok {
		t.Fatalf("Exists missing = (%v,%v), want (false,nil)", ok, err)
	}

	// Set + Get.
	if err := s.Set("default", "tok", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := s.Get("default", "tok"); err != nil || v != "v1" {
		t.Fatalf("Get = (%q,%v), want (v1,nil)", v, err)
	}

	// No-overwrite conflict, then overwrite.
	if err := s.Set("default", "tok", "v2"); !errors.Is(err, ErrExists) {
		t.Fatalf("Set no-overwrite over existing = %v, want ErrExists", err)
	}
	if err := s.Set("default", "tok", "v2", WithOverwrite()); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	if v, _ := s.Get("default", "tok"); v != "v2" {
		t.Fatalf("after overwrite Get = %q, want v2", v)
	}

	// Bundle ops over the real file backend.
	res, err := s.SetBundle("default", map[string]string{"tok": "v3"}, WithOverwrite())
	if err != nil {
		t.Fatalf("SetBundle: %v", err)
	}
	eqStrings(t, "SetBundle Written", res.Written, []string{"tok"})
	keys, err := s.ListBundle("default")
	if err != nil {
		t.Fatalf("ListBundle: %v", err)
	}
	eqStrings(t, "ListBundle", keys, []string{"tok"})
	del, err := s.DeleteBundle("default")
	if err != nil {
		t.Fatalf("DeleteBundle: %v", err)
	}
	eqStrings(t, "DeleteBundle", del, []string{"tok"})
	if ok, _ := s.Exists("default", "tok"); ok {
		t.Fatal("key still present after DeleteBundle")
	}

	// Close is idempotent.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestFileBackendNoPassphraseFailsClosed: the file backend with no
// passphrase source fails closed at Open with an actionable, value-free
// error naming the env var (§1.4).
func TestFileBackendNoPassphraseFailsClosed(t *testing.T) {
	const svc = "credstore-nopass"
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CREDSTORE_NOPASS_KEYRING_PASSPHRASE", "") // ensure unset

	_, err := Open(svc, &Options{Backend: BackendFile})
	if !errors.Is(err, ErrFilePassphraseRequired) {
		t.Fatalf("err = %v, want ErrFilePassphraseRequired", err)
	}
	if !strings.Contains(err.Error(), "CREDSTORE_NOPASS_KEYRING_PASSPHRASE") {
		t.Fatalf("error must name the passphrase env var, got: %v", err)
	}
}

// TestFileBackendPassphraseFromOptions covers the new public
// Options.FilePassphrase field (success path): with the env var unset,
// the supplied callback provides a working passphrase end-to-end.
func TestFileBackendPassphraseFromOptions(t *testing.T) {
	const svc = "credstore-optspass"
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CREDSTORE_OPTSPASS_KEYRING_PASSPHRASE", "") // env unset → must use the callback

	calls := 0
	s, err := Open(svc, &Options{
		Backend:        BackendFile,
		AllowedKeys:    []string{"tok"},
		FilePassphrase: func() (string, error) { calls++; return "from-options", nil },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Set("default", "tok", "secret-A", WithOverwrite()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := s.Get("default", "tok"); err != nil || v != "secret-A" {
		t.Fatalf("Get = (%q,%v), want (secret-A,nil)", v, err)
	}
	if calls == 0 {
		t.Fatal("Options.FilePassphrase was never consulted")
	}
}

// TestFileBackendPassphraseFuncError: a failing FilePassphrase callback
// surfaces a wrapped error and leaks no secret material.
func TestFileBackendPassphraseFuncError(t *testing.T) {
	const svc = "credstore-passerr"
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CREDSTORE_PASSERR_KEYRING_PASSPHRASE", "")

	s, err := Open(svc, &Options{
		Backend:        BackendFile,
		AllowedKeys:    []string{"tok"},
		FilePassphrase: func() (string, error) { return "", errors.New("prompt aborted") },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Assumes 99designs/keyring calls FilePasswordFunc lazily (on first
	// write, not during Open) — verified against keyring v1.2.2. If a
	// future version prompts eagerly, Open above would fail instead and
	// this Set assertion would need to move to the Open call.
	err = s.Set("default", "tok", "SUPER-SECRET-VALUE", WithOverwrite())
	if err == nil {
		t.Fatal("Set must fail when the passphrase callback errors")
	}
	if !strings.Contains(err.Error(), "passphrase prompt failed") {
		t.Fatalf("error must surface the prompt failure, got: %v", err)
	}
	if strings.Contains(err.Error(), "SUPER-SECRET-VALUE") {
		t.Fatalf("error must not contain the secret value: %v", err)
	}
}

// fakeKeyring is an in-test keyring.Keyring with programmable errors,
// used to exercise osKeyringBackend's error mapping/wrapping arms
// deterministically without a real OS keyring.
type fakeKeyring struct {
	items                           map[string]keyring.Item
	getErr, setErr, delErr, keysErr error
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{items: map[string]keyring.Item{}} }

func (f *fakeKeyring) Get(k string) (keyring.Item, error) {
	if f.getErr != nil {
		return keyring.Item{}, f.getErr
	}
	it, ok := f.items[k]
	if !ok {
		return keyring.Item{}, keyring.ErrKeyNotFound
	}
	return it, nil
}
func (f *fakeKeyring) GetMetadata(string) (keyring.Metadata, error) {
	return keyring.Metadata{}, nil
}
func (f *fakeKeyring) Set(it keyring.Item) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.items[it.Key] = it
	return nil
}
func (f *fakeKeyring) Remove(k string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.items[k]; !ok {
		return keyring.ErrKeyNotFound
	}
	delete(f.items, k)
	return nil
}
func (f *fakeKeyring) Keys() ([]string, error) {
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	ks := make([]string, 0, len(f.items))
	for k := range f.items {
		ks = append(ks, k)
	}
	return ks, nil
}

func TestOSKeyringBackendErrorMapping(t *testing.T) {
	// Not-found mapping for every read/delete path (delete's mapping was
	// otherwise unasserted).
	f := newFakeKeyring()
	b := &osKeyringBackend{kr: f, backendKind: BackendFile}

	if _, err := b.get("p/k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing = %v, want ErrNotFound", err)
	}
	if ok, err := b.exists("p/k"); err != nil || ok {
		t.Fatalf("exists missing = (%v,%v), want (false,nil)", ok, err)
	}
	if err := b.delete("p/k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}

	// No-overwrite pre-check returns ErrExists when present.
	if err := b.set("p/k", "v", false); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := b.set("p/k", "v2", false); !errors.Is(err, ErrExists) {
		t.Fatalf("set no-overwrite over existing = %v, want ErrExists", err)
	}

	// Non-not-found backend errors are wrapped (not swallowed, not
	// misclassified as ErrNotFound), each naming its operation.
	sentinel := errors.New("backend boom")
	for _, tc := range []struct {
		name string
		arm  func(*osKeyringBackend) error
		set  func(*fakeKeyring)
		op   string
	}{
		{"get", func(b *osKeyringBackend) error { _, e := b.get("p/k"); return e }, func(f *fakeKeyring) { f.getErr = sentinel }, "get"},
		{"exists", func(b *osKeyringBackend) error { _, e := b.exists("p/k"); return e }, func(f *fakeKeyring) { f.getErr = sentinel }, "exists"},
		{"set", func(b *osKeyringBackend) error { return b.set("p/k", "v", true) }, func(f *fakeKeyring) { f.setErr = sentinel }, "set"},
		// !overwrite + a non-not-found Get error hits the pre-check
		// default arm (distinct from the overwrite=true write path above).
		{"set pre-check", func(b *osKeyringBackend) error { return b.set("p/k", "v", false) }, func(f *fakeKeyring) { f.getErr = sentinel }, "set"},
		{"delete", func(b *osKeyringBackend) error { return b.delete("p/k") }, func(f *fakeKeyring) { f.delErr = sentinel }, "delete"},
		{"listKeys", func(b *osKeyringBackend) error { _, e := b.listKeys(); return e }, func(f *fakeKeyring) { f.keysErr = sentinel }, "listKeys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ff := newFakeKeyring()
			tc.set(ff)
			bb := &osKeyringBackend{kr: ff, backendKind: BackendFile}
			err := tc.arm(bb)
			if !errors.Is(err, sentinel) {
				t.Fatalf("%s err = %v, want wrapped sentinel", tc.op, err)
			}
			if errors.Is(err, ErrNotFound) {
				t.Fatalf("%s must not misclassify a real error as ErrNotFound: %v", tc.op, err)
			}
			if !strings.Contains(err.Error(), tc.op) {
				t.Fatalf("%s err must name the operation: %v", tc.op, err)
			}
		})
	}
}

// TestOpenEnvSelectsFileBackend exercises the real
// selectBackend→Open integration through os.Getenv (the file backend
// runs in CI), proving env selection actually changes the constructed
// backend and is reported as SourceEnv.
func TestOpenEnvSelectsFileBackend(t *testing.T) {
	const svc = "credstore-envsel"
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CREDSTORE_ENVSEL_KEYRING_PASSPHRASE", "pp")
	t.Setenv("CREDSTORE_ENVSEL_KEYRING_BACKEND", "file")

	s, err := Open(svc, &Options{AllowedKeys: []string{"tok"}}) // no explicit backend
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if b, src := s.Backend(); b != BackendFile || src != SourceEnv {
		t.Fatalf("Backend() = (%q,%q), want (file,env)", b, src)
	}
	if err := s.Set("default", "tok", "v", WithOverwrite()); err != nil {
		t.Fatalf("Set via env-selected file backend: %v", err)
	}
	// Roundtrip: prove the env-selected backend is functional, not just
	// labeled correctly.
	if got, err := s.Get("default", "tok"); err != nil || got != "v" {
		t.Fatalf("Get via env-selected file backend = (%q,%v), want (v,nil)", got, err)
	}
}

// TestOSKeyringBackendsGated covers the macOS/wincred/secret-service
// backends, which need an unlocked desktop keyring and so cannot run in
// headless CI. It is a deterministic skip unless CREDSTORE_OS_KEYRING_TEST=1.
func TestOSKeyringBackendsGated(t *testing.T) {
	if os.Getenv("CREDSTORE_OS_KEYRING_TEST") != "1" {
		t.Skip("set CREDSTORE_OS_KEYRING_TEST=1 to run real OS keyring tests (needs an unlocked keychain/credential-manager/secret-service)")
	}
	const svc = "credstore-osgated"
	s, err := Open(svc, &Options{AllowedKeys: []string{"tok"}}) // auto → GOOS-native
	if err != nil {
		t.Fatalf("Open auto OS backend: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	b, src := s.Backend()
	if src != SourceAuto {
		t.Fatalf("src = %q, want auto", src)
	}
	t.Logf("auto-selected OS backend: %s", b)

	// Remove the credential even if a later assertion fails, so the test
	// never leaves an entry behind in the real OS keyring.
	t.Cleanup(func() { _ = s.Delete("default", "tok") })

	if err := s.Set("default", "tok", "v", WithOverwrite()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := s.Get("default", "tok"); err != nil || v != "v" {
		t.Fatalf("Get = (%q,%v), want (v,nil)", v, err)
	}
	if err := s.Delete("default", "tok"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

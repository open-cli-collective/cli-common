package credstore

import (
	"errors"
	"os"
	"strings"
	"testing"
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

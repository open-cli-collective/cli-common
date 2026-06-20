//go:build darwin && cgo

package credstore

import (
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/byteness/keyring"
)

func TestKeychainMetadataGated(t *testing.T) {
	if os.Getenv("CREDSTORE_OS_KEYRING_TEST") != "1" {
		t.Skip("set CREDSTORE_OS_KEYRING_TEST=1 to run real macOS Keychain metadata tests")
	}

	const (
		profile = "default"
		key     = "tok"
		value   = "v"
	)
	service := "credstore-metadata-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	account := profile + "/" + key
	t.Logf("using synthetic Keychain item service=%q account=%q", service, account)

	s, err := Open(service, &Options{Backend: BackendKeychain, AllowedKeys: []string{key}})
	if err != nil {
		t.Fatalf("Open keychain backend: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	t.Cleanup(func() { _ = s.Delete(profile, key) })

	if err := s.Set(profile, key, value, WithOverwrite()); err != nil {
		t.Fatalf("Set: %v", err)
	}

	kr, err := keyring.Open(keyring.Config{
		ServiceName:              service,
		AllowedBackends:          []keyring.BackendType{keyring.KeychainBackend},
		KeychainTrustApplication: true,
	})
	if err != nil {
		t.Fatalf("open raw keychain backend: %v", err)
	}
	t.Cleanup(func() { _ = kr.Remove(account) })

	assertMetadata := func(wantLabel, wantDescription string) {
		t.Helper()
		md, err := kr.GetMetadata(account)
		if err != nil {
			t.Fatalf("GetMetadata(%q): %v", account, err)
		}
		if md.Item == nil {
			t.Fatal("metadata item is nil")
		}
		if md.Label != wantLabel {
			t.Fatalf("Label = %q, want %q", md.Label, wantLabel)
		}
		if md.Description != wantDescription {
			t.Fatalf("Description = %q, want %q", md.Description, wantDescription)
		}
	}

	wantLabel := service + " " + account
	wantDescription := "Credential for " + service + " " + account
	assertMetadata(wantLabel, wantDescription)

	if err := s.Delete(profile, key); err != nil {
		t.Fatalf("Delete before overwrite phase: %v", err)
	}
	if err := kr.Set(keyring.Item{Key: account, Data: []byte("legacy")}); err != nil {
		t.Fatalf("seed blank metadata item: %v", err)
	}
	seeded, err := kr.GetMetadata(account)
	if err != nil {
		t.Fatalf("GetMetadata(%q) after seed: %v", account, err)
	}
	seededLabel, seededDescription := seeded.Label, seeded.Description
	if seededLabel == wantLabel && seededDescription == wantDescription {
		t.Fatalf("seeded item already has target metadata label=%q description=%q", seededLabel, seededDescription)
	}

	if err := s.Set(profile, key, "blocked"); !errors.Is(err, ErrExists) {
		t.Fatalf("Set without overwrite = %v, want ErrExists", err)
	}
	assertMetadata(seededLabel, seededDescription)

	if err := s.Set(profile, key, value, WithOverwrite()); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	assertMetadata(wantLabel, wantDescription)
	md, err := kr.GetMetadata(account)
	if err != nil {
		t.Fatalf("GetMetadata(%q): %v", account, err)
	}
	if len(md.Data) != 0 {
		t.Fatalf("metadata unexpectedly included secret data: %q", string(md.Data))
	}
}

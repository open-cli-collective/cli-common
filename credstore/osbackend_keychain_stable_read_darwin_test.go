//go:build darwin && cgo

package credstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStableKeychainReadRoundTripsBytesThroughRealSecurity pins the format of
// real `security -g` output. It uses only a throwaway keychain in a temp
// directory, whose items trust /usr/bin/security, so it touches no real keychain
// and raises no dialog.
func TestStableKeychainReadRoundTripsBytesThroughRealSecurity(t *testing.T) {
	const service = "credstore-read-probe"
	keychain := filepath.Join(t.TempDir(), "probe.keychain-db")
	security := func(args ...string) {
		t.Helper()
		// #nosec G204 -- fixed executable and test-controlled synthetic arguments.
		if out, err := exec.Command("/usr/bin/security", args...).CombinedOutput(); err != nil {
			t.Fatalf("security %s: %v\n%s", args[0], err, out)
		}
	}
	security("create-keychain", "-p", "probe", keychain)
	t.Cleanup(func() {
		// #nosec G204 -- fixed executable and test-owned path.
		_ = exec.Command("/usr/bin/security", "delete-keychain", keychain).Run()
	})
	security("set-keychain-settings", keychain)

	for _, tc := range []struct {
		name   string
		secret string
	}{
		{"printable", "abc123"},
		{"hex digits", "deadbeef"},
		{"hex prefix", "0xdeadbeef"},
		{"double quotes", `a"b"`},
		{"trailing space", "a b "},
		{"backslash", `a\b`},
		{"octal lookalike", `a\012b`},
		{"newline", "line1\nline2"},
		{"pem shape", "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"},
		{"trailing newline", "tok\n"},
		{"tab", "a\tb"},
		{"utf-8", "p\xc3\xa4ss"},
		{"invalid utf-8", "\xff\xfe"},
		{"invalid utf-8 beside text", "a\xffb"},
		{"nul", "a\x00b"},
		{"lone nul", "\x00"},
		{"lone control byte", "\x01"},
		{"repeats the password prefix", "x\npassword: \"y\" "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			security("add-generic-password", "-s", service, "-a", tc.name, "-X", hex.EncodeToString([]byte(tc.secret)), "-T", "/usr/bin/security", keychain)
			got := readFromKeychain(t, keychain, service, tc.name)
			if !bytes.Equal(got, []byte(tc.secret)) {
				t.Fatalf("read %q, want %q", got, tc.secret)
			}
		})
	}

	t.Run("empty", func(t *testing.T) {
		security("add-generic-password", "-s", service, "-a", "empty", "-w", "", "-T", "/usr/bin/security", keychain)
		if got := readFromKeychain(t, keychain, service, "empty"); len(got) != 0 {
			t.Fatalf("read %q, want empty", got)
		}
	})

	t.Run("missing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := readKeychainPasswordWith(ctx, service, "missing", securityIn(keychain))
		if !errors.Is(err, errKeyringItemNotFound) {
			t.Fatalf("read error = %v, want %v", err, errKeyringItemNotFound)
		}
	})
}

func readFromKeychain(t *testing.T, keychain, service, account string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := readKeychainPasswordWith(ctx, service, account, securityIn(keychain))
	if err != nil {
		t.Fatalf("readKeychainPasswordWith: %v", err)
	}
	return got
}

// securityIn runs the production command scoped to one keychain file instead of
// the user's search list.
func securityIn(keychain string) func(context.Context, ...string) ([]byte, error) {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		return runSecurity(ctx, append(args, keychain)...)
	}
}

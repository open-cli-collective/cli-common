//go:build darwin && cgo

package credstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
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
	newThrowawayKeychain(t, keychain)

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
			addSecret(t, keychain, service, tc.name, tc.secret)
			got := readFromKeychain(t, keychain, service, tc.name)
			if !bytes.Equal(got, []byte(tc.secret)) {
				t.Fatalf("read %q, want %q", got, tc.secret)
			}
		})
	}

	t.Run("empty", func(t *testing.T) {
		runSecurityTool(t, "add-generic-password", "-s", service, "-a", "empty", "-w", "", "-T", "/usr/bin/security", keychain)
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

// TestRepairScriptKeepsSecretBytes runs the repair script's delete-and-recreate
// step against a throwaway keychain. Every `security` call the script makes is
// scoped to that keychain, and the recreated items trust /usr/bin/security, so no
// real keychain is touched and no dialog appears.
func TestRepairScriptKeepsSecretBytes(t *testing.T) {
	const service = "credstore-repair-probe"
	keychain := filepath.Join(t.TempDir(), "probe.keychain-db")
	newThrowawayKeychain(t, keychain)

	for _, tc := range []struct {
		name   string
		secret string
	}{
		{"printable", "abc123"},
		{"hex digits", "deadbeef"},
		{"double quotes", `a"b"`},
		{"backslash", `a\b`},
		{"trailing space", "a b "},
		{"pem shape", "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"},
		{"trailing newline", "tok\n"},
		{"utf-8", "p\xc3\xa4ss"},
		{"invalid utf-8", "\xff\xfe"},
		{"long secret that fits one line", strings.Repeat("k", 1800)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addSecret(t, keychain, service, tc.name, tc.secret)
			if out, err := runRepairItem(keychain, service, tc.name); err != nil {
				t.Fatalf("repair_item: %v\n%s", err, out)
			} else if !bytes.Contains(out, []byte("REPAIRED")) {
				t.Fatalf("repair_item did not repair:\n%s", out)
			}
			if got := readFromKeychain(t, keychain, service, tc.name); !bytes.Equal(got, []byte(tc.secret)) {
				t.Fatalf("after repair read %q, want %q", got, tc.secret)
			}
		})
	}

	t.Run("refuses a secret too long for one line and leaves the item", func(t *testing.T) {
		secret := strings.Repeat("k", 3000)
		addSecret(t, keychain, service, "too long", secret)
		out, err := runRepairItem(keychain, service, "too long")
		if err == nil || !bytes.Contains(out, []byte("too long to recreate")) {
			t.Fatalf("repair_item error = %v, want the over-limit refusal:\n%s", err, out)
		}
		if got := readFromKeychain(t, keychain, service, "too long"); string(got) != secret {
			t.Fatalf("after refusal read %d bytes, want the original %d", len(got), len(secret))
		}
	})

	t.Run("restores the value when the recreated item differs", func(t *testing.T) {
		const secret = "original-value"
		addSecret(t, keychain, service, "corrupted recreate", secret)
		marker := filepath.Join(t.TempDir(), "corrupted")
		out, err := runRepairItem(keychain, service, "corrupted recreate", "PROBE_CORRUPT_ONCE_MARKER="+marker)
		if err == nil || !bytes.Contains(out, []byte("attempting restore")) || bytes.Contains(out, []byte("restore failed")) {
			t.Fatalf("repair_item error = %v, want a reported and successful restore:\n%s", err, out)
		}
		if got := readFromKeychain(t, keychain, service, "corrupted recreate"); string(got) != secret {
			t.Fatalf("after restore read %q, want %q", got, secret)
		}
	})

	t.Run("refuses an empty secret and leaves the item", func(t *testing.T) {
		runSecurityTool(t, "add-generic-password", "-s", service, "-a", "empty", "-w", "", "-T", "/usr/bin/security", keychain)
		if out, err := runRepairItem(keychain, service, "empty"); err == nil {
			t.Fatalf("repair_item succeeded on an empty secret:\n%s", out)
		}
		if got := readFromKeychain(t, keychain, service, "empty"); len(got) != 0 {
			t.Fatalf("read %q, want empty", got)
		}
	})
}

func runRepairItem(keychain, service, account string, extraEnv ...string) ([]byte, error) {
	const driver = `
source "$REPAIR_SCRIPT"
apply=1
# security -i drops the end of an over-long line, which would lose the keychain
# path and write to the user's default keychain, so such a line stops here.
security() {
  if [[ "$1" == "-i" ]]; then
    local line
    IFS= read -r line
    if ((${#line} > 4000)); then
      echo "over-long line reached security -i" >&2
      exit 99
    fi
    # Simulates security -i storing the wrong value on its first call.
    if [[ -n "${PROBE_CORRUPT_ONCE_MARKER:-}" && ! -e "$PROBE_CORRUPT_ONCE_MARKER" ]]; then
      : >"$PROBE_CORRUPT_ONCE_MARKER"
      line="${line% -X *} -X 6261"
    fi
    printf '%s "%s"\n' "$line" "$PROBE_KEYCHAIN" | command security -i
  else
    command security "$@" "$PROBE_KEYCHAIN"
  fi
}
repair_item probe "$PROBE_SERVICE" "$PROBE_ACCOUNT" /usr/bin/true
`
	cmd := exec.Command("/bin/bash", "-c", driver)
	cmd.Env = append(cmd.Environ(),
		"REPAIR_SCRIPT=../scripts/repair-macos-keychain-credentials.sh",
		"PROBE_KEYCHAIN="+keychain,
		"PROBE_SERVICE="+service,
		"PROBE_ACCOUNT="+account,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd.CombinedOutput()
}

func newThrowawayKeychain(t *testing.T, keychain string) {
	t.Helper()
	runSecurityTool(t, "create-keychain", "-p", "probe", keychain)
	t.Cleanup(func() {
		// #nosec G204 -- fixed executable and test-owned path.
		_ = exec.Command("/usr/bin/security", "delete-keychain", keychain).Run()
	})
	runSecurityTool(t, "set-keychain-settings", keychain)
}

func addSecret(t *testing.T, keychain, service, account, secret string) {
	t.Helper()
	runSecurityTool(t, "add-generic-password", "-s", service, "-a", account, "-X", hex.EncodeToString([]byte(secret)), "-T", "/usr/bin/security", keychain)
}

func runSecurityTool(t *testing.T, args ...string) {
	t.Helper()
	// #nosec G204 -- fixed executable and test-controlled synthetic arguments.
	if out, err := exec.Command("/usr/bin/security", args...).CombinedOutput(); err != nil {
		t.Fatalf("security %s: %v\n%s", args[0], err, out)
	}
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

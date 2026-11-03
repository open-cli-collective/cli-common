//go:build cgo || windows

package credstore

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type keychainExitCode int

func (e keychainExitCode) Error() string { return "security failed" }
func (e keychainExitCode) ExitCode() int { return int(e) }

func TestStableKeychainReadUsesServiceAndAccount(t *testing.T) {
	var args []string
	got, err := readKeychainPasswordWith(context.Background(), "example-cli", "default/bundle", func(_ context.Context, command ...string) ([]byte, error) {
		args = command
		return []byte("password: \"secret\"\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"find-generic-password", "-s", "example-cli", "-a", "default/bundle", "-g"}) {
		t.Fatalf("security args = %q", args)
	}
	if string(got) != "secret" {
		t.Fatal("credential changed during read")
	}
}

func TestParseKeychainPassword(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
		want   string
	}{
		{"printable", "password: \"abc123\"\n", "abc123"},
		{"hex digits stay text", "password: \"deadbeef\"\n", "deadbeef"},
		{"hex prefix stays text", "password: \"0xdeadbeef\"\n", "0xdeadbeef"},
		{"double quotes", "password: \"a\"b\"\"\n", "a\"b\""},
		{"trailing space", "password: \"a b \"\n", "a b "},
		{"no trailing newline", "password: \"abc\"", "abc"},
		{"empty", "password: \n", ""},
		{"hex newline", "password: 0x6C696E65310A6C696E6532  \"line1\nline2\"\n", "line1\nline2"},
		{"hex trailing newline", "password: 0x746F6B0A  \"tok\n\"\n", "tok\n"},
		{"hex utf-8", "password: 0x70C3A47373  \"p\\303\\244ss\"\n", "p\xc3\xa4ss"},
		{"hex backslash", "password: 0x615C62  \"a\\134b\"\n", "a\\b"},
		{"hex with no display text", "password: 0xFFFE \n", "\xff\xfe"},
		{"hex at end of output", "password: 0xFFFE", "\xff\xfe"},
		{"display text repeats the prefix", "password: 0x780A70617373776F72643A2022792220  \"x\npassword: \"y\" \"\n", "x\npassword: \"y\" "},
		{"preceding line", "note\npassword: \"abc\"\n", "abc"},
		{"prefix inside a preceding line", "not a password: \"no\"\npassword: \"abc\"\n", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseKeychainPassword([]byte(tc.output))
			if err != nil {
				t.Fatalf("parseKeychainPassword: %v", err)
			}
			if !bytes.Equal(got, []byte(tc.want)) {
				t.Fatalf("parseKeychainPassword = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseKeychainPasswordRejectsUnknownFormatWithoutQuotingIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"no password line", "keychain: \"login\"\n"},
		{"prefix not at a line start", "not a password: \"SENTINEL\"\n"},
		{"unquoted", "password: SENTINEL\n"},
		{"unterminated", "password: \"SENTINEL\n"},
		{"lone quote", "password: \"\n"},
		{"hex with no digits", "password: 0x  \"SENTINEL\"\n"},
		{"invalid hex", "password: 0xSENTINEL  \"SENTINEL\"\n"},
		{"odd hex", "password: 0xABC  \"SENTINEL\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseKeychainPassword([]byte(tc.output))
			if !errors.Is(err, errKeychainPasswordFormat) {
				t.Fatalf("parseKeychainPassword error = %v, want %v", err, errKeychainPasswordFormat)
			}
			if strings.Contains(err.Error(), "SENTINEL") {
				t.Fatalf("error quotes security output: %v", err)
			}
		})
	}
}

func TestStableKeychainReadErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"missing", keychainExitCode(44), errKeyringItemNotFound},
		{"denied", keychainExitCode(36), keychainExitCode(36)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readKeychainPasswordWith(context.Background(), "svc", "acct", func(context.Context, ...string) ([]byte, error) {
				return nil, tc.err
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("read error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStableKeychainReadTimeoutNamesThePendingApproval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readKeychainPasswordWith(ctx, "svc", "acct", func(context.Context, ...string) ([]byte, error) {
		return nil, keychainExitCode(-1)
	})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "/usr/bin/security") {
		t.Fatalf("read error = %v, want cancellation naming the security approval", err)
	}
}

func TestStableKeychainReadLeavesOtherOperationsNative(t *testing.T) {
	kr := &captureBytenessKeyring{}
	b := keychainStableReadBackend{
		keyringBackend: bytenessBackend{kr: kr},
		service:        "svc",
		read: func(service, account string) ([]byte, error) {
			if service != "svc" || account != "default/token" {
				t.Fatalf("read %q/%q", service, account)
			}
			return []byte("secret"), nil
		},
	}
	item, err := b.get("default/token")
	if err != nil || string(item.data) != "secret" || kr.getCalls != 0 {
		t.Fatalf("get = %q, %v; native get calls = %d", item.data, err, kr.getCalls)
	}
	if err := b.set(keyringItem{key: "default/token", data: []byte("new")}); err != nil || kr.setCalls != 1 {
		t.Fatalf("native set error = %v, calls = %d", err, kr.setCalls)
	}
}

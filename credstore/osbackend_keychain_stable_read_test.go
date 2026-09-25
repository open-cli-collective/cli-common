//go:build cgo || windows

package credstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type keychainExitCode int

func (e keychainExitCode) Error() string { return "security failed" }
func (e keychainExitCode) ExitCode() int { return int(e) }

func TestStableKeychainReadUsesServiceAndAccount(t *testing.T) {
	var args []string
	want := []byte("secret\nwith trailing newline\n")
	got, err := readKeychainPasswordWith(context.Background(), "example-cli", "default/bundle", func(_ context.Context, command ...string) ([]byte, error) {
		args = command
		return append(append([]byte(nil), want...), '\n'), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"find-generic-password", "-s", "example-cli", "-a", "default/bundle", "-w"}) {
		t.Fatalf("security args = %q", args)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("credential changed during read")
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

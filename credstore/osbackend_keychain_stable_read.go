//go:build cgo || windows

package credstore

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const keychainReadTimeout = 60 * time.Second

// keychainStableReadBackend retains native writes, metadata, enumeration and
// deletion. Only secret reads use the system-signed security executable, so
// a grant can survive replacement of an ad-hoc-signed CLI binary.
type keychainStableReadBackend struct {
	keyringBackend
	service string
	read    func(string, string) ([]byte, error)
}

func (b keychainStableReadBackend) get(itemKey string) (keyringItem, error) {
	data, err := b.read(b.service, itemKey)
	if err != nil {
		return keyringItem{}, err
	}
	return keyringItem{key: itemKey, data: data}, nil
}

func readKeychainPassword(service, account string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainReadTimeout)
	defer cancel()
	return readKeychainPasswordWith(ctx, service, account, func(ctx context.Context, args ...string) ([]byte, error) {
		// #nosec G204 -- fixed absolute executable and argv; no shell evaluates service or account.
		return exec.CommandContext(ctx, "/usr/bin/security", args...).Output()
	})
}

func readKeychainPasswordWith(ctx context.Context, service, account string, run func(context.Context, ...string) ([]byte, error)) ([]byte, error) {
	output, err := run(ctx, "find-generic-password", "-s", service, "-a", account, "-w")
	if err != nil {
		var status interface{ ExitCode() int }
		if errors.As(err, &status) && status.ExitCode() == 44 {
			return nil, errKeyringItemNotFound
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("macOS Keychain read timed out or was canceled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("macOS Keychain read failed: %w", err)
	}
	// security -w prints one terminal newline after the credential.
	if len(output) > 0 && output[len(output)-1] == '\n' {
		output = output[:len(output)-1]
	}
	return output, nil
}

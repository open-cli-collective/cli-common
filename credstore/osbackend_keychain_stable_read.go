//go:build cgo || windows

package credstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const keychainReadTimeout = 60 * time.Second

var errKeychainPasswordFormat = errors.New("macOS Keychain read returned output in an unrecognized format")

// keychainStableReadBackend retains native writes, metadata, enumeration and
// deletion. Only secret reads use the system-signed security executable, so
// a grant can survive replacement of a CLI binary: the Keychain partition list
// holds a per-build cdhash entry for a binary with no TeamIdentifier, while
// security keeps the same apple-tool entry across CLI releases.
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
	return readKeychainPasswordWith(ctx, service, account, runSecurity)
}

// runSecurity returns the stderr of /usr/bin/security, which is where -g writes
// the secret. Stdout carries only item attributes and is discarded.
func runSecurity(ctx context.Context, args ...string) ([]byte, error) {
	// #nosec G204 -- fixed absolute executable and argv; no shell evaluates service or account.
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.Bytes(), err
}

func readKeychainPasswordWith(ctx context.Context, service, account string, run func(context.Context, ...string) ([]byte, error)) ([]byte, error) {
	// -g rather than -w: -w prints a secret holding any non-printable byte as
	// bare hex, which is indistinguishable from a printable secret made of hex
	// digits. -g marks the hex form with a 0x prefix.
	output, err := run(ctx, "find-generic-password", "-s", service, "-a", account, "-g")
	if err != nil {
		var status interface{ ExitCode() int }
		if errors.As(err, &status) && status.ExitCode() == 44 {
			return nil, errKeyringItemNotFound
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("macOS Keychain read timed out or was canceled, possibly waiting for approval to let /usr/bin/security read the item: %w", ctx.Err())
		}
		return nil, fmt.Errorf("macOS Keychain read failed: %w", err)
	}
	return parseKeychainPassword(output)
}

// parseKeychainPassword extracts the secret from `security -g` output, which has
// one of three shapes:
//
//	password: "text"              every byte is printable and none is a backslash
//	password: 0xHEX  "display"    anything else; display is lossy and may span lines
//	password:                     empty secret
//
// The hex form drops the quoted display text when it would be empty.
//
// Errors never quote the output, which may hold the secret.
func parseKeychainPassword(output []byte) ([]byte, error) {
	const prefix = "password: "
	start := bytes.Index(output, []byte(prefix))
	if start < 0 {
		return nil, errKeychainPasswordFormat
	}
	if start > 0 && output[start-1] != '\n' {
		// The first prefix at a line start is the real one: display text in the
		// hex form may repeat it, but only after the real line.
		start = bytes.Index(output, []byte("\n"+prefix))
		if start < 0 {
			return nil, errKeychainPasswordFormat
		}
		start++
	}
	value := output[start+len(prefix):]
	if bytes.HasPrefix(value, []byte("0x")) {
		encoded := value[2:]
		if end := bytes.IndexAny(encoded, " \n"); end >= 0 {
			encoded = encoded[:end]
		}
		if len(encoded) == 0 {
			return nil, errKeychainPasswordFormat
		}
		decoded := make([]byte, hex.DecodedLen(len(encoded)))
		if _, err := hex.Decode(decoded, encoded); err != nil {
			return nil, errKeychainPasswordFormat
		}
		return decoded, nil
	}
	line, _, _ := bytes.Cut(value, []byte("\n"))
	if len(line) == 0 {
		return []byte{}, nil
	}
	if len(line) < 2 || line[0] != '"' || line[len(line)-1] != '"' {
		return nil, errKeychainPasswordFormat
	}
	return bytes.Clone(line[1 : len(line)-1]), nil
}

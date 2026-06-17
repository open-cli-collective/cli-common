//go:build !keyring_no1password && !freebsd && (cgo || windows)

package credstore

import (
	"errors"
	"testing"
)

func TestOpenOSBackend_OnePasswordEnabledBuildReachesBackendValidation(t *testing.T) {
	t.Setenv("OP_VAULT_ID", "")
	t.Setenv("OP_CONNECT_HOST", "")
	t.Setenv("OP_DESKTOP_ACCOUNT_ID", "")
	for _, kind := range []Backend{BackendOP, BackendOPConnect, BackendOPDesktop} {
		t.Run(string(kind), func(t *testing.T) {
			_, err := openOSBackend(kind, "credstore-optest", &Options{}, func(string) string { return "" })
			if err == nil {
				t.Fatal("openOSBackend unexpectedly succeeded without required 1Password configuration")
			}
			if errors.Is(err, ErrBackendNotImplemented) {
				t.Fatalf("openOSBackend(%q) = %v, want enabled backend validation error, not ErrBackendNotImplemented", kind, err)
			}
		})
	}
}

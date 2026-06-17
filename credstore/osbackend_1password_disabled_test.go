//go:build keyring_no1password

package credstore

import (
	"errors"
	"testing"
)

func TestUnsupportedOnePasswordBackendInBuild_DisabledBuildRejectsOnePasswordBackends(t *testing.T) {
	for _, kind := range []Backend{BackendOP, BackendOPConnect, BackendOPDesktop} {
		err := unsupportedOnePasswordBackendInBuild(kind)
		if !errors.Is(err, ErrBackendNotImplemented) {
			t.Fatalf("unsupportedOnePasswordBackendInBuild(%q) = %v, want ErrBackendNotImplemented", kind, err)
		}
	}
}

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

func TestOpen_OnePasswordDisabledBuildRejectsOnePasswordBackends(t *testing.T) {
	for _, kind := range []Backend{BackendOP, BackendOPConnect, BackendOPDesktop} {
		t.Run(string(kind), func(t *testing.T) {
			_, err := Open("credstore-optest", &Options{Backend: kind})
			if !errors.Is(err, ErrBackendNotImplemented) {
				t.Fatalf("Open(%q) = %v, want ErrBackendNotImplemented", kind, err)
			}
		})
	}
}

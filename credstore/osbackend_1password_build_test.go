package credstore

import (
	"errors"
	"testing"
)

func TestUnsupportedOnePasswordBackendInBuild_NonOnePasswordBackendsPass(t *testing.T) {
	for _, kind := range []Backend{BackendKeychain, BackendWinCred, BackendSecretService, BackendFile, BackendPass, BackendMemory} {
		if err := unsupportedOnePasswordBackendInBuild(kind); err != nil {
			t.Fatalf("unsupportedOnePasswordBackendInBuild(%q) = %v, want nil", kind, err)
		}
	}
}

func TestUnsupportedOnePasswordBackendInBuild_OnePasswordBackendsAreDeterministic(t *testing.T) {
	for _, kind := range []Backend{BackendOP, BackendOPConnect, BackendOPDesktop} {
		err := unsupportedOnePasswordBackendInBuild(kind)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrBackendNotImplemented) {
			t.Fatalf("unsupportedOnePasswordBackendInBuild(%q) = %v, want ErrBackendNotImplemented", kind, err)
		}
	}
}

package credstore

import "testing"

func TestUnsupportedOnePasswordBackendInBuild_NonOnePasswordBackendsPass(t *testing.T) {
	for _, kind := range []Backend{BackendKeychain, BackendWinCred, BackendSecretService, BackendFile, BackendPass, BackendMemory} {
		if err := unsupportedOnePasswordBackendInBuild(kind); err != nil {
			t.Fatalf("unsupportedOnePasswordBackendInBuild(%q) = %v, want nil", kind, err)
		}
	}
}

//go:build keyring_no1password

package credstore

import "fmt"

func unsupportedOnePasswordBackendInBuild(kind Backend) error {
	switch kind {
	case BackendOP, BackendOPConnect, BackendOPDesktop:
		return fmt.Errorf("%w: backend %q was compiled out of this build via keyring_no1password", ErrBackendNotImplemented, kind)
	default:
		return nil
	}
}

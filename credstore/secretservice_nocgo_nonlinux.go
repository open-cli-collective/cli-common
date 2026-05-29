//go:build !cgo && !linux && !windows

package credstore

func openSecretServiceBackend(backendConfig) (keyringBackend, error) {
	return nil, ErrBackendNotImplemented
}

//go:build !cgo && !linux && !darwin && !windows

package credstore

import "fmt"

func openKeyringBackend(kind Backend, _ backendConfig) (keyringBackend, error) {
	return nil, fmt.Errorf("%w: backend %q is not supported in no-CGO builds for this platform", ErrBackendNotImplemented, kind)
}

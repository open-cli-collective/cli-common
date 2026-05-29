//go:build !cgo && (linux || darwin)

package credstore

import (
	"fmt"
	"runtime"
)

func openKeyringBackend(kind Backend, cfg backendConfig) (keyringBackend, error) {
	switch kind {
	case BackendFile:
		return openFileBackend(cfg)
	case BackendPass:
		return openPassBackend(cfg)
	case BackendSecretService:
		if runtime.GOOS != "linux" {
			return nil, fmt.Errorf("%w: secret-service is only supported on Linux", ErrBackendNotImplemented)
		}
		return openSecretServiceBackend(cfg)
	case BackendKeychain:
		return nil, fmt.Errorf("%w: keychain requires CGO", ErrBackendNotImplemented)
	case BackendWinCred:
		return nil, fmt.Errorf("%w: wincred is only supported on Windows", ErrBackendNotImplemented)
	case BackendMemory:
		return nil, fmt.Errorf("%w: memory backend is opened before OS backend construction", ErrBackendNotImplemented)
	default:
		return nil, fmt.Errorf("%w: backend %q is not supported in no-CGO Unix builds", ErrBackendNotImplemented, kind)
	}
}

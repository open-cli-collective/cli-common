//go:build !cgo && !linux && !windows

package credstore

func probeSecretService(string, func(string) string) error {
	return ErrBackendNotImplemented
}

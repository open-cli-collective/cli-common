//go:build cgo || windows

package credstore

import "github.com/byteness/keyring"

// probeSecretService opens the Secret Service backend and performs one
// harmless operation (list keys) to force the D-Bus round-trip
// (keyring.Open is lazy). Returns the first error, or nil if Secret
// Service is reachable. The only impure piece; injected into Open via
// openWithDeps so tests never touch D-Bus. getenv is unused (Secret
// Service needs no env) but kept for signature symmetry with the
// injected probe type.
func probeSecretService(service string, _ func(string) string) error {
	kr, err := keyring.Open(keyring.Config{
		ServiceName:     service,
		AllowedBackends: []keyring.BackendType{keyring.SecretServiceBackend},
	})
	if err != nil {
		return err
	}
	_, err = kr.Keys()
	return err
}

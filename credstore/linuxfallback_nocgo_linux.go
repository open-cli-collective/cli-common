//go:build !cgo && linux

package credstore

func probeSecretService(service string, _ func(string) string) error {
	kr, err := openSecretServiceBackend(backendConfig{serviceName: service})
	if err != nil {
		return err
	}
	_, err = kr.keys()
	return err
}

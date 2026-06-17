//go:build !keyring_no1password

package credstore

func unsupportedOnePasswordBackendInBuild(Backend) error {
	return nil
}

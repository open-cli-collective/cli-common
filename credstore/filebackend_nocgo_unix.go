//go:build !cgo && (linux || darwin)

package credstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/byteness/percent"
)

type fileKeyringBackend struct {
	dir          string
	passwordFunc promptFunc
	password     string
}

func openFileBackend(cfg backendConfig) (keyringBackend, error) {
	return &fileKeyringBackend{
		dir:          cfg.fileDir,
		passwordFunc: cfg.filePasswordFunc,
	}, nil
}

func (b *fileKeyringBackend) get(itemKey string) (keyringItem, error) {
	name, err := b.filename(itemKey)
	if err != nil {
		return keyringItem{}, err
	}
	bytes, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return keyringItem{}, errKeyringItemNotFound
	}
	if err != nil {
		return keyringItem{}, err
	}
	if err := b.unlock(); err != nil {
		return keyringItem{}, err
	}
	return decodeFileKeyringItem(string(bytes), b.password)
}

func (b *fileKeyringBackend) metadata(itemKey string) (keyringItem, error) {
	name, err := b.filename(itemKey)
	if err != nil {
		return keyringItem{}, err
	}
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return keyringItem{}, errKeyringItemNotFound
		}
		return keyringItem{}, err
	}
	return keyringItem{key: itemKey}, nil
}

func (b *fileKeyringBackend) set(it keyringItem) error {
	if err := b.unlock(); err != nil {
		return err
	}
	token, err := encodeFileKeyringItem(it, b.password)
	if err != nil {
		return err
	}
	name, err := b.filename(it.key)
	if err != nil {
		return err
	}
	return os.WriteFile(name, []byte(token), 0600)
}

func (b *fileKeyringBackend) remove(itemKey string) error {
	name, err := b.filename(itemKey)
	if err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		if os.IsNotExist(err) {
			return errKeyringItemNotFound
		}
		return err
	}
	return nil
}

func (b *fileKeyringBackend) keys() ([]string, error) {
	dir, err := b.resolveDir()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		keys = append(keys, percent.Decode(f.Name()))
	}
	return keys, nil
}

func (b *fileKeyringBackend) unlock() error {
	if _, err := b.resolveDir(); err != nil {
		return err
	}
	if b.password != "" {
		return nil
	}
	if b.passwordFunc == nil {
		return ErrFilePassphraseRequired
	}
	pwd, err := b.passwordFunc(fmt.Sprintf("Enter passphrase to unlock %q", b.dir))
	if err != nil {
		return err
	}
	b.password = pwd
	return nil
}

func (b *fileKeyringBackend) filename(itemKey string) (string, error) {
	dir, err := b.resolveDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileKeyringFilename(itemKey)), nil
}

func (b *fileKeyringBackend) resolveDir() (string, error) {
	if b.dir == "" {
		return "", fmt.Errorf("No directory provided for file keyring")
	}
	dir, err := expandTilde(b.dir)
	if err != nil {
		return "", err
	}
	stat, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		err = os.MkdirAll(dir, 0700)
	case err != nil:
		return "", err
	case !stat.IsDir():
		return "", fmt.Errorf("%s is a file, not a directory", dir)
	}
	return dir, err
}

func expandTilde(dir string) (string, error) {
	prefix := string([]rune{'~', filepath.Separator})
	if strings.HasPrefix(dir, prefix) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = strings.Replace(dir, "~", home, 1)
	}
	return dir, nil
}

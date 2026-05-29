//go:build !cgo && (linux || darwin)

package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type passKeyringBackend struct {
	dir     string
	passcmd string
	prefix  string
}

func openPassBackend(cfg backendConfig) (keyringBackend, error) {
	b := &passKeyringBackend{
		dir:     cfg.passDir,
		passcmd: cfg.passCmd,
		prefix:  cfg.passPrefix,
	}
	if b.passcmd == "" {
		b.passcmd = "pass"
	}
	if b.dir == "" {
		if passDir, ok := os.LookupEnv("PASSWORD_STORE_DIR"); ok {
			b.dir = passDir
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			b.dir = filepath.Join(home, ".password-store")
		}
	}
	var err error
	b.dir, err = expandTilde(b.dir)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(b.passcmd); err != nil {
		return nil, errors.New("The pass program is not available")
	}
	return b, nil
}

func (b *passKeyringBackend) get(itemKey string) (keyringItem, error) {
	if err := b.checkItemExists(itemKey); err != nil {
		if errors.Is(err, errKeyringItemNotFound) {
			return keyringItem{}, errKeyringItemNotFound
		}
		return keyringItem{}, err
	}
	itemPath, err := passItemPath(b.prefix, itemKey)
	if err != nil {
		return keyringItem{}, err
	}
	cmd := b.pass("show", itemPath)
	output, err := cmd.Output()
	if err != nil {
		return keyringItem{}, err
	}
	return decodePassItemOutput(output)
}

func (b *passKeyringBackend) set(it keyringItem) error {
	bytes, err := json.Marshal(persistedKeyringItem{Key: it.key, Data: it.data})
	if err != nil {
		return err
	}
	itemPath, err := passItemPath(b.prefix, it.key)
	if err != nil {
		return err
	}
	cmd := b.pass("insert", "-m", "-f", itemPath)
	cmd.Stdin = strings.NewReader(string(bytes))
	return cmd.Run()
}

func (b *passKeyringBackend) remove(itemKey string) error {
	if err := b.checkItemExists(itemKey); err != nil {
		return err
	}
	itemPath, err := passItemPath(b.prefix, itemKey)
	if err != nil {
		return err
	}
	return b.pass("rm", "-f", itemPath).Run()
}

func (b *passKeyringBackend) keys() ([]string, error) {
	keys := []string{}
	path := filepath.Join(b.dir, b.prefix)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return keys, nil
		}
		return keys, err
	}
	if !info.IsDir() {
		return keys, fmt.Errorf("%s is not a directory", path)
	}
	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(p) == ".gpg" {
			name := strings.TrimPrefix(p, path)
			if name[0] == os.PathSeparator {
				name = name[1:]
			}
			keys = append(keys, name[:len(name)-4])
		}
		return nil
	})
	return keys, err
}

func (b *passKeyringBackend) pass(args ...string) *exec.Cmd {
	cmd := exec.Command(b.passcmd, args...)
	if b.dir != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("PASSWORD_STORE_DIR=%s", b.dir))
	}
	cmd.Stderr = os.Stderr
	return cmd
}

func (b *passKeyringBackend) checkItemExists(itemKey string) error {
	itemPath, err := passItemPath(b.prefix, itemKey)
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(b.dir, itemPath+".gpg"))
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return errKeyringItemNotFound
	}
	return err
}

func passItemPath(prefix, itemKey string) (string, error) {
	if itemKey == "" || filepath.IsAbs(itemKey) {
		return "", fmt.Errorf("credstore: pass item path %q is invalid", itemKey)
	}
	parts := strings.Split(itemKey, string(os.PathSeparator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("credstore: pass item path %q escapes the service prefix", itemKey)
		}
	}
	clean := filepath.Clean(itemKey)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("credstore: pass item path %q escapes the service prefix", itemKey)
	}
	return filepath.Join(prefix, clean), nil
}

func decodePassItemOutput(output []byte) (keyringItem, error) {
	var decoded persistedKeyringItem
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(output))), &decoded); err != nil {
		return keyringItem{}, err
	}
	return keyringItem{key: decoded.Key, data: decoded.Data}, nil
}

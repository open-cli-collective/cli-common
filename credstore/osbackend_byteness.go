//go:build cgo || windows

package credstore

import (
	"errors"
	"fmt"

	"github.com/byteness/keyring"
)

func openKeyringBackend(kind Backend, cfg backendConfig) (keyringBackend, error) {
	kcfg := keyring.Config{
		ServiceName:              cfg.serviceName,
		AllowedBackends:          []keyring.BackendType{keyring.BackendType(cfg.allowedBackend)},
		KeychainTrustApplication: cfg.keychainTrustApplication,
		FileDir:                  cfg.fileDir,
		PassDir:                  cfg.passDir,
		PassCmd:                  cfg.passCmd,
		PassPrefix:               cfg.passPrefix,
	}
	if cfg.filePasswordFunc != nil {
		kcfg.FilePasswordFunc = keyring.PromptFunc(cfg.filePasswordFunc)
	}
	kr, err := keyring.Open(kcfg)
	if err != nil {
		return nil, fmt.Errorf("keyring open %s: %w", kind, err)
	}
	return bytenessBackend{kr: kr}, nil
}

type bytenessBackend struct {
	kr keyring.Keyring
}

func (b bytenessBackend) get(itemKey string) (keyringItem, error) {
	it, err := b.kr.Get(itemKey)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return keyringItem{}, errKeyringItemNotFound
		}
		return keyringItem{}, err
	}
	return keyringItem{
		key:                         it.Key,
		data:                        it.Data,
		label:                       it.Label,
		description:                 it.Description,
		keychainNotTrustApplication: it.KeychainNotTrustApplication,
		keychainNotSynchronizable:   it.KeychainNotSynchronizable,
	}, nil
}

func (b bytenessBackend) set(it keyringItem) error {
	return b.kr.Set(keyring.Item{
		Key:                         it.key,
		Data:                        it.data,
		Label:                       it.label,
		Description:                 it.description,
		KeychainNotTrustApplication: it.keychainNotTrustApplication,
		KeychainNotSynchronizable:   it.keychainNotSynchronizable,
	})
}

func (b bytenessBackend) remove(itemKey string) error {
	if err := b.kr.Remove(itemKey); err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return errKeyringItemNotFound
		}
		return err
	}
	return nil
}

func (b bytenessBackend) keys() ([]string, error) {
	return b.kr.Keys()
}

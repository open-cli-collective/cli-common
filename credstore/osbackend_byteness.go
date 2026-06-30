//go:build cgo || windows

package credstore

import (
	"errors"
	"fmt"

	"github.com/byteness/keyring"
)

func openKeyringBackend(kind Backend, cfg backendConfig) (keyringBackend, error) {
	kcfg := keyringConfigFromBackendConfig(cfg)
	kr, err := keyring.Open(kcfg)
	if err != nil {
		return nil, fmt.Errorf("keyring open %s: %w", kind, err)
	}
	return bytenessBackend{kr: kr}, nil
}

func keyringConfigFromBackendConfig(cfg backendConfig) keyring.Config {
	kcfg := keyring.Config{
		ServiceName:              cfg.serviceName,
		AllowedBackends:          []keyring.BackendType{keyring.BackendType(cfg.allowedBackend)},
		KeychainTrustApplication: cfg.keychainTrustApplication,
		FileDir:                  cfg.fileDir,
		PassDir:                  cfg.passDir,
		PassCmd:                  cfg.passCmd,
		PassPrefix:               cfg.passPrefix,
		OPTimeout:                cfg.opTimeout,
		OPVaultID:                cfg.opVaultID,
		OPItemTitlePrefix:        cfg.opItemTitlePrefix,
		OPItemTag:                cfg.opItemTag,
		OPItemFieldTitle:         cfg.opItemFieldTitle,
		OPConnectHost:            cfg.opConnectHost,
		OPConnectTokenEnv:        cfg.opConnectTokenEnv,
		OPTokenEnv:               cfg.opTokenEnv,
		OPDesktopAccountID:       cfg.opDesktopAccountID,
	}
	if cfg.filePasswordFunc != nil {
		kcfg.FilePasswordFunc = keyring.PromptFunc(cfg.filePasswordFunc)
	}
	return kcfg
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

func (b bytenessBackend) metadata(itemKey string) (keyringItem, error) {
	md, err := b.kr.GetMetadata(itemKey)
	if err != nil {
		switch {
		case errors.Is(err, keyring.ErrKeyNotFound):
			return keyringItem{}, errKeyringItemNotFound
		case errors.Is(err, keyring.ErrMetadataNotSupported), errors.Is(err, keyring.ErrMetadataNeedsCredentials):
			return keyringItem{}, errKeyringMetadataUnsupported
		default:
			return keyringItem{}, err
		}
	}
	if md.Item == nil {
		if md.ModificationTime.IsZero() {
			return keyringItem{}, errKeyringMetadataUnsupported
		}
		return keyringItem{}, nil
	}
	return keyringItem{
		key:                         md.Key,
		label:                       md.Label,
		description:                 md.Description,
		keychainNotTrustApplication: md.KeychainNotTrustApplication,
		keychainNotSynchronizable:   md.KeychainNotSynchronizable,
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

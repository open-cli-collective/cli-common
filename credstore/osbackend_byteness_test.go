//go:build cgo || windows

package credstore

import (
	"errors"
	"testing"
	"time"

	"github.com/byteness/keyring"
)

type captureBytenessKeyring struct {
	setItem                           keyring.Item
	metadataRet                       keyring.Metadata
	metadataErr                       error
	getCalls, metadataCalls, setCalls int
}

func (c *captureBytenessKeyring) Get(string) (keyring.Item, error) {
	c.getCalls++
	return keyring.Item{}, keyring.ErrKeyNotFound
}

func (c *captureBytenessKeyring) GetMetadata(string) (keyring.Metadata, error) {
	c.metadataCalls++
	if c.metadataErr != nil {
		return keyring.Metadata{}, c.metadataErr
	}
	return c.metadataRet, nil
}

func (c *captureBytenessKeyring) Set(item keyring.Item) error {
	c.setCalls++
	c.setItem = item
	return nil
}

func (c *captureBytenessKeyring) Remove(string) error { return nil }

func (c *captureBytenessKeyring) Keys() ([]string, error) { return nil, nil }

func TestBytenessBackendSetPassesThroughMetadata(t *testing.T) {
	kr := &captureBytenessKeyring{}
	be := bytenessBackend{kr: kr}

	item := keyringItem{
		key:                         "default/git_token",
		data:                        []byte("secret"),
		label:                       "codereview default/git_token",
		description:                 "Credential for codereview default/git_token",
		keychainNotTrustApplication: true,
		keychainNotSynchronizable:   true,
	}
	if err := be.set(item); err != nil {
		t.Fatalf("set: %v", err)
	}

	if kr.setItem.Key != item.key || string(kr.setItem.Data) != "secret" {
		t.Fatalf("stored item = (%q,%q), want (%q,%q)", kr.setItem.Key, string(kr.setItem.Data), item.key, "secret")
	}
	if kr.setItem.Label != item.label {
		t.Fatalf("Label = %q, want %q", kr.setItem.Label, item.label)
	}
	if kr.setItem.Description != item.description {
		t.Fatalf("Description = %q, want %q", kr.setItem.Description, item.description)
	}
	if !kr.setItem.KeychainNotTrustApplication {
		t.Fatal("KeychainNotTrustApplication = false, want true")
	}
	if !kr.setItem.KeychainNotSynchronizable {
		t.Fatal("KeychainNotSynchronizable = false, want true")
	}
}

func TestBytenessBackendMetadataUsesMetadataOnly(t *testing.T) {
	kr := &captureBytenessKeyring{
		metadataRet: keyring.Metadata{Item: &keyring.Item{
			Key:                         "default/git_token",
			Data:                        []byte("secret data must not be copied"),
			Label:                       "label",
			Description:                 "description",
			KeychainNotTrustApplication: true,
			KeychainNotSynchronizable:   true,
		}},
	}
	be := bytenessBackend{kr: kr}

	it, err := be.metadata("default/git_token")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if kr.metadataCalls != 1 {
		t.Fatalf("GetMetadata calls = %d, want 1", kr.metadataCalls)
	}
	if kr.getCalls != 0 {
		t.Fatalf("Get calls = %d, want 0", kr.getCalls)
	}
	if it.key != "default/git_token" || it.label != "label" || it.description != "description" {
		t.Fatalf("metadata item = %+v", it)
	}
	if len(it.data) != 0 {
		t.Fatalf("metadata copied secret data: %q", string(it.data))
	}
	if !it.keychainNotTrustApplication {
		t.Fatal("keychainNotTrustApplication = false, want true")
	}
	if !it.keychainNotSynchronizable {
		t.Fatal("keychainNotSynchronizable = false, want true")
	}
}

func TestBytenessBackendMetadataErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"not found", keyring.ErrKeyNotFound, errKeyringItemNotFound},
		{"not supported", keyring.ErrMetadataNotSupported, errKeyringMetadataUnsupported},
		{"needs credentials", keyring.ErrMetadataNeedsCredentials, errKeyringMetadataUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			be := bytenessBackend{kr: &captureBytenessKeyring{metadataErr: tc.err}}
			_, err := be.metadata("default/git_token")
			if !errors.Is(err, tc.want) {
				t.Fatalf("metadata err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestKeyringConfigFromBackendConfigForwardsOnePasswordOptions(t *testing.T) {
	// #nosec G101 -- test fixture values are non-secret placeholders
	cfg := keyringConfigFromBackendConfig(backendConfig{
		serviceName:        "codereview",
		allowedBackend:     BackendOPDesktop,
		opTimeout:          5 * time.Second,
		opVaultID:          "vault-123",
		opItemTitlePrefix:  "cr",
		opItemTag:          "codereview",
		opItemFieldTitle:   "credential",
		opConnectHost:      "https://connect.example",
		opConnectTokenEnv:  "CUSTOM_OP_CONNECT_TOKEN",
		opTokenEnv:         "CUSTOM_OP_TOKEN",
		opDesktopAccountID: "desktop-account",
	})

	if cfg.ServiceName != "codereview" {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, "codereview")
	}
	if len(cfg.AllowedBackends) != 1 || cfg.AllowedBackends[0] != keyring.OPDesktopBackend {
		t.Fatalf("AllowedBackends = %v, want [%v]", cfg.AllowedBackends, keyring.OPDesktopBackend)
	}
	if cfg.OPTimeout != 5*time.Second || cfg.OPVaultID != "vault-123" {
		t.Fatalf("unexpected timeout/vault forwarding: %+v", cfg)
	}
	if cfg.OPItemTitlePrefix != "cr" || cfg.OPItemTag != "codereview" || cfg.OPItemFieldTitle != "credential" {
		t.Fatalf("unexpected item metadata forwarding: %+v", cfg)
	}
	if cfg.OPConnectHost != "https://connect.example" || cfg.OPConnectTokenEnv != "CUSTOM_OP_CONNECT_TOKEN" {
		t.Fatalf("unexpected connect forwarding: %+v", cfg)
	}
	if cfg.OPTokenEnv != "CUSTOM_OP_TOKEN" || cfg.OPDesktopAccountID != "desktop-account" {
		t.Fatalf("unexpected token/account forwarding: %+v", cfg)
	}
}

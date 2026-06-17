//go:build cgo || windows

package credstore

import (
	"testing"
	"time"

	"github.com/byteness/keyring"
)

type captureBytenessKeyring struct {
	setItem keyring.Item
}

func (c *captureBytenessKeyring) Get(string) (keyring.Item, error) {
	return keyring.Item{}, keyring.ErrKeyNotFound
}

func (c *captureBytenessKeyring) GetMetadata(string) (keyring.Metadata, error) {
	return keyring.Metadata{}, keyring.ErrMetadataNotSupported
}

func (c *captureBytenessKeyring) Set(item keyring.Item) error {
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

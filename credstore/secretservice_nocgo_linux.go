//go:build !cgo && linux

package credstore

import (
	"fmt"

	"github.com/byteness/go-libsecret"
)

func openSecretServiceBackend(cfg backendConfig) (keyringBackend, error) {
	service, err := libsecret.NewService()
	if err != nil {
		return nil, err
	}
	return openSecretServiceBackendWithService(cfg.serviceName, &libsecretServiceAdapter{service: service})
}

type libsecretServiceAdapter struct {
	service *libsecret.Service
	session *libsecret.Session
}

func (a *libsecretServiceAdapter) open() error {
	session, err := a.service.Open()
	if err != nil {
		return err
	}
	a.session = session
	return nil
}

func (a *libsecretServiceAdapter) collections() ([]secretServiceCollection, error) {
	collections, err := a.service.Collections()
	if err != nil {
		return nil, err
	}
	out := make([]secretServiceCollection, 0, len(collections))
	for i := range collections {
		out = append(out, libsecretCollectionAdapter{collection: &collections[i], service: a})
	}
	return out, nil
}

func (a *libsecretServiceAdapter) createCollection(name string) (secretServiceCollection, error) {
	collection, err := a.service.CreateCollection(name)
	if err != nil {
		return nil, err
	}
	return libsecretCollectionAdapter{collection: collection, service: a}, nil
}

func (a *libsecretServiceAdapter) unlock(target secretServiceUnlockable) error {
	dbusObject, ok := target.(libsecret.DBusObject)
	if !ok {
		return fmt.Errorf("credstore: secret-service unlock target %T is not a D-Bus object", target)
	}
	return a.service.Unlock(dbusObject)
}

type libsecretCollectionAdapter struct {
	collection *libsecret.Collection
	service    *libsecretServiceAdapter
}

func (a libsecretCollectionAdapter) path() string {
	return string(a.collection.Path())
}

func (a libsecretCollectionAdapter) label() (string, error) {
	return a.path(), nil
}

func (a libsecretCollectionAdapter) locked() (bool, error) {
	return a.collection.Locked()
}

func (a libsecretCollectionAdapter) items() ([]secretServiceItem, error) {
	items, err := a.collection.Items()
	if err != nil {
		return nil, err
	}
	return a.adaptItems(items), nil
}

func (a libsecretCollectionAdapter) searchItems(itemKey string) ([]secretServiceItem, error) {
	items, err := a.collection.SearchItems(itemKey)
	if err != nil {
		return nil, err
	}
	return a.adaptItems(items), nil
}

func (a libsecretCollectionAdapter) createItem(label string, value []byte) error {
	secret := libsecret.NewSecret(a.service.session, []byte{}, value, "application/json")
	_, err := a.collection.CreateItem(label, secret, true)
	return err
}

func (a libsecretCollectionAdapter) unlockTarget() secretServiceUnlockable {
	return a.collection
}

func (a libsecretCollectionAdapter) adaptItems(items []libsecret.Item) []secretServiceItem {
	out := make([]secretServiceItem, 0, len(items))
	for i := range items {
		out = append(out, libsecretItemAdapter{item: &items[i], service: a.service})
	}
	return out
}

type libsecretItemAdapter struct {
	item    *libsecret.Item
	service *libsecretServiceAdapter
}

func (a libsecretItemAdapter) locked() (bool, error) {
	return a.item.Locked()
}

func (a libsecretItemAdapter) secretValue() ([]byte, error) {
	secret, err := a.item.GetSecret(a.service.session)
	if err != nil {
		return nil, err
	}
	return secret.Value, nil
}

func (a libsecretItemAdapter) label() (string, error) {
	return a.item.Label()
}

func (a libsecretItemAdapter) delete() error {
	return a.item.Delete()
}

func (a libsecretItemAdapter) unlockTarget() secretServiceUnlockable {
	return a.item
}

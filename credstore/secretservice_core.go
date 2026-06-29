package credstore

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const secretServiceDBusPath = "/org/freedesktop/secrets"

var errSecretServiceCollectionNotFound = errors.New("the collection does not exist; add a key first")

type secretServiceReadableItem interface {
	locked() (bool, error)
	secretValue() ([]byte, error)
}

type secretServiceLabeler interface {
	label() (string, error)
}

type secretServiceService interface {
	open() error
	collections() ([]secretServiceCollection, error)
	createCollection(string) (secretServiceCollection, error)
	unlock(secretServiceUnlockable) error
}

type secretServiceUnlockable interface{}

type secretServiceCollection interface {
	secretServiceLabeler
	locked() (bool, error)
	path() string
	items() ([]secretServiceItem, error)
	searchItems(string) ([]secretServiceItem, error)
	createItem(string, []byte) error
	unlockTarget() secretServiceUnlockable
}

type secretServiceItem interface {
	secretServiceReadableItem
	secretServiceLabeler
	delete() error
	unlockTarget() secretServiceUnlockable
}

type secretServiceBackend struct {
	name       string
	service    secretServiceService
	collection secretServiceCollection
}

func openSecretServiceBackendWithService(name string, service secretServiceService) (keyringBackend, error) {
	if name == "" {
		name = "secret-service"
	}
	b := &secretServiceBackend{
		name:    name,
		service: service,
	}
	return b, b.openSecrets()
}

func (b *secretServiceBackend) get(itemKey string) (keyringItem, error) {
	if err := b.openCollection(); err != nil {
		return keyringItem{}, secretServiceNotFoundError(err)
	}
	items, err := b.collection.searchItems(itemKey)
	if err != nil {
		return keyringItem{}, err
	}
	if len(items) == 0 {
		return keyringItem{}, errKeyringItemNotFound
	}
	item := items[0]
	return readSecretServiceItem(item, func() error {
		return b.service.unlock(item.unlockTarget())
	})
}

func (b *secretServiceBackend) metadata(itemKey string) (keyringItem, error) {
	if err := b.openCollection(); err != nil {
		return keyringItem{}, secretServiceNotFoundError(err)
	}
	items, err := b.collection.searchItems(itemKey)
	if err != nil {
		return keyringItem{}, err
	}
	if len(items) == 0 {
		return keyringItem{}, errKeyringItemNotFound
	}
	return keyringItem{key: itemKey}, nil
}

func (b *secretServiceBackend) set(it keyringItem) error {
	if err := b.openSecrets(); err != nil {
		return err
	}
	if b.collection == nil {
		collection, err := b.service.createCollection(b.name)
		if err != nil {
			return err
		}
		b.collection = collection
	}
	if err := b.ensureCollectionUnlocked(); err != nil {
		return err
	}
	data, err := encodeSecretServiceItem(it)
	if err != nil {
		return err
	}
	return b.collection.createItem(it.key, data)
}

func (b *secretServiceBackend) remove(itemKey string) error {
	if err := b.openCollection(); err != nil {
		return secretServiceNotFoundError(err)
	}
	items, err := b.collection.searchItems(itemKey)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errKeyringItemNotFound
	}
	item := items[0]
	locked, err := item.locked()
	if err != nil {
		return err
	}
	if locked {
		if err := b.service.unlock(item.unlockTarget()); err != nil {
			return err
		}
	}
	return item.delete()
}

func (b *secretServiceBackend) keys() ([]string, error) {
	if err := b.openCollection(); err != nil {
		return secretServiceKeysError(err)
	}
	if err := b.ensureCollectionUnlocked(); err != nil {
		return nil, err
	}
	items, err := b.collection.items()
	if err != nil {
		return nil, err
	}
	labelers := make([]secretServiceLabeler, 0, len(items))
	for _, item := range items {
		labelers = append(labelers, item)
	}
	return secretServiceKeysFromItems(labelers), nil
}

func (b *secretServiceBackend) openCollection() error {
	if err := b.openSecrets(); err != nil {
		return err
	}
	if b.collection == nil {
		return errSecretServiceCollectionNotFound
	}
	return nil
}

func (b *secretServiceBackend) openSecrets() error {
	if err := b.service.open(); err != nil {
		return err
	}
	collections, err := b.service.collections()
	if err != nil {
		return err
	}
	for _, collection := range collections {
		if secretServiceCollectionPathMatches(collection.path(), b.name) {
			b.collection = collection
			return nil
		}
	}
	return nil
}

func (b *secretServiceBackend) ensureCollectionUnlocked() error {
	locked, err := b.collection.locked()
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	return b.service.unlock(b.collection.unlockTarget())
}

func secretServiceCollectionPathMatches(collectionPath, name string) bool {
	return decodeSecretServicePath(collectionPath) == secretServiceDBusPath+"/collection/"+name
}

func readSecretServiceItem(item secretServiceReadableItem, unlock func() error) (keyringItem, error) {
	locked, err := item.locked()
	if err != nil {
		return keyringItem{}, err
	}
	if locked {
		if err := unlock(); err != nil {
			return keyringItem{}, err
		}
	}
	value, err := item.secretValue()
	if err != nil {
		return keyringItem{}, err
	}
	return decodeSecretServiceItem(value)
}

func encodeSecretServiceItem(it keyringItem) ([]byte, error) {
	return json.Marshal(persistedKeyringItem{Key: it.key, Data: it.data})
}

func decodeSecretServiceItem(value []byte) (keyringItem, error) {
	var decoded persistedKeyringItem
	if err := json.Unmarshal(value, &decoded); err != nil {
		return keyringItem{}, err
	}
	return keyringItem{key: decoded.Key, data: decoded.Data}, nil
}

func secretServiceKeysFromItems(items []secretServiceLabeler) []string {
	keys := []string{}
	for _, item := range items {
		label, err := item.label()
		if err == nil {
			keys = append(keys, label)
		}
	}
	return keys
}

func secretServiceKeysError(err error) ([]string, error) {
	if secretServiceIsMissingCollection(err) {
		return []string{}, nil
	}
	return nil, err
}

func secretServiceNotFoundError(err error) error {
	if secretServiceIsMissingCollection(err) {
		return errKeyringItemNotFound
	}
	return err
}

func secretServiceIsMissingCollection(err error) bool {
	return errors.Is(err, errSecretServiceCollectionNotFound)
}

func decodeSecretServicePath(src string) string {
	var dst strings.Builder
	for i := 0; i < len(src); i++ {
		if src[i] != '_' {
			dst.WriteByte(src[i])
			continue
		}
		if i+3 > len(src) {
			dst.WriteByte(src[i])
			continue
		}
		decoded, err := hex.DecodeString(src[i+1 : i+3])
		if err != nil {
			dst.WriteByte(src[i])
			continue
		}
		dst.Write(decoded)
		i += 2
	}
	return dst.String()
}

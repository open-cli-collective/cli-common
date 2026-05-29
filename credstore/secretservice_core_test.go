package credstore

import (
	"errors"
	"testing"
)

func TestSecretServiceCollectionPathMatches(t *testing.T) {
	if !secretServiceCollectionPathMatches("/org/freedesktop/secrets/collection/atlassian_2dcli", "atlassian-cli") {
		t.Fatal("escaped collection path did not match decoded collection name")
	}
	if secretServiceCollectionPathMatches("/org/freedesktop/secrets/collection/slack-chat-api", "atlassian-cli") {
		t.Fatal("wrong collection name matched")
	}
	if got := decodeSecretServicePath("/bad/path/_zz"); got != "/bad/path/_zz" {
		t.Fatalf("invalid escape decoded to %q", got)
	}
	if got := decodeSecretServicePath("/org/freedesktop/secrets/collection/literal_name"); got != "/org/freedesktop/secrets/collection/literal_name" {
		t.Fatalf("literal underscore decoded to %q", got)
	}
}

func TestReadSecretServiceItem(t *testing.T) {
	t.Run("unlocks before reading", func(t *testing.T) {
		item := fakeSecretServiceItem{
			lockedRet: true,
			value:     mustEncodeSecretServiceItem(t, keyringItem{key: "default/tok", data: []byte("secret")}),
		}
		unlocked := false
		got, err := readSecretServiceItem(item, func() error {
			unlocked = true
			return nil
		})
		if err != nil {
			t.Fatalf("readSecretServiceItem: %v", err)
		}
		if !unlocked {
			t.Fatal("locked item was not unlocked before read")
		}
		if got.key != "default/tok" || string(got.data) != "secret" {
			t.Fatalf("decoded item = (%q,%q), want (default/tok,secret)", got.key, got.data)
		}
	})

	t.Run("locked error wins", func(t *testing.T) {
		sentinel := errors.New("locked failed")
		_, err := readSecretServiceItem(fakeSecretServiceItem{lockedErr: sentinel}, func() error {
			t.Fatal("unlock must not run after locked error")
			return nil
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want %v", err, sentinel)
		}
	})

	t.Run("unlock error wins", func(t *testing.T) {
		sentinel := errors.New("unlock failed")
		_, err := readSecretServiceItem(fakeSecretServiceItem{lockedRet: true}, func() error {
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want %v", err, sentinel)
		}
	})

	t.Run("secret error wins", func(t *testing.T) {
		sentinel := errors.New("secret failed")
		_, err := readSecretServiceItem(fakeSecretServiceItem{secretErr: sentinel}, func() error { return nil })
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want %v", err, sentinel)
		}
	})
}

func TestSecretServiceKeysFromItems(t *testing.T) {
	keys := secretServiceKeysFromItems([]secretServiceLabeler{
		fakeSecretServiceLabeler{labelRet: "default/a"},
		fakeSecretServiceLabeler{labelErr: errors.New("label failed")},
		fakeSecretServiceLabeler{labelRet: "default/b"},
	})
	eqStrings(t, "secret-service keys", keys, []string{"default/a", "default/b"})
}

func TestSecretServiceMissingCollectionMapping(t *testing.T) {
	keys, err := secretServiceKeysError(errSecretServiceCollectionNotFound)
	if err != nil {
		t.Fatalf("secretServiceKeysError: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys = %v, want empty", keys)
	}
	if err := secretServiceNotFoundError(errSecretServiceCollectionNotFound); !errors.Is(err, errKeyringItemNotFound) {
		t.Fatalf("secretServiceNotFoundError = %v, want errKeyringItemNotFound", err)
	}
	sentinel := errors.New("dbus failed")
	if _, err := secretServiceKeysError(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("secretServiceKeysError = %v, want sentinel", err)
	}
	if err := secretServiceNotFoundError(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("secretServiceNotFoundError = %v, want sentinel", err)
	}
}

func TestSecretServiceBackendWithFakeService(t *testing.T) {
	t.Run("discovers collection and lists keys", func(t *testing.T) {
		service := &fakeSecretService{
			collectionsRet: []secretServiceCollection{
				&fakeSecretServiceCollection{pathRet: "/org/freedesktop/secrets/collection/other"},
				&fakeSecretServiceCollection{
					pathRet: "/org/freedesktop/secrets/collection/atlassian_2dcli",
					itemsRet: []secretServiceItem{
						&fakeSecretServiceItem{labelRet: "default/a"},
						&fakeSecretServiceItem{labelErr: errors.New("ignored")},
						&fakeSecretServiceItem{labelRet: "default/b"},
					},
				},
			},
		}
		be, err := openSecretServiceBackendWithService("atlassian-cli", service)
		if err != nil {
			t.Fatalf("openSecretServiceBackendWithService: %v", err)
		}
		keys, err := be.(*secretServiceBackend).keys()
		if err != nil {
			t.Fatalf("keys: %v", err)
		}
		eqStrings(t, "keys", keys, []string{"default/a", "default/b"})
		if service.openCalls == 0 || service.collectionsCalls == 0 {
			t.Fatalf("service was not opened/discovered: open=%d collections=%d", service.openCalls, service.collectionsCalls)
		}
	})

	t.Run("missing collection maps get to not found and keys to empty", func(t *testing.T) {
		be, err := openSecretServiceBackendWithService("missing", &fakeSecretService{})
		if err != nil {
			t.Fatalf("openSecretServiceBackendWithService: %v", err)
		}
		if _, err := be.(*secretServiceBackend).get("default/tok"); !errors.Is(err, errKeyringItemNotFound) {
			t.Fatalf("get missing collection = %v, want errKeyringItemNotFound", err)
		}
		keys, err := be.(*secretServiceBackend).keys()
		if err != nil {
			t.Fatalf("keys missing collection: %v", err)
		}
		if len(keys) != 0 {
			t.Fatalf("keys missing collection = %v, want empty", keys)
		}
	})

	t.Run("get unlocks and decodes first search result", func(t *testing.T) {
		item := &fakeSecretServiceItem{
			lockedRet: true,
			value:     mustEncodeSecretServiceItem(t, keyringItem{key: "default/tok", data: []byte("secret")}),
		}
		service := &fakeSecretService{
			collectionsRet: []secretServiceCollection{&fakeSecretServiceCollection{
				pathRet:   "/org/freedesktop/secrets/collection/svc",
				searchRet: []secretServiceItem{item},
			}},
		}
		be, err := openSecretServiceBackendWithService("svc", service)
		if err != nil {
			t.Fatalf("openSecretServiceBackendWithService: %v", err)
		}
		got, err := be.(*secretServiceBackend).get("default/tok")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.key != "default/tok" || string(got.data) != "secret" {
			t.Fatalf("get = (%q,%q), want (default/tok,secret)", got.key, got.data)
		}
		if service.unlockCalls != 1 || service.lastUnlock != item {
			t.Fatalf("unlock = (%d,%T), want one item unlock", service.unlockCalls, service.lastUnlock)
		}
	})

	t.Run("set creates missing collection and writes encoded item", func(t *testing.T) {
		created := &fakeSecretServiceCollection{}
		service := &fakeSecretService{createRet: created}
		be, err := openSecretServiceBackendWithService("svc", service)
		if err != nil {
			t.Fatalf("openSecretServiceBackendWithService: %v", err)
		}
		if err := be.(*secretServiceBackend).set(keyringItem{key: "default/tok", data: []byte("new-secret")}); err != nil {
			t.Fatalf("set: %v", err)
		}
		if service.createName != "svc" {
			t.Fatalf("createName = %q, want svc", service.createName)
		}
		if created.createLabel != "default/tok" {
			t.Fatalf("createLabel = %q, want default/tok", created.createLabel)
		}
		decoded, err := decodeSecretServiceItem(created.createValue)
		if err != nil {
			t.Fatalf("decode created value: %v", err)
		}
		if string(decoded.data) != "new-secret" {
			t.Fatalf("created data = %q, want new-secret", decoded.data)
		}
	})

	t.Run("remove unlocks and deletes first match", func(t *testing.T) {
		item := &fakeSecretServiceItem{lockedRet: true}
		service := &fakeSecretService{
			collectionsRet: []secretServiceCollection{&fakeSecretServiceCollection{
				pathRet:   "/org/freedesktop/secrets/collection/svc",
				searchRet: []secretServiceItem{item},
			}},
		}
		be, err := openSecretServiceBackendWithService("svc", service)
		if err != nil {
			t.Fatalf("openSecretServiceBackendWithService: %v", err)
		}
		if err := be.(*secretServiceBackend).remove("default/tok"); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if service.unlockCalls != 1 || !item.deleted {
			t.Fatalf("remove did not unlock/delete: unlock=%d deleted=%v", service.unlockCalls, item.deleted)
		}
	})

	t.Run("remove missing item reports not found", func(t *testing.T) {
		service := &fakeSecretService{
			collectionsRet: []secretServiceCollection{&fakeSecretServiceCollection{
				pathRet: "/org/freedesktop/secrets/collection/svc",
			}},
		}
		be, err := openSecretServiceBackendWithService("svc", service)
		if err != nil {
			t.Fatalf("openSecretServiceBackendWithService: %v", err)
		}
		if err := be.(*secretServiceBackend).remove("default/missing"); !errors.Is(err, errKeyringItemNotFound) {
			t.Fatalf("remove missing item = %v, want errKeyringItemNotFound", err)
		}
	})
}

type fakeSecretServiceItem struct {
	lockedRet bool
	lockedErr error
	value     []byte
	secretErr error
	labelRet  string
	labelErr  error
	deleteErr error
	deleted   bool
}

func (f fakeSecretServiceItem) locked() (bool, error) {
	return f.lockedRet, f.lockedErr
}

func (f fakeSecretServiceItem) secretValue() ([]byte, error) {
	return f.value, f.secretErr
}

func (f *fakeSecretServiceItem) label() (string, error) {
	return f.labelRet, f.labelErr
}

func (f *fakeSecretServiceItem) delete() error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = true
	return nil
}

func (f *fakeSecretServiceItem) unlockTarget() secretServiceUnlockable {
	return f
}

type fakeSecretServiceLabeler struct {
	labelRet string
	labelErr error
}

func (f fakeSecretServiceLabeler) label() (string, error) {
	return f.labelRet, f.labelErr
}

type fakeSecretService struct {
	openErr          error
	collectionsRet   []secretServiceCollection
	collectionsErr   error
	createRet        secretServiceCollection
	createErr        error
	unlockErr        error
	openCalls        int
	collectionsCalls int
	createName       string
	unlockCalls      int
	lastUnlock       secretServiceUnlockable
}

func (f *fakeSecretService) open() error {
	f.openCalls++
	return f.openErr
}

func (f *fakeSecretService) collections() ([]secretServiceCollection, error) {
	f.collectionsCalls++
	return f.collectionsRet, f.collectionsErr
}

func (f *fakeSecretService) createCollection(name string) (secretServiceCollection, error) {
	f.createName = name
	return f.createRet, f.createErr
}

func (f *fakeSecretService) unlock(target secretServiceUnlockable) error {
	f.unlockCalls++
	f.lastUnlock = target
	return f.unlockErr
}

type fakeSecretServiceCollection struct {
	pathRet     string
	lockedRet   bool
	lockedErr   error
	itemsRet    []secretServiceItem
	itemsErr    error
	searchRet   []secretServiceItem
	searchErr   error
	createErr   error
	createLabel string
	createValue []byte
}

func (f *fakeSecretServiceCollection) path() string {
	return f.pathRet
}

func (f *fakeSecretServiceCollection) label() (string, error) {
	return f.pathRet, nil
}

func (f *fakeSecretServiceCollection) locked() (bool, error) {
	return f.lockedRet, f.lockedErr
}

func (f *fakeSecretServiceCollection) items() ([]secretServiceItem, error) {
	return f.itemsRet, f.itemsErr
}

func (f *fakeSecretServiceCollection) searchItems(string) ([]secretServiceItem, error) {
	return f.searchRet, f.searchErr
}

func (f *fakeSecretServiceCollection) createItem(label string, value []byte) error {
	f.createLabel = label
	f.createValue = value
	return f.createErr
}

func (f *fakeSecretServiceCollection) unlockTarget() secretServiceUnlockable {
	return f
}

func mustEncodeSecretServiceItem(t *testing.T, it keyringItem) []byte {
	t.Helper()
	data, err := encodeSecretServiceItem(it)
	if err != nil {
		t.Fatalf("encodeSecretServiceItem: %v", err)
	}
	return data
}

package credstore

import "sync"

// memoryBackend is the in-memory backend (§2.1): no disk side effects,
// safe for concurrent use, used by tests and CI where no OS keyring is
// available. A nil map signals closed.
type memoryBackend struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{m: make(map[string]string)}
}

func (b *memoryBackend) kind() Backend { return BackendMemory }

func (b *memoryBackend) get(itemKey string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		return "", ErrStoreClosed
	}
	v, ok := b.m[itemKey]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// set performs the conditional write atomically under b.mu: when
// overwrite is false and the entry exists, it returns ErrExists and
// leaves the store unchanged.
func (b *memoryBackend) set(itemKey, value string, overwrite bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		return ErrStoreClosed
	}
	if !overwrite {
		if _, ok := b.m[itemKey]; ok {
			return ErrExists
		}
	}
	b.m[itemKey] = value
	return nil
}

func (b *memoryBackend) delete(itemKey string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		return ErrStoreClosed
	}
	if _, ok := b.m[itemKey]; !ok {
		return ErrNotFound
	}
	delete(b.m, itemKey)
	return nil
}

func (b *memoryBackend) exists(itemKey string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		return false, ErrStoreClosed
	}
	_, ok := b.m[itemKey]
	return ok, nil
}

// close best-effort clears values then drops the map. Go string secrets
// cannot be guaranteed zeroized; this is the best a Go library can do.
func (b *memoryBackend) close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.m {
		b.m[k] = ""
	}
	b.m = nil
	return nil
}

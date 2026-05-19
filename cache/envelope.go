// Package cache is the directory-agnostic tier-1 cache core
// (working-with-state.md §5b): a self-describing JSON envelope with atomic
// temp-file-rename writes, version-mismatch-as-miss, and freshness
// classification. Location is always injected via Locator; this package never
// resolves a directory and never imports a CLI's config. Tier 2 (resource
// registry / dependency DAG / fetchers / refresh wiring) is deliberately out
// of scope (§5b, rule-of-three, deferred).
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Version is the on-disk envelope schema version. A mismatch is treated as a
// cache miss so schema bumps self-heal on the next write.
const Version = 1

// ErrCacheMiss reports an absent or version-mismatched envelope. It is not an
// error condition for callers — it is the "fetch and populate" signal.
var ErrCacheMiss = errors.New("cache: miss")

// Envelope is the on-disk JSON shape for a single cached resource.
type Envelope[T any] struct {
	Resource  string    `json:"resource"`
	Instance  string    `json:"instance"`
	FetchedAt time.Time `json:"fetched_at"`
	TTL       string    `json:"ttl"`
	Version   int       `json:"version"`
	Data      T         `json:"data"`
}

// ReadResource reads the envelope for name at loc.
//   - (envelope, nil) on success.
//   - (zero, ErrCacheMiss) if the file does not exist or the on-disk Version
//     differs from the current schema.
//   - (zero, error) on path validation, I/O, or JSON decode failure.
//
// ReadResource does NOT check freshness; callers use Classify.
func ReadResource[T any](loc Locator, name string) (Envelope[T], error) {
	path, err := loc.resourceFile(name)
	if err != nil {
		return Envelope[T]{}, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path already validated + composed by Locator.resourceFile (Root absolute, components regex-checked)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Envelope[T]{}, ErrCacheMiss
		}
		return Envelope[T]{}, fmt.Errorf("cache: reading resource file: %w", err)
	}

	var env Envelope[T]
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope[T]{}, fmt.Errorf("cache: parsing resource file: %w", err)
	}

	// Version or identity mismatch ⇒ treat as a miss (same self-healing rule):
	// a hand-edited or misplaced file whose metadata disagrees with the key it
	// was read under must not be returned as if it were that resource.
	if env.Version != Version || env.Resource != name || env.Instance != loc.InstanceKey {
		return Envelope[T]{}, ErrCacheMiss
	}
	return env, nil
}

// WriteResource atomically writes an envelope for name at loc. Resource,
// Instance (= loc.InstanceKey), Version, and FetchedAt (UTC now) are set here;
// ttl comes from the caller (a hard-coded per-resource value — §4.4).
func WriteResource[T any](loc Locator, name, ttl string, data T) error {
	env := Envelope[T]{
		Resource:  name,
		Instance:  loc.InstanceKey,
		FetchedAt: time.Now().UTC(),
		TTL:       ttl,
		Version:   Version,
		Data:      data,
	}
	return atomicWriteEnvelope(loc, name, env)
}

// atomicWriteEnvelope marshals env and writes it to the cache path for name
// using a temp-file-in-same-dir → rename. The rename makes the final file
// appear atomically (a reader sees either the old envelope or the new one,
// never a partial one). The temp file is removed on every error branch; a
// hard process/host crash can still leave an orphan *.json.tmp, which the
// next successful write supersedes (it is never read as an envelope).
func atomicWriteEnvelope[T any](loc Locator, name string, env Envelope[T]) error {
	path, err := loc.resourceFile(name)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cache: creating cache directory: %w", err)
	}

	jsonData, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("cache: marshaling envelope: %w", err)
	}

	tmp, err := os.CreateTemp(dir, name+"-*.json.tmp")
	if err != nil {
		return fmt.Errorf("cache: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(jsonData); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cache: writing temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cache: closing temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cache: setting file mode: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cache: moving temp file to final path: %w", err)
	}
	return nil
}

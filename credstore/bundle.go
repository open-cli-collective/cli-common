package credstore

// Bundle operations over a profile's keys, per the Secret-Handling
// Standard §2.1 (ListBundle/DeleteBundle/SetBundle), §1.5.1 (SetBundle
// atomicity), §1.5.2 (allowlist gates writes/deletes, not reads), and
// §1.7 (config clear removes the whole bundle).

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Result reports the disposition of a SetBundle call.
//
//	Written   – writes retained on return (bare keys, sorted)
//	Restored  – pre-existing keys put back to their prior value by rollback
//	Deleted   – new keys removed by rollback
//	Untouched – target keys not changed by this call: a failed
//	            no-overwrite ErrExists key (left as the racer wrote it)
//	            plus keys never attempted after the failure point
//
// On success only Written is populated; on rollback Written is nil.
type Result struct {
	Written   []string
	Restored  []string
	Deleted   []string
	Untouched []string
}

// validateProfile mirrors joinItemKey's profile checks (§1.3): a profile
// is a non-empty [A-Za-z0-9_-] segment.
func validateProfile(profile string) error {
	if profile == "" {
		return &RefError{Kind: RefErrorEmpty, Segment: "profile"}
	}
	if !validSegment(profile) {
		return &RefError{Kind: RefErrorInvalidChar, Segment: "profile", Ref: profile}
	}
	return nil
}

// bundleBareKeys returns the sorted bare keys stored under profile (the
// "<key>" half of every "<profile>/<key>" Item.Key). Caller holds s.mu.
func (s *Store) bundleBareKeys(profile string) ([]string, error) {
	all, err := s.be.listKeys()
	if err != nil {
		return nil, err
	}
	prefix := profile + "/"
	var keys []string
	for _, ik := range all {
		if strings.HasPrefix(ik, prefix) {
			keys = append(keys, ik[len(prefix):])
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// ListBundle returns the sorted keys stored under profile. A valid
// profile with no keys returns (nil, nil). Not allowlist-gated — it
// reports stored reality (§1.5.2 gates writes/deletes, not reads).
func (s *Store) ListBundle(profile string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrStoreClosed
	}
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	return s.bundleBareKeys(profile)
}

// DeleteBundle removes every key under profile (config clear, §1.7). It
// is idempotent (a valid profile with no keys → (nil, nil)) and not
// allowlist-gated. It does not fail-fast: every key is attempted; if any
// deletes fail it still returns the keys actually deleted plus an error
// naming all failed keys (fail-fast would strand later secrets).
func (s *Store) DeleteBundle(profile string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrStoreClosed
	}
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	targets, err := s.bundleBareKeys(profile)
	if err != nil {
		return nil, err
	}
	prefix := profile + "/"
	var deleted, failed []string
	for _, k := range targets {
		if err := s.be.delete(prefix + k); err != nil {
			failed = append(failed, k)
			continue
		}
		deleted = append(deleted, k)
	}
	if len(failed) > 0 {
		return deleted, fmt.Errorf("credstore: DeleteBundle(%q): failed to delete %d key(s): %s",
			profile, len(failed), strings.Join(failed, ", "))
	}
	return deleted, nil
}

// SetBundle writes kv under profile, implementing the §1.5.1 atomicity
// contract: validate everything first; without WithOverwrite any
// pre-existing target fails the whole call; with WithOverwrite, prior
// values are snapshotted before any write so a mid-bundle failure can be
// rolled back. The call-scoped snapshot is best-effort cleared before
// return (Go strings cannot be guaranteed zeroized).
func (s *Store) SetBundle(profile string, kv map[string]string, opts ...SetOpt) (Result, error) {
	var so setOptions
	for _, o := range opts {
		o(&so)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Result{}, ErrStoreClosed
	}
	if err := validateProfile(profile); err != nil {
		return Result{}, err
	}
	if len(kv) == 0 {
		return Result{}, nil
	}

	// Deterministic order; validate all keys (syntax + allowlist) and
	// build Item.Keys before any write.
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	itemKey := make(map[string]string, len(keys))
	for _, k := range keys {
		ik, err := joinItemKey(profile, k)
		if err != nil {
			return Result{}, err
		}
		if err := s.checkAllowed(k); err != nil {
			return Result{}, err
		}
		itemKey[k] = ik
	}

	// Pre-write state.
	existed := make(map[string]bool, len(keys))
	for _, k := range keys {
		ok, err := s.be.exists(itemKey[k])
		if err != nil {
			return Result{}, err
		}
		existed[k] = ok
	}
	snap := make(map[string]string) // bare key -> prior value (existing only)
	if !so.overwrite {
		var conflicts []string
		for _, k := range keys {
			if existed[k] {
				conflicts = append(conflicts, k)
			}
		}
		if len(conflicts) > 0 {
			return Result{}, fmt.Errorf("credstore: SetBundle(%q): %w for key(s): %s",
				profile, ErrExists, strings.Join(conflicts, ", "))
		}
	} else {
		for _, k := range keys {
			if existed[k] {
				v, err := s.be.get(itemKey[k])
				if err != nil {
					return Result{}, err
				}
				snap[k] = v
			}
		}
	}

	// Forward write using the real overwrite flag so the backend's
	// atomic no-overwrite guard still holds against a racer.
	var written []string
	var writeErr error
	failedKey := ""
	for _, k := range keys {
		if err := s.be.set(itemKey[k], kv[k], so.overwrite); err != nil {
			writeErr = err
			failedKey = k
			break
		}
		written = append(written, k)
	}
	if writeErr == nil {
		clearSnapshot(snap)
		return Result{Written: append([]string(nil), keys...)}, nil
	}

	// Roll back. ErrExists under !overwrite means a racer owns failedKey
	// — never touch it; only undo our own writes. Any other error is
	// ambiguous (a future backend's set may mutate on error) so failedKey
	// is rolled back too.
	isExists := errors.Is(writeErr, ErrExists)
	rbKeys := append([]string(nil), written...)
	if !isExists {
		rbKeys = append(rbKeys, failedKey)
	}
	sort.Strings(rbKeys)

	var res Result
	var rbFailed []string
	for _, k := range rbKeys {
		if prior, ok := snap[k]; ok {
			if err := s.be.set(itemKey[k], prior, true); err != nil {
				rbFailed = append(rbFailed, k)
				continue
			}
			res.Restored = append(res.Restored, k)
		} else {
			if err := s.be.delete(itemKey[k]); err != nil && !errors.Is(err, ErrNotFound) {
				rbFailed = append(rbFailed, k)
				continue
			}
			res.Deleted = append(res.Deleted, k)
		}
	}
	sort.Strings(res.Restored)
	sort.Strings(res.Deleted)

	attempted := make(map[string]bool, len(written)+1)
	for _, k := range written {
		attempted[k] = true
	}
	attempted[failedKey] = true // attempted; rolled back unless ErrExists
	var untouched []string
	for _, k := range keys {
		if !attempted[k] {
			untouched = append(untouched, k)
		}
	}
	if isExists {
		untouched = append(untouched, failedKey) // racer's value, left as-is
	}
	sort.Strings(untouched)
	res.Untouched = untouched

	clearSnapshot(snap)
	if len(rbFailed) > 0 {
		sort.Strings(rbFailed)
		return res, fmt.Errorf("credstore: SetBundle(%q): write failed at %q: %w; rollback also failed for %s — keyring may be inconsistent",
			profile, failedKey, writeErr, strings.Join(rbFailed, ", "))
	}
	return res, fmt.Errorf("credstore: SetBundle(%q): write failed at %q: %w", profile, failedKey, writeErr)
}

// clearSnapshot best-effort overwrites the snapshot's values. The map is
// call-scoped and goes out of scope on return; as elsewhere, Go strings
// cannot be guaranteed zeroized — this is the best a Go library can do.
func clearSnapshot(snap map[string]string) {
	for k := range snap {
		snap[k] = ""
	}
}

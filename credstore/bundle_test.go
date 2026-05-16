package credstore

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func memOf(t *testing.T, s *Store) *memoryBackend {
	t.Helper()
	mb, ok := s.be.(*memoryBackend)
	if !ok {
		t.Fatalf("backend is not *memoryBackend")
	}
	return mb
}

func eqStrings(t *testing.T, ctx string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", ctx, got, want)
	}
}

func TestListBundle(t *testing.T) {
	s := openMem(t)
	if _, err := s.ListBundle(""); !errors.Is(err, ErrRefEmpty) {
		t.Fatalf("ListBundle(\"\") err = %v, want ErrRefEmpty", err)
	}
	if _, err := s.ListBundle("bad.prof"); !errors.Is(err, ErrRefInvalidChar) {
		t.Fatalf("ListBundle(invalid) err = %v, want ErrRefInvalidChar", err)
	}
	if got, err := s.ListBundle("default"); err != nil || got != nil {
		t.Fatalf("ListBundle(empty profile) = (%v,%v), want (nil,nil)", got, err)
	}

	mustSet(t, s, "default", "b_key", "1")
	mustSet(t, s, "default", "a_key", "2")
	mustSet(t, s, "other", "z_key", "3")
	got, err := s.ListBundle("default")
	if err != nil {
		t.Fatalf("ListBundle: %v", err)
	}
	eqStrings(t, "ListBundle(default)", got, []string{"a_key", "b_key"})

	// Not allowlist-gated: a key not in any allowlist, stored directly,
	// is still listed (config show / migration need stored reality).
	if err := memOf(t, s).set("default/legacy_key", "x", false); err != nil {
		t.Fatalf("seed legacy key: %v", err)
	}
	got, _ = s.ListBundle("default")
	eqStrings(t, "ListBundle after legacy", got, []string{"a_key", "b_key", "legacy_key"})
}

func TestListBundleClosed(t *testing.T) {
	s, _ := Open("svc", &Options{Backend: BackendMemory})
	_ = s.Close()
	if _, err := s.ListBundle("default"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("ListBundle after Close = %v, want ErrStoreClosed", err)
	}
}

func TestDeleteBundle(t *testing.T) {
	s := openMem(t)
	if _, err := s.DeleteBundle(""); !errors.Is(err, ErrRefEmpty) {
		t.Fatalf("DeleteBundle(\"\") err = %v, want ErrRefEmpty", err)
	}
	if _, err := s.DeleteBundle("bad.prof"); !errors.Is(err, ErrRefInvalidChar) {
		t.Fatalf("DeleteBundle(invalid) err = %v, want ErrRefInvalidChar", err)
	}
	if got, err := s.DeleteBundle("default"); err != nil || got != nil {
		t.Fatalf("DeleteBundle(empty) = (%v,%v), want (nil,nil) idempotent", got, err)
	}

	mustSet(t, s, "default", "k1", "v")
	mustSet(t, s, "default", "k2", "v")
	mustSet(t, s, "keep", "k3", "v")
	deleted, err := s.DeleteBundle("default")
	if err != nil {
		t.Fatalf("DeleteBundle: %v", err)
	}
	eqStrings(t, "DeleteBundle(default)", deleted, []string{"k1", "k2"})
	if got, _ := s.ListBundle("default"); got != nil {
		t.Fatalf("default not empty after DeleteBundle: %v", got)
	}
	if got, _ := s.ListBundle("keep"); len(got) != 1 || got[0] != "k3" {
		t.Fatalf("other profile damaged: %v", got)
	}
}

func TestDeleteBundleAttemptsAllOnFailure(t *testing.T) {
	s := openMem(t)
	mustSet(t, s, "p", "a", "v")
	mustSet(t, s, "p", "b", "v")
	mustSet(t, s, "p", "c", "v")
	// Fail deletes of a and c; b must still be deleted (no fail-fast).
	memOf(t, s).deleteHook = func(ik string) error {
		if ik == "p/a" || ik == "p/c" {
			return errors.New("device busy")
		}
		return nil
	}
	deleted, err := s.DeleteBundle("p")
	if err == nil {
		t.Fatal("DeleteBundle: expected error naming failed keys")
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "c") {
		t.Fatalf("error must name both failed keys: %v", err)
	}
	eqStrings(t, "DeleteBundle deleted", deleted, []string{"b"})
	// The keys whose delete failed must still be present (not silently
	// dropped from tracking).
	for _, k := range []string{"a", "c"} {
		if ok, _ := s.Exists("p", k); !ok {
			t.Fatalf("failed-delete key %q must still be present", k)
		}
	}
}

func TestSetBundleHappyPath(t *testing.T) {
	s := openMem(t)
	res, err := s.SetBundle("default", map[string]string{"b": "2", "a": "1", "c": "3"})
	if err != nil {
		t.Fatalf("SetBundle: %v", err)
	}
	eqStrings(t, "Written", res.Written, []string{"a", "b", "c"})
	if res.Restored != nil || res.Absent != nil || res.Untouched != nil {
		t.Fatalf("non-Written fields should be nil on success: %+v", res)
	}
	for k, want := range map[string]string{"a": "1", "b": "2", "c": "3"} {
		if v, _ := s.Get("default", k); v != want {
			t.Fatalf("Get(%q) = %q, want %q", k, v, want)
		}
	}
}

func TestSetBundleProfileAndEmpty(t *testing.T) {
	s := openMem(t)
	if _, err := s.SetBundle("", nil); !errors.Is(err, ErrRefEmpty) {
		t.Fatalf("SetBundle(\"\",nil) = %v, want ErrRefEmpty (not silent no-op)", err)
	}
	if res, err := s.SetBundle("default", nil); err != nil || res.Written != nil {
		t.Fatalf("SetBundle(default,nil) = (%+v,%v), want empty Result nil err", res, err)
	}
	if _, err := s.SetBundle("bad.prof", map[string]string{"a": "1"}); !errors.Is(err, ErrRefInvalidChar) {
		t.Fatalf("SetBundle(invalid profile) = %v, want ErrRefInvalidChar", err)
	}
}

func TestSetBundleNoOverwriteConflict(t *testing.T) {
	s := openMem(t)
	mustSet(t, s, "default", "exists", "old")
	mustSet(t, s, "default", "also", "old2")
	res, err := s.SetBundle("default", map[string]string{"exists": "new", "also": "new2", "fresh": "v"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if !strings.Contains(err.Error(), "exists") || !strings.Contains(err.Error(), "also") {
		t.Fatalf("error must name all conflicting keys: %v", err)
	}
	if res.Written != nil {
		t.Fatalf("nothing should be written on conflict: %+v", res)
	}
	if v, _ := s.Get("default", "exists"); v != "old" {
		t.Fatalf("existing key mutated: %q", v)
	}
	if v, _ := s.Get("default", "also"); v != "old2" {
		t.Fatalf("second existing key mutated: %q", v)
	}
	if ok, _ := s.Exists("default", "fresh"); ok {
		t.Fatal("fresh key written despite conflict gate")
	}
}

func TestSetBundleOverwrite(t *testing.T) {
	s := openMem(t)
	mustSet(t, s, "default", "a", "old")
	res, err := s.SetBundle("default", map[string]string{"a": "newA", "b": "newB"}, WithOverwrite())
	if err != nil {
		t.Fatalf("SetBundle overwrite: %v", err)
	}
	eqStrings(t, "Written", res.Written, []string{"a", "b"})
	if v, _ := s.Get("default", "a"); v != "newA" {
		t.Fatalf("a = %q, want newA", v)
	}
}

func TestSetBundleRollbackNewKeysDeleted(t *testing.T) {
	s := openMem(t)
	memOf(t, s).setHook = func(ik string) error {
		if ik == "default/c" {
			return errors.New("backend exploded")
		}
		return nil
	}
	res, err := s.SetBundle("default", map[string]string{"a": "1", "b": "2", "c": "3"})
	if err == nil || !strings.Contains(err.Error(), `"c"`) {
		t.Fatalf("err = %v, want write-failed-at c", err)
	}
	if res.Written != nil {
		t.Fatalf("Written must be nil after full rollback: %+v", res)
	}
	eqStrings(t, "Absent", res.Absent, []string{"a", "b", "c"})
	for _, k := range []string{"a", "b", "c"} {
		if ok, _ := s.Exists("default", k); ok {
			t.Fatalf("key %q survived rollback", k)
		}
	}
}

func TestSetBundleRollbackRestoresPriorValues(t *testing.T) {
	s := openMem(t)
	mustSet(t, s, "default", "a", "ORIG")
	memOf(t, s).setHook = func(ik string) error {
		if ik == "default/c" {
			return errors.New("boom")
		}
		return nil
	}
	res, err := s.SetBundle("default",
		map[string]string{"a": "NEW", "b": "NEW", "c": "NEW"}, WithOverwrite())
	if err == nil {
		t.Fatal("expected mid-bundle failure")
	}
	eqStrings(t, "Restored", res.Restored, []string{"a"})
	eqStrings(t, "Absent", res.Absent, []string{"b", "c"})
	if v, _ := s.Get("default", "a"); v != "ORIG" {
		t.Fatalf("a not restored to prior value: %q (snapshot must capture pre-write state)", v)
	}
	if ok, _ := s.Exists("default", "b"); ok {
		t.Fatal("new key b not rolled back")
	}
}

func TestSetBundleAllRestoreRollback(t *testing.T) {
	// Every target key pre-exists, so rollback is pure restore (no
	// delete/Absent). The forward write of "c" fails on its first call;
	// the hook lets c's *restore* (second call) through — the
	// distinguish-by-call pattern the setHook doc prescribes.
	s := openMem(t)
	mustSet(t, s, "default", "a", "OA")
	mustSet(t, s, "default", "b", "OB")
	mustSet(t, s, "default", "c", "OC")
	calls := 0
	memOf(t, s).setHook = func(ik string) error {
		if ik == "default/c" {
			calls++
			if calls == 1 {
				return errors.New("forward boom")
			}
		}
		return nil
	}
	res, err := s.SetBundle("default",
		map[string]string{"a": "NA", "b": "NB", "c": "NC"}, WithOverwrite())
	if err == nil {
		t.Fatal("expected mid-bundle failure")
	}
	eqStrings(t, "Restored", res.Restored, []string{"a", "b", "c"})
	if res.Absent != nil || res.Written != nil {
		t.Fatalf("pure-restore rollback: Absent/Written must be nil: %+v", res)
	}
	for k, want := range map[string]string{"a": "OA", "b": "OB", "c": "OC"} {
		if v, _ := s.Get("default", k); v != want {
			t.Fatalf("Get(%q) = %q, want prior %q", k, v, want)
		}
	}
}

func TestSetBundleErrExistsRacerNotTouched(t *testing.T) {
	s := openMem(t)
	// Simulate a racer that wrote "b" between our scan and our write.
	memOf(t, s).setHook = func(ik string) error {
		if ik == "default/b" {
			return ErrExists
		}
		return nil
	}
	res, err := s.SetBundle("default", map[string]string{"a": "1", "b": "2", "c": "3"})
	if !errors.Is(err, ErrExists) || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("err = %v, want ErrExists naming b", err)
	}
	// b is the racer's — must not be deleted/restored; only our own
	// write (a) is rolled back. b and the never-attempted c are Untouched.
	eqStrings(t, "Absent", res.Absent, []string{"a"})
	eqStrings(t, "Untouched", res.Untouched, []string{"b", "c"})
	if res.Written != nil {
		t.Fatalf("Written must be nil: %+v", res)
	}
}

func TestSetBundleRollbackFailureSurfaced(t *testing.T) {
	s := openMem(t)
	mb := memOf(t, s)
	mb.setHook = func(ik string) error {
		if ik == "default/b" {
			return errors.New("write boom")
		}
		return nil
	}
	mb.deleteHook = func(ik string) error {
		if ik == "default/a" {
			return errors.New("delete boom")
		}
		return nil
	}
	res, err := s.SetBundle("default", map[string]string{"a": "1", "b": "2"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `write failed at "b"`) ||
		!strings.Contains(msg, "rollback also failed for a") ||
		!strings.Contains(msg, "keyring may be inconsistent") {
		t.Fatalf("error must surface rollback failure + affected key: %v", err)
	}
	// b was never written (set failed) so it rolls back via delete →
	// ErrNotFound tolerated → reported Absent; a's rollback failed.
	eqStrings(t, "Absent", res.Absent, []string{"b"})
	if res.Restored != nil || res.Untouched != nil {
		t.Fatalf("only Absent should be populated here: %+v", res)
	}
	// The advertised inconsistency: a was written then its rollback
	// delete failed, so a is leaked — it must still be present.
	if ok, _ := s.Exists("default", "a"); !ok {
		t.Fatal("key 'a' should remain present after its rollback delete failed (the surfaced inconsistency)")
	}
}

func TestSetBundleAllowlistEnforced(t *testing.T) {
	s := openMem(t, "a", "b")

	// Rejection path: a disallowed sibling blocks the whole bundle.
	res, err := s.SetBundle("default", map[string]string{"a": "1", "x": "2"})
	if !errors.Is(err, ErrKeyNotAllowed) {
		t.Fatalf("err = %v, want ErrKeyNotAllowed", err)
	}
	if res.Written != nil {
		t.Fatalf("nothing written when a key is disallowed: %+v", res)
	}
	if ok, _ := s.Exists("default", "a"); ok {
		t.Fatal("no key may be written when a sibling is disallowed (validation precedes all writes)")
	}

	// Acceptance path: all keys allowed → success (guards against a bug
	// that gates writes whenever an allowlist is configured).
	res, err = s.SetBundle("default", map[string]string{"a": "1", "b": "2"})
	if err != nil {
		t.Fatalf("SetBundle with all-allowed keys: %v", err)
	}
	eqStrings(t, "Written", res.Written, []string{"a", "b"})
}

func TestBundleOpsClosed(t *testing.T) {
	s, _ := Open("svc", &Options{Backend: BackendMemory})
	_ = s.Close()
	if _, err := s.DeleteBundle("default"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("DeleteBundle after Close = %v, want ErrStoreClosed", err)
	}
	if _, err := s.SetBundle("default", map[string]string{"a": "1"}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("SetBundle after Close = %v, want ErrStoreClosed", err)
	}
}

func mustSet(t *testing.T, s *Store, profile, key, val string) {
	t.Helper()
	if err := s.Set(profile, key, val); err != nil {
		t.Fatalf("Set(%q,%q): %v", profile, key, err)
	}
}

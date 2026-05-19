package cache_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/open-cli-collective/cli-common/cache"
)

type payload struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestRoundTrip(t *testing.T) {
	loc := cache.Locator{Root: t.TempDir(), InstanceKey: "monit.atlassian.net"}

	in := payload{Name: "fields", N: 7}
	if err := cache.WriteResource(loc, "fields", "1h", in); err != nil {
		t.Fatalf("WriteResource: %v", err)
	}

	env, err := cache.ReadResource[payload](loc, "fields")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if env.Data != in {
		t.Fatalf("Data = %+v, want %+v", env.Data, in)
	}
	if env.Resource != "fields" || env.Instance != "monit.atlassian.net" {
		t.Fatalf("Resource/Instance = %q/%q", env.Resource, env.Instance)
	}
	if env.Version != cache.Version || env.TTL != "1h" {
		t.Fatalf("Version/TTL = %d/%q", env.Version, env.TTL)
	}
	if env.FetchedAt.IsZero() || env.FetchedAt.Location() != time.UTC {
		t.Fatalf("FetchedAt = %v, want non-zero UTC", env.FetchedAt)
	}
}

func TestReadMissing(t *testing.T) {
	loc := cache.Locator{Root: t.TempDir(), InstanceKey: "i"}
	if _, err := cache.ReadResource[payload](loc, "absent"); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("err = %v, want ErrCacheMiss", err)
	}
}

func TestVersionMismatchIsMiss(t *testing.T) {
	root := t.TempDir()
	loc := cache.Locator{Root: root, InstanceKey: "i"}
	dir := filepath.Join(root, "i")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A different schema version on disk must read as a miss (self-healing).
	raw := `{"resource":"r","instance":"i","fetched_at":"2026-01-01T00:00:00Z","ttl":"1h","version":999,"data":{"name":"x","n":1}}`
	if err := os.WriteFile(filepath.Join(dir, "r.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadResource[payload](loc, "r"); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("err = %v, want ErrCacheMiss", err)
	}
}

func TestMalformedJSONIsError(t *testing.T) {
	root := t.TempDir()
	loc := cache.Locator{Root: root, InstanceKey: "i"}
	dir := filepath.Join(root, "i")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := cache.ReadResource[payload](loc, "r")
	if err == nil || errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("err = %v, want a non-miss decode error", err)
	}
}

func TestAtomicWrite_NoTempLeak_Perms(t *testing.T) {
	root := t.TempDir()
	loc := cache.Locator{Root: root, InstanceKey: "inst"}
	if err := cache.WriteResource(loc, "res", "1h", payload{Name: "a", N: 1}); err != nil {
		t.Fatalf("WriteResource: %v", err)
	}
	dir := filepath.Join(root, "inst")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, "res.json"))
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("envelope mode = %o, want 0600", perm)
		}
		di, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := di.Mode().Perm(); perm != 0o700 {
			t.Fatalf("cache dir mode = %o, want 0700", perm)
		}
	}
}

func TestInvalidRoot_CreatesNothing(t *testing.T) {
	// Run inside an empty temp cwd so we can prove a relative/empty root
	// never writes relative to the process working directory.
	cwd := t.TempDir()
	t.Chdir(cwd)

	for _, root := range []string{"", "foo/bar", "relative"} {
		loc := cache.Locator{Root: root, InstanceKey: "i"}
		if err := cache.WriteResource(loc, "r", "1h", payload{}); !errors.Is(err, cache.ErrInvalidRoot) {
			t.Fatalf("root=%q WriteResource err = %v, want ErrInvalidRoot", root, err)
		}
		if _, err := cache.ReadResource[payload](loc, "r"); !errors.Is(err, cache.ErrInvalidRoot) {
			t.Fatalf("root=%q ReadResource err = %v, want ErrInvalidRoot", root, err)
		}
	}

	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cwd not untouched, contains: %v", entries)
	}
}

func TestUnsafeComponents(t *testing.T) {
	root := t.TempDir()
	bad := []string{"", "..", "a/b", "../escape", ".hidden..x"}
	for _, v := range bad {
		t.Run("instance="+v, func(t *testing.T) {
			loc := cache.Locator{Root: root, InstanceKey: v}
			if err := cache.WriteResource(loc, "r", "1h", payload{}); !errors.Is(err, cache.ErrInvalidName) {
				t.Fatalf("instanceKey=%q err = %v, want ErrInvalidName", v, err)
			}
		})
		t.Run("name="+v, func(t *testing.T) {
			loc := cache.Locator{Root: root, InstanceKey: "ok"}
			if err := cache.WriteResource(loc, v, "1h", payload{}); !errors.Is(err, cache.ErrInvalidName) {
				t.Fatalf("name=%q err = %v, want ErrInvalidName", v, err)
			}
		})
	}
}

func TestClassify_OnlyFreshStaleManual(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		fetchedAt time.Time
		ttl       string
		want      cache.Status
	}{
		{"fresh", now.Add(-30 * time.Minute), "1h", cache.StatusFresh},
		{"elapsed-stale", now.Add(-2 * time.Hour), "1h", cache.StatusStale},
		{"zero-time-stale", time.Time{}, "1h", cache.StatusStale},
		{"manual-sentinel", now, "manual", cache.StatusManual},
		{"unparseable-ttl-stale", now, "not-a-duration", cache.StatusStale},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.Classify(tt.fetchedAt, tt.ttl, now)
			if got != tt.want {
				t.Fatalf("Classify = %v, want %v", got, tt.want)
			}
			if got == cache.StatusUninitialized || got == cache.StatusUnavailable {
				t.Fatalf("Classify must never return %v", got)
			}
		})
	}
}

func TestStatusString(t *testing.T) {
	cases := map[cache.Status]string{
		cache.StatusUninitialized: "uninitialized",
		cache.StatusFresh:         "fresh",
		cache.StatusStale:         "stale",
		cache.StatusManual:        "manual",
		cache.StatusUnavailable:   "unavailable",
		cache.Status(99):          "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Fatalf("Status(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		fetchedAt time.Time
		want      string
	}{
		{"zero", time.Time{}, "-"},
		{"negative-clamped", now.Add(time.Hour), "0s"},
		{"seconds", now.Add(-45 * time.Second), "45s"},
		{"minutes", now.Add(-5 * time.Minute), "5m"},
		{"hours", now.Add(-3 * time.Hour), "3h"},
		{"days", now.Add(-50 * time.Hour), "2d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cache.Age(tt.fetchedAt, now); got != tt.want {
				t.Fatalf("Age = %q, want %q", got, tt.want)
			}
		})
	}
}

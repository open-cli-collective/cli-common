package statedir_test

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-cli-collective/cli-common/statedir"
	"github.com/open-cli-collective/cli-common/statedirtest"
)

func TestScopeConfigDir_NoCreate(t *testing.T) {
	statedirtest.Hermetic(t)

	got, err := statedir.Scope{Name: "atlassian-cli"}.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	want := filepath.Join(base, "atlassian-cli")
	if got != want {
		t.Fatalf("ConfigDir = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("ConfigDir must not create the directory; stat err = %v", err)
	}
}

func TestScopeConfigDirEnsured_Creates0700(t *testing.T) {
	statedirtest.Hermetic(t)

	scope := statedir.Scope{Name: "slck"}
	want, err := scope.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	dir, err := scope.ConfigDirEnsured()
	if err != nil {
		t.Fatalf("ConfigDirEnsured: %v", err)
	}
	if dir != want {
		t.Fatalf("ConfigDirEnsured = %q, want resolved ConfigDir %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat created dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", dir)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("created config dir mode = %o, want 0700", perm)
		}
	}
}

func TestCacheDirEnsured_Creates0700(t *testing.T) {
	statedirtest.Hermetic(t)

	cache := statedir.Cache{Tool: "nrq"}
	want, err := cache.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	dir, err := cache.CacheDirEnsured()
	if err != nil {
		t.Fatalf("CacheDirEnsured: %v", err)
	}
	if dir != want {
		t.Fatalf("CacheDirEnsured = %q, want resolved CacheDir %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat created dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", dir)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("created cache dir mode = %o, want 0700", perm)
		}
	}
}

func TestDataDir_NoCreate(t *testing.T) {
	statedirtest.Hermetic(t)

	got, err := statedir.Data{Tool: "cr"}.DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	want := currentPlatformDataDir(t, "cr")
	if got != want {
		t.Fatalf("DataDir = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("DataDir must not create the directory; stat err = %v", err)
	}
}

func TestDataDirEnsured_Creates0700(t *testing.T) {
	statedirtest.Hermetic(t)

	data := statedir.Data{Tool: "cr"}
	want, err := data.DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	dir, err := data.DataDirEnsured()
	if err != nil {
		t.Fatalf("DataDirEnsured: %v", err)
	}
	if dir != want {
		t.Fatalf("DataDirEnsured = %q, want resolved DataDir %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat created dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", dir)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("created data dir mode = %o, want 0700", perm)
		}
	}
}

func TestCacheDir_PerBinary(t *testing.T) {
	statedirtest.Hermetic(t)

	jtk, err := statedir.Cache{Tool: "jtk"}.CacheDir()
	if err != nil {
		t.Fatalf("jtk CacheDir: %v", err)
	}
	cfl, err := statedir.Cache{Tool: "cfl"}.CacheDir()
	if err != nil {
		t.Fatalf("cfl CacheDir: %v", err)
	}
	if jtk == cfl {
		t.Fatalf("per-binary cache dirs must differ: both = %q", jtk)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir: %v", err)
	}
	if want := filepath.Join(base, "jtk"); jtk != want {
		t.Fatalf("jtk CacheDir = %q, want %q", jtk, want)
	}
	if _, err := os.Stat(jtk); !os.IsNotExist(err) {
		t.Fatalf("CacheDir must not create the directory; stat err = %v", err)
	}
}

func TestDataDir_PerBinary(t *testing.T) {
	statedirtest.Hermetic(t)

	jtk, err := statedir.Data{Tool: "jtk"}.DataDir()
	if err != nil {
		t.Fatalf("jtk DataDir: %v", err)
	}
	cfl, err := statedir.Data{Tool: "cfl"}.DataDir()
	if err != nil {
		t.Fatalf("cfl DataDir: %v", err)
	}
	if jtk == cfl {
		t.Fatalf("per-binary data dirs must differ: both = %q", jtk)
	}
	if want := currentPlatformDataDir(t, "jtk"); jtk != want {
		t.Fatalf("jtk DataDir = %q, want %q", jtk, want)
	}
	if _, err := os.Stat(jtk); !os.IsNotExist(err) {
		t.Fatalf("DataDir must not create the directory; stat err = %v", err)
	}
}

func TestHermeticPillarsDoNotCollide(t *testing.T) {
	statedirtest.Hermetic(t)

	configDir, err := statedir.Scope{Name: "codereview"}.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	cacheDir, err := statedir.Cache{Tool: "codereview"}.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	dataDir, err := statedir.Data{Tool: "codereview"}.DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}

	dirs := map[string]string{
		"config": configDir,
		"cache":  cacheDir,
		"data":   dataDir,
	}
	for aName, aDir := range dirs {
		for bName, bDir := range dirs {
			if aName >= bName {
				continue
			}
			if aDir == bDir {
				t.Fatalf("%s and %s dirs collide at %q", aName, bName, aDir)
			}
		}
		if _, err := os.Stat(aDir); !os.IsNotExist(err) {
			t.Fatalf("%s dir resolution must not create %q; stat err = %v", aName, aDir, err)
		}
	}
}

func TestEnsuredDirsPropagateMkdirFailure(t *testing.T) {
	tests := []struct {
		name    string
		resolve func() (string, error)
		ensure  func() (string, error)
		wantErr string
	}{
		{
			name:    "config",
			resolve: statedir.Scope{Name: "blocked-config"}.ConfigDir,
			ensure:  statedir.Scope{Name: "blocked-config"}.ConfigDirEnsured,
			wantErr: "creating config dir",
		},
		{
			name:    "cache",
			resolve: statedir.Cache{Tool: "blocked-cache"}.CacheDir,
			ensure:  statedir.Cache{Tool: "blocked-cache"}.CacheDirEnsured,
			wantErr: "creating cache dir",
		},
		{
			name:    "data",
			resolve: statedir.Data{Tool: "blocked-data"}.DataDir,
			ensure:  statedir.Data{Tool: "blocked-data"}.DataDirEnsured,
			wantErr: "creating data dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statedirtest.Hermetic(t)

			dir, err := tt.resolve()
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
				t.Fatalf("create parent: %v", err)
			}
			if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
				t.Fatalf("create blocking file: %v", err)
			}

			got, err := tt.ensure()
			if err == nil {
				t.Fatalf("ensure returned nil error and path %q, want %q error", got, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ensure error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestInvalidNames(t *testing.T) {
	statedirtest.Hermetic(t)

	bad := []string{"", ".", "..", "a/b", `a\b`, "a..b", "../escape", "trailingdot.", string(os.PathSeparator)}
	for _, name := range bad {
		t.Run("scope="+name, func(t *testing.T) {
			if _, err := (statedir.Scope{Name: name}).ConfigDir(); !errors.Is(err, statedir.ErrInvalidName) {
				t.Fatalf("Scope{%q}.ConfigDir() err = %v, want ErrInvalidName", name, err)
			}
			if _, err := (statedir.Cache{Tool: name}).CacheDir(); !errors.Is(err, statedir.ErrInvalidName) {
				t.Fatalf("Cache{%q}.CacheDir() err = %v, want ErrInvalidName", name, err)
			}
			if _, err := (statedir.Data{Tool: name}).DataDir(); !errors.Is(err, statedir.ErrInvalidName) {
				t.Fatalf("Data{%q}.DataDir() err = %v, want ErrInvalidName", name, err)
			}
		})
	}
}

func currentPlatformDataDir(t *testing.T, tool string) string {
	t.Helper()
	// This external test helper mirrors dataDirFor's platform policy; update it
	// whenever the resolver's platform-specific data roots change.
	switch runtime.GOOS {
	case "linux":
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatalf("os.UserHomeDir: %v", err)
			}
			if home == "" {
				t.Fatalf("os.UserHomeDir returned empty home")
			}
			stateHome = filepath.Join(home, ".local", "state")
		} else if !path.IsAbs(stateHome) {
			t.Fatalf("XDG_STATE_HOME %q is relative", stateHome)
		}
		return filepath.Join(stateHome, tool)
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("os.UserHomeDir: %v", err)
		}
		if home == "" {
			t.Fatalf("os.UserHomeDir returned empty home")
		}
		return filepath.Join(home, "Library", "Application Support", tool, "data")
	case "windows":
		localAppData := os.Getenv("LocalAppData")
		if localAppData == "" {
			localAppData = os.Getenv("LOCALAPPDATA")
		}
		if localAppData == "" {
			t.Fatalf("LocalAppData is empty")
		}
		return filepath.Join(localAppData, tool, "data")
	default:
		t.Fatalf("unsupported GOOS %q", runtime.GOOS)
		return ""
	}
}

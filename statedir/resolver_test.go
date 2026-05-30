package statedir_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

	dir, err := statedir.Scope{Name: "slck"}.ConfigDirEnsured()
	if err != nil {
		t.Fatalf("ConfigDirEnsured: %v", err)
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

	dir, err := statedir.Cache{Tool: "nrq"}.CacheDirEnsured()
	if err != nil {
		t.Fatalf("CacheDirEnsured: %v", err)
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

	dir, err := statedir.Data{Tool: "cr"}.DataDirEnsured()
	if err != nil {
		t.Fatalf("DataDirEnsured: %v", err)
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
	switch runtime.GOOS {
	case "linux":
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatalf("os.UserHomeDir: %v", err)
			}
			stateHome = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(stateHome, tool)
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("os.UserHomeDir: %v", err)
		}
		return filepath.Join(home, "Library", "Application Support", tool, "data")
	case "windows":
		localAppData := os.Getenv("LocalAppData")
		if localAppData == "" {
			localAppData = os.Getenv("LOCALAPPDATA")
		}
		return filepath.Join(localAppData, tool, "data")
	default:
		t.Fatalf("unsupported GOOS %q", runtime.GOOS)
		return ""
	}
}

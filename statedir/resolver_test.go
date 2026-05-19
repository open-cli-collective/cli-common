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

func TestInvalidNames(t *testing.T) {
	statedirtest.Hermetic(t)

	bad := []string{"", ".", "..", "a/b", "a..b", "../escape", string(os.PathSeparator)}
	for _, name := range bad {
		t.Run("scope="+name, func(t *testing.T) {
			if _, err := (statedir.Scope{Name: name}).ConfigDir(); !errors.Is(err, statedir.ErrInvalidName) {
				t.Fatalf("Scope{%q}.ConfigDir() err = %v, want ErrInvalidName", name, err)
			}
			if _, err := (statedir.Cache{Tool: name}).CacheDir(); !errors.Is(err, statedir.ErrInvalidName) {
				t.Fatalf("Cache{%q}.CacheDir() err = %v, want ErrInvalidName", name, err)
			}
		})
	}
}

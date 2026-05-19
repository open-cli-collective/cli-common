package statedirtest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-cli-collective/cli-common/statedirtest"
)

func TestHermetic_IsolatesAll7Vars(t *testing.T) {
	root := statedirtest.Hermetic(t)

	vars := []string{
		"HOME", "USERPROFILE", "AppData", "LocalAppData",
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME",
	}
	for _, v := range vars {
		got := os.Getenv(v)
		if got == "" {
			t.Errorf("%s not set by Hermetic", v)
			continue
		}
		if !strings.HasPrefix(got, root) {
			t.Errorf("%s = %q, want a path under temp root %q", v, got, root)
		}
	}
}

func TestHermetic_OSHelpersResolveUnderTemp(t *testing.T) {
	root := statedirtest.Hermetic(t)

	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	if !strings.HasPrefix(cfg, root) {
		t.Fatalf("os.UserConfigDir() = %q, want under temp root %q", cfg, root)
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir: %v", err)
	}
	if !strings.HasPrefix(cache, root) {
		t.Fatalf("os.UserCacheDir() = %q, want under temp root %q", cache, root)
	}
}

func TestHermetic_HomeAndUserprofileShareLogicalHome(t *testing.T) {
	statedirtest.Hermetic(t)

	home := os.Getenv("HOME")
	if up := os.Getenv("USERPROFILE"); up != home {
		t.Fatalf("USERPROFILE = %q, want it to equal HOME %q", up, home)
	}
	if filepath.Base(home) != "home" {
		t.Fatalf("HOME = %q, want its base to be %q", home, "home")
	}
}

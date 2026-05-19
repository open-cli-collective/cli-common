package cache

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// ErrInvalidRoot is returned when Locator.Root is empty or not absolute. A
	// zero-value Locator would otherwise filepath.Join("", key, name) and
	// write relative to the process working directory.
	ErrInvalidRoot = errors.New("cache: locator root must be a non-empty absolute path")
	// ErrInvalidName is returned when an instance key or resource name is
	// unsafe as a path component.
	ErrInvalidName = errors.New("cache: unsafe path component")
)

// safeComponent bounds instance keys and resource names to the subset that is
// safe to compose into a filesystem path: letters, digits, dot, hyphen,
// starting alphanumeric. Path separators, whitespace, and control characters
// are rejected rather than trusted (the values are caller-supplied — e.g. a
// hostname derived from config).
var safeComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.\-]*$`)

func validComponent(s string) bool {
	return safeComponent.MatchString(s) && !strings.Contains(s, "..")
}

// Locator is the injected cache location. The cache library is
// directory-agnostic: Root is supplied by the caller (typically
// statedir.Cache.CacheDir / CacheDirEnsured), never derived here.
type Locator struct {
	// Root is the non-empty absolute cache root for one tool.
	Root string
	// InstanceKey is the per-instance subdir (jtk: hostname / cloud-id;
	// gro: a constant; a single-instance CLI: "default").
	InstanceKey string
}

// resourceFile validates all three path inputs, then composes
// <Root>/<InstanceKey>/<name>.json. Any invalid input returns a typed error
// and never composes or creates a path.
func (l Locator) resourceFile(name string) (string, error) {
	if l.Root == "" || !filepath.IsAbs(l.Root) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRoot, l.Root)
	}
	if !validComponent(l.InstanceKey) {
		return "", fmt.Errorf("%w: instance key %q", ErrInvalidName, l.InstanceKey)
	}
	if !validComponent(name) {
		return "", fmt.Errorf("%w: resource name %q", ErrInvalidName, name)
	}
	return filepath.Join(l.Root, l.InstanceKey, name+".json"), nil
}

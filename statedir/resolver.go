// Package statedir is the shared path/dir resolver for non-secret on-disk
// state (working-with-state.md §5a). It owns the genuinely-common policy that
// is easy to get subtly wrong per-CLI: the credential-scope config-dir naming
// rule (§3), the per-binary cache-dir rule (§4.1), and the create-vs-no-create
// split. It is deliberately NOT a blanket "no file may call os.User*Dir()"
// ban — a CLI's bespoke legacy-source probing legitimately computes its own
// paths; that stays per-CLI (see LegacySource).
//
// Resolution is always os.UserConfigDir()/os.UserCacheDir() + the scope/tool
// name. No hand-rolled ~/.config and no %APPDATA% branch: the stdlib helpers
// honor $XDG_* on Linux and return the OS-native dir on macOS/Windows — that
// is the standard. A relative $XDG_* yields the stdlib error unchanged (the
// §1.1 intentional tightening).
package statedir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dirPerm is the §3 directory permission for config and cache dirs.
const dirPerm = 0o700

// ErrInvalidName is returned when a scope or tool name is unusable as a single
// path component (empty, ".", "..", or containing a path separator). The name
// is composed into a filesystem path, so it is validated rather than trusted.
var ErrInvalidName = errors.New("statedir: invalid scope/tool name")

// validateComponent rejects any value that is not safe as a single path
// component. Scope/tool names in this family are short slugs
// ("slck", "nrq", "atlassian-cli", ...) — there is no legitimate reason for
// one to contain a separator or a "..".
func validateComponent(kind, v string) error {
	switch {
	case v == "":
		return fmt.Errorf("%w: %s is empty", ErrInvalidName, kind)
	case v == "." || v == "..":
		return fmt.Errorf("%w: %s is %q", ErrInvalidName, kind, v)
	case strings.ContainsAny(v, `/\`):
		// Reject BOTH separators on every OS so the "single path component"
		// contract is platform-independent (a name valid on Linux must not
		// become a traversal on Windows).
		return fmt.Errorf("%w: %s %q contains a path separator", ErrInvalidName, kind, v)
	case strings.Contains(v, ".."):
		return fmt.Errorf("%w: %s %q contains %q", ErrInvalidName, kind, v, "..")
	}
	return nil
}

// Scope is the config-dir naming key (§3): the credential scope, not
// necessarily the binary. A single-binary CLI uses its tool name; a
// shared-credential repo uses the shared scope (atlassian-cli ⇒ one config
// dir, one config.yml, one keyring bundle).
type Scope struct {
	Name string
}

// ConfigDir resolves the config directory WITHOUT creating it. Side-effect
// free for dry-run / `config clear --all` / resolve-before-migrate paths.
func (s Scope) ConfigDir() (string, error) {
	if err := validateComponent("scope name", s.Name); err != nil {
		return "", err
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("statedir: resolving user config dir: %w", err)
	}
	return filepath.Join(base, s.Name), nil
}

// ConfigDirEnsured is ConfigDir plus os.MkdirAll(dir, 0700). It does NOT
// re-chmod a pre-existing wrong-mode dir — MkdirAll only sets the mode on
// components it creates. Hardening an already-present mis-moded dir is
// per-port work (§6.4), not a commons concern.
func (s Scope) ConfigDirEnsured() (string, error) {
	dir, err := s.ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("statedir: creating config dir: %w", err)
	}
	return dir, nil
}

// Cache is the cache-dir naming key (§4.1): always the binary/tool name, even
// inside a shared-credential repo — jtk and cfl cache different domains and
// never share a cache dir.
type Cache struct {
	Tool string
}

// CacheDir resolves the cache directory WITHOUT creating it.
func (c Cache) CacheDir() (string, error) {
	if err := validateComponent("tool name", c.Tool); err != nil {
		return "", err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("statedir: resolving user cache dir: %w", err)
	}
	return filepath.Join(base, c.Tool), nil
}

// CacheDirEnsured is CacheDir plus os.MkdirAll(dir, 0700). Same no-re-chmod
// rule as ConfigDirEnsured.
func (c Cache) CacheDirEnsured() (string, error) {
	dir, err := c.CacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("statedir: creating cache dir: %w", err)
	}
	return dir, nil
}

// LegacySource is the migration-source enumeration seam (§5a). The resolver
// never enumerates, reads, or interprets these: each CLI computes its own
// legacy probe paths and decides copy/move/conflict policy per-port (§3.2).
// This type exists only so the shape and intent are shared and documented; it
// deliberately carries no behavior. A Migrate(...) orchestrator is explicitly
// NOT provided here — that would pre-decide per-port §3.2 policy without a
// consumer.
type LegacySource struct {
	// Label is a human-readable name for conflict / one-line-notice messages
	// (e.g. "legacy ~/.config/cfl"). Never a value, never a secret.
	Label string
	// Path is the absolute path the CLI computed itself.
	Path string
}

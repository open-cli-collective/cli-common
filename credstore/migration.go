package credstore

// This file implements the package-level migration helpers per the Open
// CLI Collective Secret-Handling Standard §2.1 (line 412) and §1.8
// (migration from legacy plaintext config). credstore owns only the
// *formats* and the conflict *error*: the one-line stderr signal, the
// machine-readable _migration JSON object, and the legacy-vs-keyring
// conflict error. The migration mechanics (reading legacy config, moving
// the secret into the keyring via a Store, rewriting the file) and
// conflict *detection* (comparing the two values) stay per-CLI — this
// package never sees a secret value, which is also why the conflict
// error is leak-proof by construction (§1.8 line 254 / §1.12).

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// MigrationSchemaVersion is the _migration object's schema version
// (§1.8). Bumped only on a breaking change to the JSON shape.
const MigrationSchemaVersion = 1

// MigrationFieldName is the top-level JSON key under which the migration
// signal is emitted (§1.8).
const MigrationFieldName = "_migration"

// formatMigrationLine renders the one-time human stderr signal exactly as
// §1.8 (line 251) specifies, without a trailing newline so it composes.
// Unexported: not public API beyond §2.1; EmitMigrationStderr is the
// supported entry point. White-box tests call this directly.
func formatMigrationLine(field, ref string) string {
	return fmt.Sprintf("migrated %s to keyring at %s; this is a one-time operation", field, ref)
}

// emitMigration writes the one-time migration line to w. Unexported
// writer seam so tests inject a buffer instead of mutating the global
// os.Stderr file descriptor (consistent with the package's pure/impure
// seam convention, e.g. formatMigrationLine vs the public entry point).
func emitMigration(w io.Writer, field, ref string) {
	// Best-effort diagnostic line (like the stdlib fmt.Println family): a
	// failed stderr write has no actionable recovery and must not change
	// the standard-mandated EmitMigrationStderr(field, ref) signature.
	_, _ = fmt.Fprintln(w, formatMigrationLine(field, ref))
}

// EmitMigrationStderr prints the one-time migration signal to stderr
// (§1.8). A CLI calls this on the run where it moved a legacy plaintext
// field into the keyring. field is the legacy config field name; ref is
// the credential ref it now lives under. Never include a secret value in
// either argument — by contract these are descriptive identifiers only.
func EmitMigrationStderr(field, ref string) {
	emitMigration(os.Stderr, field, ref)
}

// MigrationChange is one entry in the _migration signal: which legacy
// field moved, and the descriptive opaque from/to locations. from and to
// are location descriptors (e.g. "config:legacy_plaintext",
// "keyring:atlassian-cli/default/api_token") — never the value (§1.8).
type MigrationChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// MigrationBlock is the value of the _migration field: a schema version
// plus the list of changes that occurred on this run. A CLI that embeds
// the signal as a field of its own response type tags that field
// `json:"_migration"` with this as the value.
type MigrationBlock struct {
	Version int               `json:"version"`
	Changes []MigrationChange `json:"changes"`
}

// MigrationObject is the standalone §1.8 object — it marshals to exactly
// {"_migration":{"version":1,"changes":[...]}}. For CLIs that merge
// objects into their JSON response rather than embedding a struct field.
type MigrationObject struct {
	Migration MigrationBlock `json:"_migration"`
}

// MigrationJSONEntry constructs one MigrationChange (the §2.1-named
// helper). field is the legacy config field; from/to are opaque location
// descriptors — never the secret value.
func MigrationJSONEntry(field, from, to string) MigrationChange {
	return MigrationChange{Field: field, From: from, To: to}
}

// NewMigrationBlock builds the _migration value with the current schema
// version and a non-nil Changes slice (so it marshals "changes":[],
// never null, on the degenerate empty call).
func NewMigrationBlock(changes ...MigrationChange) MigrationBlock {
	if changes == nil {
		changes = []MigrationChange{}
	}
	return MigrationBlock{Version: MigrationSchemaVersion, Changes: changes}
}

// NewMigrationObject builds the standalone {"_migration":{...}} object.
func NewMigrationObject(changes ...MigrationChange) MigrationObject {
	return MigrationObject{Migration: NewMigrationBlock(changes...)}
}

// ErrMigrationConflict is the stable identity of the error
// MigrationConflictError returns. errors.Is(err, ErrMigrationConflict)
// holds regardless of the message text, mirroring the PR6
// ErrSecretLeaked/leakError pattern.
var ErrMigrationConflict = errors.New("credstore: legacy plaintext value conflicts with existing keyring value")

// migrationConflictError carries the actionable message while reporting
// the stable ErrMigrationConflict identity to errors.Is.
type migrationConflictError struct{ msg string }

func (e *migrationConflictError) Error() string { return e.msg }

func (e *migrationConflictError) Is(target error) bool { return target == ErrMigrationConflict }

// MigrationConflictError builds the §1.8 (line 254) conflict error: the
// legacy plaintext value differs from the value already in the keyring,
// so the CLI must not silently pick a winner. The message names both
// locations, states that the values differ, and offers the three
// remediation options. It is leak-proof by construction: it takes no
// value argument, so it cannot print either value, masked or unmasked
// (§1.8 line 254 / §1.12).
//
// cli is the tool name (for the `<cli> config clear` remediation); field
// is the conflicting legacy field; legacyLocation is a human description
// of where the plaintext lives (e.g. the config file path); ref is the
// keyring ref holding the existing value.
func MigrationConflictError(cli, field, legacyLocation, ref string) error {
	msg := fmt.Sprintf(
		"credstore: %s: the legacy plaintext value at %s differs from the existing keyring value at %s; "+
			"refusing to silently pick a winner. Resolve with one of:\n"+
			"  - run `%s config clear` then re-run (keeps the legacy plaintext value, removes the keyring entry)\n"+
			"  - manually delete the `%s` field from %s (keeps the keyring value)\n"+
			"  - re-run with --overwrite (forces the legacy plaintext into the keyring, replacing the existing entry)",
		field, legacyLocation, ref, cli, field, legacyLocation)
	return &migrationConflictError{msg: msg}
}

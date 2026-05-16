package credstore

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestFormatMigrationLine(t *testing.T) {
	got := formatMigrationLine("api_token", "atlassian-cli/default")
	want := "migrated api_token to keyring at atlassian-cli/default; this is a one-time operation"
	if got != want {
		t.Fatalf("formatMigrationLine = %q, want %q", got, want)
	}
	// No trailing newline (composable).
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("must not carry a trailing newline: %q", got)
	}
}

func TestEmitMigrationStderr(t *testing.T) {
	// Capture os.Stderr and os.Stdout via pipes to prove the line goes to
	// stderr (with a newline) and stdout is untouched.
	origErr, origOut := os.Stderr, os.Stdout
	er, ew, _ := os.Pipe()
	or, ow, _ := os.Pipe()
	os.Stderr, os.Stdout = ew, ow
	t.Cleanup(func() { os.Stderr, os.Stdout = origErr, origOut })

	EmitMigrationStderr("api_token", "atlassian-cli/default")

	_ = ew.Close()
	_ = ow.Close()
	gotErr, _ := io.ReadAll(er)
	gotOut, _ := io.ReadAll(or)

	want := "migrated api_token to keyring at atlassian-cli/default; this is a one-time operation\n"
	if string(gotErr) != want {
		t.Fatalf("stderr = %q, want %q", gotErr, want)
	}
	if len(gotOut) != 0 {
		t.Fatalf("stdout must be untouched, got %q", gotOut)
	}
}

func TestMigrationJSONShapeByteForByte(t *testing.T) {
	// The §1.8 example, byte-for-byte (struct field order fixes key order).
	obj := NewMigrationObject(MigrationJSONEntry(
		"api_token", "config:legacy_plaintext", "keyring:atlassian-cli/default/api_token"))
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"_migration":{"version":1,"changes":[{"field":"api_token","from":"config:legacy_plaintext","to":"keyring:atlassian-cli/default/api_token"}]}}`
	if string(b) != want {
		t.Fatalf("marshal =\n  %s\nwant\n  %s", b, want)
	}

	// Empty changes → "changes":[] (never null), version still 1.
	be, _ := json.Marshal(NewMigrationObject())
	if string(be) != `{"_migration":{"version":1,"changes":[]}}` {
		t.Fatalf("empty object = %s", be)
	}

	// Multiple changes preserved in order.
	bm, _ := json.Marshal(NewMigrationObject(
		MigrationJSONEntry("a", "config:legacy_plaintext", "keyring:svc/default/a"),
		MigrationJSONEntry("b", "config:legacy_plaintext", "keyring:svc/default/b"),
	))
	if !strings.Contains(string(bm), `"field":"a"`) ||
		strings.Index(string(bm), `"field":"a"`) > strings.Index(string(bm), `"field":"b"`) {
		t.Fatalf("multi-change order wrong: %s", bm)
	}

	// Version constant is what the standard pins.
	if MigrationSchemaVersion != 1 || MigrationFieldName != "_migration" {
		t.Fatalf("schema constants drifted: v=%d field=%q", MigrationSchemaVersion, MigrationFieldName)
	}
}

func TestMigrationObjectRoundTrip(t *testing.T) {
	in := NewMigrationObject(MigrationJSONEntry("tok", "config:legacy_plaintext", "keyring:svc/default/tok"))
	b, _ := json.Marshal(in)
	var out MigrationObject
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Migration.Version != 1 || len(out.Migration.Changes) != 1 ||
		out.Migration.Changes[0] != (MigrationChange{Field: "tok", From: "config:legacy_plaintext", To: "keyring:svc/default/tok"}) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestMigrationConflictError(t *testing.T) {
	const (
		cli = "jtk"
		fld = "api_token"
		loc = "/home/u/.config/jira-ticket-cli/config.yml"
		ref = "atlassian-cli/default"
	)
	err := MigrationConflictError(cli, fld, loc, ref)

	if !errors.Is(err, ErrMigrationConflict) {
		t.Fatalf("must match ErrMigrationConflict sentinel, got %v", err)
	}
	msg := err.Error()

	// Names both locations, states they differ.
	for _, want := range []string{cli, fld, loc, ref, "differs", "credstore:"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q: %q", want, msg)
		}
	}
	// All three remediation options present (§1.8 lines 256-258).
	for _, opt := range []string{"config clear", "manually delete", "--overwrite"} {
		if !strings.Contains(msg, opt) {
			t.Fatalf("message missing remediation %q: %q", opt, msg)
		}
	}
	// Leak-proof by construction: there is no value parameter, so no
	// secret value can appear. Sanity-check that the constructor never
	// echoes anything resembling a credential (only the identifiers we
	// passed are present).
	if strings.Contains(msg, "BEGIN") || strings.Contains(msg, "xoxb-") {
		t.Fatalf("unexpected value-like content: %q", msg)
	}
}

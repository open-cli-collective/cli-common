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
	// Inputs are Sprintf *arguments*, not the format string: a fmt verb
	// in field/ref must appear literally, never be interpreted.
	if g := formatMigrationLine("%s%d", "r%n"); g != "migrated %s%d to keyring at r%n; this is a one-time operation" {
		t.Fatalf("fmt verb in input not literal: %q", g)
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

	// Multiple changes — exact bytes (order + every from/to field pinned,
	// not just a positional substring probe).
	bm, _ := json.Marshal(NewMigrationObject(
		MigrationJSONEntry("a", "config:legacy_plaintext", "keyring:svc/default/a"),
		MigrationJSONEntry("b", "config:legacy_plaintext", "keyring:svc/default/b"),
	))
	wantMulti := `{"_migration":{"version":1,"changes":[` +
		`{"field":"a","from":"config:legacy_plaintext","to":"keyring:svc/default/a"},` +
		`{"field":"b","from":"config:legacy_plaintext","to":"keyring:svc/default/b"}]}}`
	if string(bm) != wantMulti {
		t.Fatalf("multi-change =\n  %s\nwant\n  %s", bm, wantMulti)
	}

	// Version constant is what the standard pins.
	if MigrationSchemaVersion != 1 || MigrationFieldName != "_migration" {
		t.Fatalf("schema constants drifted: v=%d field=%q", MigrationSchemaVersion, MigrationFieldName)
	}
}

func TestMigrationJSONEntryAndBlockDirect(t *testing.T) {
	// MigrationJSONEntry in isolation.
	if e := MigrationJSONEntry("f", "from-loc", "to-loc"); e != (MigrationChange{Field: "f", From: "from-loc", To: "to-loc"}) {
		t.Fatalf("MigrationJSONEntry = %+v", e)
	}

	// NewMigrationBlock called directly: version pinned, Changes non-nil
	// even on the empty call (the changes==nil guard) so it marshals [].
	empty := NewMigrationBlock()
	if empty.Version != 1 || empty.Changes == nil || len(empty.Changes) != 0 {
		t.Fatalf("NewMigrationBlock() = %+v, want version 1 + non-nil empty Changes", empty)
	}
	if b, _ := json.Marshal(empty); string(b) != `{"version":1,"changes":[]}` {
		t.Fatalf("empty block = %s", b)
	}

	// Documented primary embedding mode: MigrationBlock as a caller's own
	// json:"_migration" struct field marshals to the exact §1.8 shape.
	type cliResponse struct {
		Migration MigrationBlock `json:"_migration"`
		Result    string         `json:"result"`
	}
	resp := cliResponse{
		Migration: NewMigrationBlock(MigrationJSONEntry("api_token", "config:legacy_plaintext", "keyring:svc/default/api_token")),
		Result:    "ok",
	}
	b, _ := json.Marshal(resp)
	want := `{"_migration":{"version":1,"changes":[{"field":"api_token","from":"config:legacy_plaintext","to":"keyring:svc/default/api_token"}]},"result":"ok"}`
	if string(b) != want {
		t.Fatalf("embedded field =\n  %s\nwant\n  %s", b, want)
	}
}

func TestMigrationJSONEscaping(t *testing.T) {
	// "byte-for-byte" must survive JSON-special characters in inputs:
	// encoding/json escapes them; the contract still holds.
	b, _ := json.Marshal(NewMigrationObject(
		MigrationJSONEntry(`we"ird\field`, "config:legacy_plaintext", "keyring:svc/默认/tok")))
	want := `{"_migration":{"version":1,"changes":[{"field":"we\"ird\\field","from":"config:legacy_plaintext","to":"keyring:svc/默认/tok"}]}}`
	if string(b) != want {
		t.Fatalf("escaping =\n  %s\nwant\n  %s", b, want)
	}
	var rt MigrationObject
	if err := json.Unmarshal(b, &rt); err != nil || rt.Migration.Changes[0].Field != `we"ird\field` {
		t.Fatalf("escaping round-trip: err=%v got=%q", err, rt.Migration.Changes[0].Field)
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

	// Golden message: exact, spec-format-pinned. This single assertion
	// subsumes "names both locations" + "all three options" AND proves
	// leak-proofness — the message is fully determined by the four
	// non-value identifiers, so nothing else (no value) can appear.
	want := "credstore: api_token: the legacy plaintext value at " +
		"/home/u/.config/jira-ticket-cli/config.yml differs from the existing keyring " +
		"value at atlassian-cli/default; refusing to silently pick a winner. Resolve with one of:\n" +
		"  - run `jtk config clear` then re-run (keeps the legacy plaintext value, removes the keyring entry)\n" +
		"  - manually delete the `api_token` field from /home/u/.config/jira-ticket-cli/config.yml (keeps the keyring value)\n" +
		"  - re-run with --overwrite (forces the legacy plaintext into the keyring, replacing the existing entry)"
	if got := err.Error(); got != want {
		t.Fatalf("conflict message =\n%q\nwant\n%q", got, want)
	}
}

//go:build !cgo && (linux || darwin)

package credstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPassItemPathRejectsEscapes(t *testing.T) {
	for _, itemKey := range []string{
		"",
		"../tok",
		"default/../tok",
		"default/tok/..",
		filepath.Join(string(os.PathSeparator), "tmp", "tok"),
	} {
		if got, err := passItemPath("svc", itemKey); err == nil {
			t.Fatalf("passItemPath(%q) = %q, want error", itemKey, got)
		}
	}
	got, err := passItemPath("svc", "default/tok")
	if err != nil {
		t.Fatalf("passItemPath valid: %v", err)
	}
	if got != filepath.Join("svc", "default", "tok") {
		t.Fatalf("passItemPath valid = %q", got)
	}
}

func TestPassCheckItemExistsPreservesStatErrors(t *testing.T) {
	b := &passKeyringBackend{dir: t.TempDir(), prefix: "svc"}
	if err := b.checkItemExists("default/missing"); !errors.Is(err, errKeyringItemNotFound) {
		t.Fatalf("missing err = %v, want errKeyringItemNotFound", err)
	}
	if err := b.checkItemExists("../escape"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escape err = %v, want escape error", err)
	}
}

func TestDecodePassItemOutputTrimsWhitespace(t *testing.T) {
	got, err := decodePassItemOutput([]byte("\n\t{\"Key\":\"default/tok\",\"Data\":\"dmFsdWU=\"}\n"))
	if err != nil {
		t.Fatalf("decodePassItemOutput: %v", err)
	}
	if got.key != "default/tok" || string(got.data) != "value" {
		t.Fatalf("decoded = (%q,%q), want (default/tok,value)", got.key, got.data)
	}
}

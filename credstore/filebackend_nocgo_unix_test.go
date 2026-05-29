//go:build !cgo && (linux || darwin)

package credstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileBackendKeysIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "default%2Ftok"), []byte(bytenessFileBackendFixture), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "not-a-key"), 0700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	b := &fileKeyringBackend{dir: dir, passwordFunc: fixedStringPrompt("fixture-passphrase")}
	keys, err := b.keys()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	eqStrings(t, "file backend keys", keys, []string{"default/tok"})
}

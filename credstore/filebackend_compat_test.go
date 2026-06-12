package credstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	jose "github.com/dvsekhvalnov/jose2go"
)

const bytenessFileBackendFixture = "eyJhbGciOiJQQkVTMi1IUzI1NitBMTI4S1ciLCJjcmVhdGVkIjoiMjAyNi0wNS0yOSAxODo0Mzo1MC4xMTE2ODYgLTA0MDAgRURUIG09KzAuMDAzNjA1Mzc2IiwiZW5jIjoiQTI1NkdDTSIsInAyYyI6ODE5MiwicDJzIjoiTlpqemVVU3JydkRNcWgzdiJ9.NWIEbaOgN8pWYbOKLhqP2Kvu9y-skzbWVh_CkM-p-QHzyHtk61CnMQ.b1xMiL3Xm-U3K1e5.kCiisrrLjZNwtGKx08CUQZV3-x3OKV2ypBy0LxEqkbI-NEggQKgki43C2rzoG6-Yw-TRoa08RaeDMm7tuYKcSm_XPYSpOE23CobYlSM2rs9p4HKHFZTITckXZWPOVUiDAd-YGKbJ9hIviun7HcYBdxCkkn3JvAjxTcbJMYuDzWzILttIQ-coZoKqSKhsihkYT71-LCA.Ba5n5Fx0HGQ1MS2eZ-JdWQ"

func TestFileBackendReadsByteNessFixture(t *testing.T) {
	const (
		svc        = "credstore-fixture"
		passphrase = "fixture-passphrase"
	)
	base := t.TempDir()
	writeRawFileBackendItem(t, base, svc, "default%2Ftok", bytenessFileBackendFixture)
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("CREDSTORE_FIXTURE_KEYRING_PASSPHRASE", passphrase)

	s, err := Open(svc, &Options{Backend: BackendFile, AllowedKeys: []string{"tok"}})
	if err != nil {
		t.Fatalf("Open file backend: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	got, err := s.Get("default", "tok")
	if err != nil {
		t.Fatalf("Get fixture: %v", err)
	}
	if got != "fixture-value" {
		t.Fatalf("Get fixture = %q, want fixture-value", got)
	}
}

func TestFileBackendCoreWriterReadableThroughOpen(t *testing.T) {
	const (
		svc        = "credstore-corewriter"
		passphrase = "core-passphrase"
	)
	base := t.TempDir()
	token, err := encodeFileKeyringItem(keyringItem{key: "default/tok", data: []byte("core-value")}, passphrase)
	if err != nil {
		t.Fatalf("encodeFileKeyringItem: %v", err)
	}
	writeRawFileBackendItem(t, base, svc, "default%2Ftok", token)
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("CREDSTORE_COREWRITER_KEYRING_PASSPHRASE", passphrase)

	s, err := Open(svc, &Options{Backend: BackendFile, AllowedKeys: []string{"tok"}})
	if err != nil {
		t.Fatalf("Open file backend: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	got, err := s.Get("default", "tok")
	if err != nil {
		t.Fatalf("Get core-written item: %v", err)
	}
	if got != "core-value" {
		t.Fatalf("Get core-written item = %q, want core-value", got)
	}
}

func TestFileBackendEncodingMatchesByteNessShape(t *testing.T) {
	const passphrase = "shape-passphrase"
	if got := fileKeyringFilename("default/tok"); got != "default%2Ftok" {
		t.Fatalf("fileKeyringFilename = %q, want ByteNess percent-escaped default%%2Ftok", got)
	}
	token, err := encodeFileKeyringItem(keyringItem{key: "default/tok", data: []byte("shape-value")}, passphrase)
	if err != nil {
		t.Fatalf("encodeFileKeyringItem: %v", err)
	}
	payload, _, err := jose.Decode(token, passphrase)
	if err != nil {
		t.Fatalf("jose.Decode: %v", err)
	}
	var got struct {
		Key                         string
		Data                        []byte
		Label                       string
		Description                 string
		KeychainNotTrustApplication bool
		KeychainNotSynchronizable   bool
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("json.Unmarshal payload: %v", err)
	}
	if got.Key != "default/tok" || string(got.Data) != "shape-value" {
		t.Fatalf("payload = (%q,%q), want (default/tok,shape-value)", got.Key, got.Data)
	}
	if got.Label != "" || got.Description != "" || got.KeychainNotTrustApplication || got.KeychainNotSynchronizable {
		t.Fatalf("payload non-secret ByteNess fields drifted: %+v", got)
	}
}

func TestFileBackendEncodingPreservesMetadataFields(t *testing.T) {
	passphrase := "metadata-" + "passphrase"
	token, err := encodeFileKeyringItem(keyringItem{
		key:                         "default/tok",
		data:                        []byte("shape-value"),
		label:                       "codereview default/tok",
		description:                 "Credential for codereview default/tok",
		keychainNotTrustApplication: true,
		keychainNotSynchronizable:   true,
	}, passphrase)
	if err != nil {
		t.Fatalf("encodeFileKeyringItem: %v", err)
	}
	payload, _, err := jose.Decode(token, passphrase)
	if err != nil {
		t.Fatalf("jose.Decode: %v", err)
	}
	var got persistedKeyringItem
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("json.Unmarshal payload: %v", err)
	}
	if got.Label != "codereview default/tok" {
		t.Fatalf("Label = %q, want %q", got.Label, "codereview default/tok")
	}
	if got.Description != "Credential for codereview default/tok" {
		t.Fatalf("Description = %q, want %q", got.Description, "Credential for codereview default/tok")
	}
	if !got.KeychainNotTrustApplication {
		t.Fatal("KeychainNotTrustApplication = false, want true")
	}
	if !got.KeychainNotSynchronizable {
		t.Fatal("KeychainNotSynchronizable = false, want true")
	}
}

func writeRawFileBackendItem(t *testing.T, base, service, encodedName, token string) {
	t.Helper()
	dir := filepath.Join(base, service, "keyring")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	name := filepath.Join(dir, encodedName)
	if err := os.WriteFile(name, []byte(token), 0600); err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
}

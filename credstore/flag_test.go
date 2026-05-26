package credstore

import (
	"errors"
	"strings"
	"testing"
)

func TestParseBackend_AllRecognized(t *testing.T) {
	for _, b := range allBackends {
		t.Run(string(b), func(t *testing.T) {
			got, err := ParseBackend(string(b))
			if err != nil {
				t.Fatalf("ParseBackend(%q): unexpected error: %v", b, err)
			}
			if got != b {
				t.Fatalf("ParseBackend(%q) = %q, want %q", b, got, b)
			}
		})
	}
}

func TestParseBackend_Unknown(t *testing.T) {
	_, err := ParseBackend("not-a-backend")
	if err == nil {
		t.Fatal("ParseBackend(\"not-a-backend\"): want error, got nil")
	}
	if !errors.Is(err, ErrBackendNotImplemented) {
		t.Fatalf("ParseBackend unknown: errors.Is(_, ErrBackendNotImplemented) = false, want true; err=%v", err)
	}
	for _, b := range allBackends {
		if !strings.Contains(err.Error(), string(b)) {
			t.Errorf("ParseBackend unknown error must name every valid backend; missing %q in %q", b, err.Error())
		}
	}
}

func TestValidBackendNames_MatchesAllBackends(t *testing.T) {
	names := ValidBackendNames()
	if len(names) != len(allBackends) {
		t.Fatalf("ValidBackendNames len = %d, allBackends len = %d", len(names), len(allBackends))
	}
	for i, b := range allBackends {
		if names[i] != string(b) {
			t.Errorf("ValidBackendNames[%d] = %q, allBackends[%d] = %q", i, names[i], i, b)
		}
	}
	// Mutating the returned slice must not leak into subsequent calls.
	names[0] = "tampered"
	again := ValidBackendNames()
	if again[0] == "tampered" {
		t.Fatal("ValidBackendNames must return a fresh copy on each call")
	}
}

func TestBackendFlagUsage_NamesEveryBackend(t *testing.T) {
	for _, b := range allBackends {
		if !strings.Contains(BackendFlagUsage, string(b)) {
			t.Errorf("BackendFlagUsage missing %q: %q", b, BackendFlagUsage)
		}
	}
	if !strings.Contains(BackendFlagUsage, "Precedence:") {
		t.Errorf("BackendFlagUsage should document precedence: %q", BackendFlagUsage)
	}
	if !strings.Contains(BackendFlagUsage, "<SERVICE>_KEYRING_BACKEND") {
		t.Errorf("BackendFlagUsage should name the env-var equivalent: %q", BackendFlagUsage)
	}
}

func TestBackendEnvVar(t *testing.T) {
	cases := []struct {
		service string
		want    string
	}{
		{"slack-chat-api", "SLACK_CHAT_API_KEYRING_BACKEND"},
		{"atlassian-cli", "ATLASSIAN_CLI_KEYRING_BACKEND"},
		{"gro", "GRO_KEYRING_BACKEND"},
		{"newrelic-cli", "NEWRELIC_CLI_KEYRING_BACKEND"},
	}
	for _, tc := range cases {
		if got := BackendEnvVar(tc.service); got != tc.want {
			t.Errorf("BackendEnvVar(%q) = %q, want %q", tc.service, got, tc.want)
		}
	}
}

func TestBindBackendFlag_FlagValid(t *testing.T) {
	opts := &Options{}
	if err := BindBackendFlag(opts, "file", "secret-service"); err != nil {
		t.Fatalf("BindBackendFlag: unexpected error: %v", err)
	}
	if opts.Backend != BackendFile {
		t.Errorf("opts.Backend = %q, want %q", opts.Backend, BackendFile)
	}
	if opts.ConfigBackend != BackendSecretService {
		t.Errorf("opts.ConfigBackend = %q, want %q", opts.ConfigBackend, BackendSecretService)
	}
}

func TestBindBackendFlag_FlagInvalid(t *testing.T) {
	opts := &Options{Backend: BackendKeychain, ConfigBackend: BackendFile}
	err := BindBackendFlag(opts, "bogus", "secret-service")
	if err == nil {
		t.Fatal("BindBackendFlag(bogus): want error, got nil")
	}
	if !errors.Is(err, ErrBackendNotImplemented) {
		t.Errorf("errors.Is(_, ErrBackendNotImplemented) = false, want true; err=%v", err)
	}
	// opts must be untouched on error.
	if opts.Backend != BackendKeychain {
		t.Errorf("opts.Backend mutated on error: got %q, want %q", opts.Backend, BackendKeychain)
	}
	if opts.ConfigBackend != BackendFile {
		t.Errorf("opts.ConfigBackend mutated on error: got %q, want %q", opts.ConfigBackend, BackendFile)
	}
}

func TestBindBackendFlag_EmptyFlag(t *testing.T) {
	opts := &Options{}
	if err := BindBackendFlag(opts, "", "file"); err != nil {
		t.Fatalf("BindBackendFlag empty flag: unexpected error: %v", err)
	}
	if opts.Backend != "" {
		t.Errorf("opts.Backend = %q, want empty (no flag supplied)", opts.Backend)
	}
	if opts.ConfigBackend != BackendFile {
		t.Errorf("opts.ConfigBackend = %q, want %q", opts.ConfigBackend, BackendFile)
	}
}

func TestBindBackendFlag_ConfigPassthrough(t *testing.T) {
	// Config value is passed through unvalidated at this layer — Open
	// validates it inside selectBackend. A stale/unknown config string
	// surfaces there, not here.
	opts := &Options{}
	if err := BindBackendFlag(opts, "", "not-real-yet"); err != nil {
		t.Fatalf("BindBackendFlag with unknown config: unexpected error at this layer: %v", err)
	}
	if string(opts.ConfigBackend) != "not-real-yet" {
		t.Errorf("opts.ConfigBackend = %q, want verbatim passthrough %q", opts.ConfigBackend, "not-real-yet")
	}
}

func TestBindBackendFlag_NilOpts(t *testing.T) {
	err := BindBackendFlag(nil, "file", "")
	if err == nil {
		t.Fatal("BindBackendFlag(nil, ...): want error, got nil (must not panic)")
	}
	if !strings.Contains(err.Error(), "non-nil") {
		t.Errorf("nil-opts error should mention non-nil requirement: %v", err)
	}
}

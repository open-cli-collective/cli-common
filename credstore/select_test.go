package credstore

import (
	"errors"
	"strings"
	"testing"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestSelectBackendPrecedence(t *testing.T) {
	const svc = "slack-chat-api"
	const envVar = "SLACK_CHAT_API_KEYRING_BACKEND"
	tests := []struct {
		name    string
		opts    *Options
		env     map[string]string
		goos    string
		wantB   Backend
		wantSrc Source
	}{
		{"explicit beats env+config+auto", &Options{Backend: BackendMemory, ConfigBackend: BackendFile}, map[string]string{envVar: "secret-service"}, "linux", BackendMemory, SourceExplicit},
		{"env beats config+auto", &Options{ConfigBackend: BackendFile}, map[string]string{envVar: "secret-service"}, "linux", BackendSecretService, SourceEnv},
		{"config beats auto", &Options{ConfigBackend: BackendFile}, nil, "darwin", BackendFile, SourceConfig},
		{"auto darwin", &Options{}, nil, "darwin", BackendKeychain, SourceAuto},
		{"auto windows", &Options{}, nil, "windows", BackendWinCred, SourceAuto},
		{"auto linux", &Options{}, nil, "linux", BackendSecretService, SourceAuto},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, src, err := selectBackend(svc, tc.opts, envFrom(tc.env), tc.goos)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b != tc.wantB || src != tc.wantSrc {
				t.Fatalf("= (%q,%q), want (%q,%q)", b, src, tc.wantB, tc.wantSrc)
			}
		})
	}
}

func TestSelectBackendEnvVarNameMapping(t *testing.T) {
	// Hyphenated service segment → upper-snake env var (§1.4 example:
	// slack-chat-api → SLACK_CHAT_API_KEYRING_BACKEND).
	b, src, err := selectBackend(
		"slack-chat-api", &Options{},
		envFrom(map[string]string{"SLACK_CHAT_API_KEYRING_BACKEND": "file"}), "linux")
	if err != nil || b != BackendFile || src != SourceEnv {
		t.Fatalf("= (%q,%q,%v), want (file,env,nil)", b, src, err)
	}
}

func TestSelectBackendFailClosed(t *testing.T) {
	const svc = "atlassian-cli"
	const envVar = "ATLASSIAN_CLI_KEYRING_BACKEND"
	tests := []struct {
		name    string
		opts    *Options
		env     map[string]string
		goos    string
		wantMsg string // the offending value the error must name
	}{
		{"bogus explicit", &Options{Backend: Backend("bogus")}, nil, "linux", "bogus"},
		{"bogus env", &Options{}, map[string]string{envVar: "nope"}, "linux", "nope"},
		{"bogus config", &Options{ConfigBackend: Backend("weird")}, nil, "linux", "weird"},
		{"unknown goos", &Options{}, nil, "plan9", "plan9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, src, err := selectBackend(svc, tc.opts, envFrom(tc.env), tc.goos)
			if !errors.Is(err, ErrBackendNotImplemented) {
				t.Fatalf("err = %v, want ErrBackendNotImplemented", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err %q must name offending value %q", err, tc.wantMsg)
			}
			if b != "" || src != "" {
				t.Fatalf("nothing must be selected on failure: got (%q,%q)", b, src)
			}
		})
	}
}

func TestSelectBackendMemoryNeverAuto(t *testing.T) {
	// memory is selectable explicitly/env/config but never by auto
	// (fail-closed: no silent in-memory degradation, §2.1/INT-430).
	if b, src, err := selectBackend("svc", &Options{Backend: BackendMemory}, envFrom(nil), "linux"); err != nil || b != BackendMemory || src != SourceExplicit {
		t.Fatalf("explicit memory = (%q,%q,%v), want (memory,explicit,nil)", b, src, err)
	}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		b, src, err := selectBackend("svc", &Options{}, envFrom(nil), goos)
		if err != nil {
			t.Fatalf("auto %s: unexpected err %v", goos, err)
		}
		if b == BackendMemory {
			t.Fatalf("auto on %s selected memory; must pick an OS keyring", goos)
		}
		if src != SourceAuto {
			t.Fatalf("auto %s src = %q, want auto", goos, src)
		}
	}
}

// TestParseBackend_RecognizesPass is cheap insurance against a future
// iterator regression in parseBackend: the lockstep test already pins
// that allBackends matches the declared constants, but a separate
// assertion on the public surface keeps a name we documented from
// silently disappearing.
func TestParseBackend_RecognizesPass(t *testing.T) {
	b, ok := parseBackend("pass")
	if !ok {
		t.Fatal("parseBackend(\"pass\") = (_, false); want true")
	}
	if b != BackendPass {
		t.Errorf("parseBackend(\"pass\") = %q, want %q", b, BackendPass)
	}
}

func TestParseBackend_RecognizesOnePasswordBackends(t *testing.T) {
	tests := map[string]Backend{
		"op":         BackendOP,
		"op-connect": BackendOPConnect,
		"op-desktop": BackendOPDesktop,
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, ok := parseBackend(input)
			if !ok {
				t.Fatalf("parseBackend(%q) = (_, false); want true", input)
			}
			if got != want {
				t.Fatalf("parseBackend(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

// TestSelectBackendOnePasswordNeverAuto mirrors the pass/memory contract:
// 1Password backends are selectable explicitly / via env / via config, but
// never by auto. Selecting one has external availability and account/vault
// coupling, so auto-picking it would be a stealth runtime behavior change.
func TestSelectBackendOnePasswordNeverAuto(t *testing.T) {
	const svc = "svc"
	for _, kind := range []Backend{BackendOP, BackendOPConnect, BackendOPDesktop} {
		t.Run(string(kind), func(t *testing.T) {
			if b, src, err := selectBackend(svc, &Options{Backend: kind}, envFrom(nil), "linux"); err != nil || b != kind || src != SourceExplicit {
				t.Fatalf("explicit %s = (%q,%q,%v), want (%q,explicit,nil)", kind, b, src, err, kind)
			}
			if b, src, err := selectBackend(svc, &Options{}, envFrom(map[string]string{"SVC_KEYRING_BACKEND": string(kind)}), "linux"); err != nil || b != kind || src != SourceEnv {
				t.Fatalf("env %s = (%q,%q,%v), want (%q,env,nil)", kind, b, src, err, kind)
			}
			if b, src, err := selectBackend(svc, &Options{ConfigBackend: kind}, envFrom(nil), "linux"); err != nil || b != kind || src != SourceConfig {
				t.Fatalf("config %s = (%q,%q,%v), want (%q,config,nil)", kind, b, src, err, kind)
			}
		})
	}

	for _, goos := range []string{"darwin", "windows", "linux"} {
		b, src, err := selectBackend(svc, &Options{}, envFrom(nil), goos)
		if err != nil {
			continue
		}
		if b == BackendOP || b == BackendOPConnect || b == BackendOPDesktop {
			t.Fatalf("auto on %s selected %s; 1Password backends must be opt-in only", goos, b)
		}
		if src != SourceAuto {
			t.Fatalf("auto %s src = %q, want auto", goos, src)
		}
	}
}

// TestSelectBackendPassNeverAuto mirrors the BackendMemory contract:
// pass is selectable explicitly / via env / via config, but never via
// auto. A future selectBackend refactor that started auto-picking pass
// on systems that happen to have the binary installed would be a stealth
// security regression (silent backend change), so pin the contract.
func TestSelectBackendPassNeverAuto(t *testing.T) {
	if b, src, err := selectBackend("svc", &Options{Backend: BackendPass}, envFrom(nil), "linux"); err != nil || b != BackendPass || src != SourceExplicit {
		t.Fatalf("explicit pass = (%q,%q,%v), want (pass,explicit,nil)", b, src, err)
	}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		b, src, err := selectBackend("svc", &Options{}, envFrom(nil), goos)
		if err != nil {
			continue // some goos may legitimately error in auto (e.g. unknown)
		}
		if b == BackendPass {
			t.Fatalf("auto on %s selected pass; pass must be opt-in only", goos)
		}
		if src != SourceAuto {
			t.Fatalf("auto %s src = %q, want auto", goos, src)
		}
	}
}

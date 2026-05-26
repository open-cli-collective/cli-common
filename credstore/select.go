package credstore

// Backend selection per the Secret-Handling Standard §1.4. selectBackend
// is a pure function — the process environment (getenv) and the target
// OS (goos) are injected — so every precedence and OS path is unit
// testable without a real keyring or mutating the runner's env. It is
// fail-closed: an unrecognized value at any layer is an error, never a
// silent fallback, and the in-memory backend is never auto-selected
// (preserving the INT-430/§2.1 posture).

import (
	"fmt"
	"strings"
)

// selectBackend resolves which backend to use and how it was chosen.
// Precedence (highest first): explicit Options.Backend > env
// <SERVICE>_KEYRING_BACKEND > Options.ConfigBackend > OS default. Every
// caller-influenced value is validated through parseBackend — even the
// explicit one is not trusted blindly.
func selectBackend(service string, opts *Options, getenv func(string) string, goos string) (Backend, Source, error) {
	if opts == nil {
		opts = &Options{} // self-defensive; Open already normalizes, but the pure fn must not panic for direct callers
	}
	if opts.Backend != "" {
		b, ok := parseBackend(string(opts.Backend))
		if !ok {
			return "", "", fmt.Errorf("%w: Options.Backend %q is not a known backend", ErrBackendNotImplemented, opts.Backend)
		}
		return b, SourceExplicit, nil
	}

	envVar := backendEnvVar(service)
	if v := getenv(envVar); v != "" {
		b, ok := parseBackend(v)
		if !ok {
			return "", "", fmt.Errorf("%w: %s=%q is not a known backend", ErrBackendNotImplemented, envVar, v)
		}
		return b, SourceEnv, nil
	}

	if opts.ConfigBackend != "" {
		b, ok := parseBackend(string(opts.ConfigBackend))
		if !ok {
			return "", "", fmt.Errorf("%w: config keyring.backend %q is not a known backend", ErrBackendNotImplemented, opts.ConfigBackend)
		}
		return b, SourceConfig, nil
	}

	b, ok := osDefaultBackend(goos)
	if !ok {
		return "", "", fmt.Errorf("%w: no default keyring backend for GOOS %q; set Options.Backend or %s", ErrBackendNotImplemented, goos, envVar)
	}
	return b, SourceAuto, nil
}

// allBackends is the single source of truth for the recognized backend
// name set, in stable display order. parseBackend, ValidBackendNames,
// BackendFlagUsage, and the test that guards against drift all derive
// from this slice — adding a backend means editing only this list (and
// any backend-specific construction in openOSBackend).
var allBackends = []Backend{
	BackendKeychain,
	BackendWinCred,
	BackendSecretService,
	BackendFile,
	BackendMemory,
}

// parseBackend maps a backend string (Options/env/config) to a Backend.
// Iterates allBackends so the recognized set has exactly one source.
func parseBackend(s string) (Backend, bool) {
	for _, b := range allBackends {
		if Backend(s) == b {
			return b, true
		}
	}
	return "", false
}

// osDefaultBackend is the §1.4 auto choice. Linux selects secret-service
// with no fallback — the unavailable/denied/locked classification and
// opt-in file fallback is a later unit (PR5). memory is never an auto
// default (fail-closed).
func osDefaultBackend(goos string) (Backend, bool) {
	switch goos {
	case "darwin":
		return BackendKeychain, true
	case "windows":
		return BackendWinCred, true
	case "linux":
		return BackendSecretService, true
	default:
		return "", false
	}
}

// backendEnvVar is the per-service backend selector variable:
// <SERVICE>_KEYRING_BACKEND, SERVICE being the upper-snake-cased service
// segment (e.g. slack-chat-api -> SLACK_CHAT_API_KEYRING_BACKEND). The
// selector is non-secret runtime config, not the §1.4 env-var secret
// exception.
func backendEnvVar(service string) string {
	return envServicePrefix(service) + "_KEYRING_BACKEND"
}

// envServicePrefix upper-snake-cases a service segment for env var
// names. Service segments are validSegment ([A-Za-z0-9_-]), so only
// '-' needs translating.
func envServicePrefix(service string) string {
	return strings.ToUpper(strings.ReplaceAll(service, "-", "_"))
}

package credstore

// Public helpers for CLIs that surface a --backend flag and a
// keyring.backend config key. Framework-agnostic: this file imports no
// flag package and no cobra. Each downstream CLI registers its own
// --backend flag using BackendFlagName / BackendFlagUsage and validates
// the value with ParseBackend (or routes both flag + config values
// through BindBackendFlag).
//
// Wiring contract (downstream CLIs):
//  1. Register --backend using BackendFlagName / BackendFlagUsage.
//  2. Load config; read the keyring.backend string (empty if unset).
//  3. Call BindBackendFlag(&opts, flagString, configString). The single
//     call validates the flag value and populates opts.Backend +
//     opts.ConfigBackend. Returned errors wrap ErrBackendNotImplemented.
//  4. Do NOT read <SERVICE>_KEYRING_BACKEND directly — credstore reads
//     it in selectBackend. Setting opts.Backend from that env var would
//     corrupt SourceEnv attribution and silently change precedence.
//
// Precedence (handled inside credstore.Open):
//   --backend flag > <SERVICE>_KEYRING_BACKEND env > config > OS default

import (
	"fmt"
	"strings"
)

// BackendFlagName is the canonical long-flag name CLIs should register.
const BackendFlagName = "backend"

// BackendFlagUsage is help text listing valid values and naming the
// per-service env var mechanism. Built at package-init time from
// allBackends so it stays in lock-step with the recognized set.
var BackendFlagUsage = buildBackendFlagUsage()

func buildBackendFlagUsage() string {
	names := make([]string, len(allBackends))
	for i, b := range allBackends {
		names[i] = string(b)
	}
	return "credential backend to use; one of: " + strings.Join(names, ", ") +
		". Precedence: --backend flag > <SERVICE>_KEYRING_BACKEND env var > config keyring.backend > OS default."
}

// ValidBackendNames returns the recognized backend name list in stable
// order. Derived from allBackends; use for completion, error messages,
// and help text generation. The returned slice is a fresh copy and is
// safe for callers to mutate.
func ValidBackendNames() []string {
	out := make([]string, len(allBackends))
	for i, b := range allBackends {
		out[i] = string(b)
	}
	return out
}

// ParseBackend validates a user-supplied backend name and returns the
// typed Backend. On failure the error lists every valid value and wraps
// ErrBackendNotImplemented so callers can classify with errors.Is —
// matching selectBackend's existing failure class.
func ParseBackend(name string) (Backend, error) {
	if b, ok := parseBackend(name); ok {
		return b, nil
	}
	return "", fmt.Errorf("%w: %q is not a known backend (valid: %s)",
		ErrBackendNotImplemented, name, strings.Join(ValidBackendNames(), ", "))
}

// BackendEnvVar returns the per-service env-var name that controls the
// backend (e.g. service "atlassian-cli" -> "ATLASSIAN_CLI_KEYRING_BACKEND").
// Exposed so CLI help text can show the actual var, not a placeholder.
//
// Precondition: service must already be a valid credstore service
// segment (the same value passed to credstore.Open). Service-name
// validation is credstore.Open's responsibility, not this helper's —
// callers pass a constant in practice.
func BackendEnvVar(service string) string {
	return backendEnvVar(service)
}

// BindBackendFlag applies the user-supplied --backend flag value and
// the config-file value to opts, validating the flag value.
// configValue is passed through to opts.ConfigBackend unchanged —
// credstore.Open will validate it during selection (so a stale config
// value surfaces as a clean error at Open time, not silent acceptance).
// Pass "" for either flagValue or configValue when not supplied.
//
// On invalid flag input, opts is not mutated and the returned error
// wraps ErrBackendNotImplemented.
//
// opts must be non-nil; passing nil returns an error rather than
// panicking. CLIs must NOT read <SERVICE>_KEYRING_BACKEND themselves
// and pass it here as flagValue — credstore reads that env var
// directly in selectBackend, and remapping it would corrupt SourceEnv
// attribution.
func BindBackendFlag(opts *Options, flagValue, configValue string) error {
	if opts == nil {
		return fmt.Errorf("credstore: BindBackendFlag requires a non-nil *Options")
	}
	if flagValue != "" {
		b, err := ParseBackend(flagValue)
		if err != nil {
			return err
		}
		opts.Backend = b
	}
	opts.ConfigBackend = Backend(configValue)
	return nil
}

package credstore

// Public helpers for CLIs that surface a --backend flag and a
// keyring.backend config key. Framework-agnostic: this file imports no
// flag package and no cobra. Each downstream CLI registers its own
// --backend flag using BackendFlagName / BackendFlagUsage() and validates
// the value with ParseBackend (or routes both flag + config values
// through BindBackendFlag). Invalid backend values returned by either
// helper wrap ErrBackendNotImplemented; the nil-opts guard on
// BindBackendFlag is a separate programmer-error signal that does not.
//
// Wiring contract (downstream CLIs):
//  1. Register --backend using BackendFlagName / BackendFlagUsage().
//  2. Load config; read the keyring.backend string (empty if unset).
//  3. Call BindBackendFlag(&opts, flagValue, flagSet, configValue),
//     passing flagSet=true exactly when the user actually supplied the
//     flag (e.g. cmd.Flags().Changed("backend") in cobra). The single
//     call validates the flag value and populates opts.Backend +
//     opts.ConfigBackend. Invalid backend values (including an
//     explicit empty --backend=) return an error wrapping
//     ErrBackendNotImplemented with opts untouched.
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

// BackendFlagUsage returns help text listing valid values and naming
// the per-service env var mechanism. Built fresh from allBackends each
// call so it stays in lock-step with the recognized set — a function
// rather than an exported var so external packages cannot accidentally
// overwrite it and corrupt every consumer's help text.
func BackendFlagUsage() string {
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
// Pass "" for configValue when no config-file value is set.
//
// flagSet must reflect whether the user actually supplied --backend on
// the command line (typically cmd.Flags().Changed("backend") in cobra).
// When flagSet is false, flagValue is ignored and opts.Backend is not
// touched. When flagSet is true, flagValue must be a recognized backend
// name; an explicit empty --backend= is rejected as fail-closed, not
// silently treated as "no flag." This prevents an explicit empty flag
// from silently falling through to lower-precedence env/config/auto
// selection.
//
// On invalid flag input, opts is not mutated and the returned error
// wraps ErrBackendNotImplemented.
//
// opts must be non-nil; passing nil returns an error rather than
// panicking. CLIs must NOT read <SERVICE>_KEYRING_BACKEND themselves
// and pass it here as flagValue — credstore reads that env var
// directly in selectBackend, and remapping it would corrupt SourceEnv
// attribution.
func BindBackendFlag(opts *Options, flagValue string, flagSet bool, configValue string) error {
	if opts == nil {
		return fmt.Errorf("credstore: BindBackendFlag requires a non-nil *Options")
	}
	if flagSet {
		b, err := ParseBackend(flagValue)
		if err != nil {
			return err
		}
		opts.Backend = b
	}
	opts.ConfigBackend = Backend(configValue)
	return nil
}

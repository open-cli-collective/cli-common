package credstore

// Linux Secret Service classification and the auto-only file fallback,
// per the Secret-Handling Standard §1.4 lines 152-157. The fragile part
// — turning a D-Bus error into a decision — is isolated into the pure
// classifySecretServiceErr so it is exhaustively unit-testable with
// synthetic errors and never needs a live D-Bus.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/99designs/keyring"
	"github.com/godbus/dbus"
)

// ssClass is the result of classifying a Secret Service probe (§1.4).
type ssClass int

const (
	ssReachable   ssClass = iota // probe succeeded; secret-service is usable
	ssUnavailable                // no D-Bus session bus / no Secret Service daemon → file fallback
	ssDenied                     // present but locked / auth failure / rejects → fail closed
	ssAmbiguous                  // cannot confidently classify → treat as denied, fail closed
)

// dbusErrName extracts a D-Bus error name from err if one is present
// (typed, possibly wrapped as either *dbus.Error or dbus.Error).
// Returns "" when err carries no dbus.Error.
func dbusErrName(err error) string {
	var dep *dbus.Error
	if errors.As(err, &dep) {
		return dep.Name
	}
	var dev dbus.Error
	if errors.As(err, &dev) {
		return dev.Name
	}
	return ""
}

// classifySecretServiceErr maps a Secret Service probe error to an
// ssClass (§1.4). Pure and conservative: only a clear "no bus / service
// unknown" signal is ssUnavailable, only a clear "locked / denied / no
// session" signal is ssDenied; everything else — unrecognized D-Bus
// names, timeouts, generic OS/filesystem errors, opaque shapes — is
// ssAmbiguous, which the caller fails closed on. The bias is
// deliberate (§1.4): the file fallback is opt-in only when we are sure
// no working keyring is present, so an unrelated error never triggers a
// stealth downgrade.
func classifySecretServiceErr(err error) ssClass {
	if err == nil {
		return ssReachable
	}
	switch name := dbusErrName(err); name {
	case "org.freedesktop.DBus.Error.ServiceUnknown",
		"org.freedesktop.DBus.Error.NameHasNoOwner",
		"org.freedesktop.DBus.Error.Spawn.ServiceNotFound":
		return ssUnavailable
	case "org.freedesktop.Secret.Error.IsLocked",
		"org.freedesktop.Secret.Error.NoSession",
		"org.freedesktop.DBus.Error.AccessDenied":
		return ssDenied
	case "":
		// No typed dbus.Error — fall through to the conservative
		// string check for connection-setup failures.
	default:
		// A recognized dbus.Error name we deliberately do not treat as
		// unavailable (NoReply, Timeout, opaque) → ambiguous.
		return ssAmbiguous
	}
	// Connection-setup failures surface before any call as a plain
	// (non-dbus.Error) error. ONLY an explicit session-bus signature is
	// unavailable; a bare filesystem phrase (e.g. "no such file or
	// directory") is not, so an unrelated FS error cannot cause a
	// stealth file downgrade.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "dbus_session_bus_address") ||
		strings.Contains(msg, "session bus") {
		return ssUnavailable
	}
	return ssAmbiguous
}

// linuxAutoBackend decides the Linux auto-path backend from a Secret
// Service probe (§1.4 lines 152-157). It needs nothing but the probe:
// file-passphrase handling happens later in openOSBackend and the
// fail-closed message is static.
func linuxAutoBackend(probe func() error) (Backend, error) {
	switch classifySecretServiceErr(probe()) {
	case ssReachable:
		return BackendSecretService, nil
	case ssUnavailable:
		return BackendFile, nil
	case ssDenied:
		return "", fmt.Errorf("%w: secret-service is present but the keyring is locked or denied access (probe: list keys); unlock it (gnome-keyring-daemon, seahorse, kwalletmanager) or set <SERVICE>_KEYRING_BACKEND=file to use the encrypted file backend", ErrSecretServiceFailClosed)
	case ssAmbiguous:
		return "", fmt.Errorf("%w: could not confirm secret-service availability (probe: list keys returned an unrecognized failure); failing closed instead of silently downgrading — unlock the keyring (gnome-keyring-daemon, seahorse, kwalletmanager) or set <SERVICE>_KEYRING_BACKEND=file", ErrSecretServiceFailClosed)
	}
	// Unreached: classifySecretServiceErr only ever returns one of the
	// four classes above. Defensive fail-closed keeps the contract if a
	// class is added without updating this switch.
	return "", fmt.Errorf("%w: unclassified secret-service probe result", ErrSecretServiceFailClosed)
}

// probeSecretService opens the Secret Service backend and performs one
// harmless operation (list keys) to force the D-Bus round-trip
// (keyring.Open is lazy). Returns the first error, or nil if Secret
// Service is reachable. The only impure piece; injected into Open via
// openWithDeps so tests never touch D-Bus. getenv is unused (Secret
// Service needs no env) but kept for signature symmetry with the
// injected probe type.
func probeSecretService(service string, _ func(string) string) error {
	kr, err := keyring.Open(keyring.Config{
		ServiceName:     service,
		AllowedBackends: []keyring.BackendType{keyring.SecretServiceBackend},
	})
	if err != nil {
		return err
	}
	_, err = kr.Keys()
	return err
}

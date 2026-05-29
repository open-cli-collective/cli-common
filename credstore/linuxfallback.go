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

	"github.com/godbus/dbus/v5"
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
	// (non-dbus.Error) error. Match ONLY the address-determination
	// signatures: the env-var name, or godbus's "couldn't determine
	// address of session bus" (hence "determine" + "session bus"
	// co-occurring). A bare "session bus" substring is intentionally
	// NOT enough — a message like "session bus is locked and requires
	// authentication" must stay ambiguous (→ fail closed), never become
	// a stealth file downgrade. Likewise a bare filesystem phrase.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "dbus_session_bus_address") ||
		(strings.Contains(msg, "session bus") && strings.Contains(msg, "determine")) {
		return ssUnavailable
	}
	return ssAmbiguous
}

// linuxAutoBackend decides the Linux auto-path backend from a Secret
// Service probe (§1.4 lines 152-157). envVar is the resolved
// per-service backend-selector name (e.g. ATLASSIAN_CLI_KEYRING_BACKEND)
// so the fail-closed remediation is a copy-pasteable command, not an
// un-substituted "<SERVICE>" template. File-passphrase handling happens
// later in openOSBackend.
func linuxAutoBackend(probe func() error, envVar string) (Backend, error) {
	switch classifySecretServiceErr(probe()) {
	case ssReachable:
		return BackendSecretService, nil
	case ssUnavailable:
		return BackendFile, nil
	case ssDenied:
		return "", fmt.Errorf("%w: secret-service is present but the keyring is locked or denied access (probe: list keys); unlock it (gnome-keyring-daemon, seahorse, kwalletmanager) or set %s=file to use the encrypted file backend", ErrSecretServiceFailClosed, envVar)
	case ssAmbiguous:
		return "", fmt.Errorf("%w: could not confirm secret-service availability (probe: list keys returned an unrecognized failure); failing closed instead of silently downgrading — unlock the keyring (gnome-keyring-daemon, seahorse, kwalletmanager) or set %s=file", ErrSecretServiceFailClosed, envVar)
	}
	// Unreached: classifySecretServiceErr only ever returns one of the
	// four classes above. Defensive fail-closed keeps the contract if a
	// class is added without updating this switch.
	return "", fmt.Errorf("%w: unclassified secret-service probe result", ErrSecretServiceFailClosed)
}

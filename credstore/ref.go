// Package credstore is the shared credential-store library for Open CLI
// Collective CLIs. It implements the Open CLI Collective Secret-Handling
// Standard (working-with-secrets.md; epic INT-310, ticket INT-429).
//
// This file implements the credential-ref grammar (standard §1.3) and the
// default-ref codification (§2.1). A credential ref is exactly
// "<service>/<profile>": two segments joined by a single '/'. Each segment
// is drawn from [A-Za-z0-9_-] and is non-empty. '/' is structural and is
// therefore forbidden inside a segment.
//
// The OS-keyring backends, the Store/Open lifecycle, bundle operations,
// redaction, and legacy migration helpers are intentionally not in this
// file; they are separate units of work under epic INT-310.
package credstore

import (
	"fmt"
	"strings"
)

// DefaultProfile is the profile segment used when a CLI does not specify
// one. The standard (§2.1) codifies the default credential ref as
// "<service>/default".
const DefaultProfile = "default"

// RefErrorKind classifies why a credential ref failed validation.
type RefErrorKind int

const (
	// RefErrorEmpty means the ref input was empty, or one of its segments
	// was empty (e.g. "/profile" or "service/").
	RefErrorEmpty RefErrorKind = iota
	// RefErrorSegmentCount means the input was not of the form
	// "<service>/<profile>" — it did not contain exactly one '/'.
	RefErrorSegmentCount
	// RefErrorInvalidChar means a segment contained a byte outside the
	// allowed set [A-Za-z0-9_-].
	RefErrorInvalidChar
)

// RefError is the typed error returned by all ref operations so callers
// can produce actionable messages and branch on errors.Is against the
// package sentinels. Credential refs are non-secret configuration
// (standard §1.2), so RefError safely echoes the offending value — no
// leak concern.
type RefError struct {
	Kind RefErrorKind
	// Segment is "service" or "profile" when the failure is specific to
	// one segment; otherwise "".
	Segment string
	// Ref is the offending input (a full ref for ParseRef, or the
	// offending segment value for FormatRef). Non-secret per §1.2.
	Ref string
}

func (e *RefError) Error() string {
	switch e.Kind {
	case RefErrorEmpty:
		if e.Segment != "" {
			return fmt.Sprintf("credstore: empty %s segment in credential ref %q", e.Segment, e.Ref)
		}
		return "credstore: empty credential ref"
	case RefErrorSegmentCount:
		return fmt.Sprintf("credstore: credential ref %q must be \"<service>/<profile>\" (exactly one '/')", e.Ref)
	case RefErrorInvalidChar:
		return fmt.Sprintf("credstore: %s segment in credential ref %q contains a character outside [A-Za-z0-9_-]", e.Segment, e.Ref)
	default:
		return fmt.Sprintf("credstore: invalid credential ref %q", e.Ref)
	}
}

// Is reports whether target is a *RefError of the same Kind, so callers
// can write errors.Is(err, credstore.ErrRefEmpty).
func (e *RefError) Is(target error) bool {
	t, ok := target.(*RefError)
	if !ok {
		return false
	}
	return t.Kind == e.Kind
}

// Sentinels for errors.Is. They carry only a Kind.
var (
	ErrRefEmpty        = &RefError{Kind: RefErrorEmpty}
	ErrRefSegmentCount = &RefError{Kind: RefErrorSegmentCount}
	ErrRefInvalidChar  = &RefError{Kind: RefErrorInvalidChar}
)

func isSegmentByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z':
		return true
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_' || b == '-':
		return true
	default:
		return false
	}
}

// validSegment reports whether s is a non-empty string drawn entirely
// from [A-Za-z0-9_-]. Iteration is byte-wise: the charset is ASCII, so
// any multi-byte UTF-8 input necessarily contains a non-conforming byte.
func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isSegmentByte(s[i]) {
			return false
		}
	}
	return true
}

// ParseRef splits a credential ref into its service and profile segments.
// Validation order (standard §1.3): empty input, then exactly-one-'/',
// then non-empty segments, then charset.
func ParseRef(ref string) (service, profile string, err error) {
	if ref == "" {
		return "", "", &RefError{Kind: RefErrorEmpty, Ref: ref}
	}
	if strings.Count(ref, "/") != 1 {
		return "", "", &RefError{Kind: RefErrorSegmentCount, Ref: ref}
	}
	service, profile, _ = strings.Cut(ref, "/")
	if service == "" {
		return "", "", &RefError{Kind: RefErrorEmpty, Segment: "service", Ref: ref}
	}
	if profile == "" {
		return "", "", &RefError{Kind: RefErrorEmpty, Segment: "profile", Ref: ref}
	}
	if !validSegment(service) {
		return "", "", &RefError{Kind: RefErrorInvalidChar, Segment: "service", Ref: ref}
	}
	if !validSegment(profile) {
		return "", "", &RefError{Kind: RefErrorInvalidChar, Segment: "profile", Ref: ref}
	}
	return service, profile, nil
}

// FormatRef is the inverse of ParseRef: it validates both segments and
// joins them with '/'. The error's Ref field carries the offending
// segment value (non-secret per §1.2).
func FormatRef(service, profile string) (string, error) {
	if service == "" {
		return "", &RefError{Kind: RefErrorEmpty, Segment: "service"}
	}
	if !validSegment(service) {
		return "", &RefError{Kind: RefErrorInvalidChar, Segment: "service", Ref: service}
	}
	if profile == "" {
		return "", &RefError{Kind: RefErrorEmpty, Segment: "profile"}
	}
	if !validSegment(profile) {
		return "", &RefError{Kind: RefErrorInvalidChar, Segment: "profile", Ref: profile}
	}
	return service + "/" + profile, nil
}

// DefaultRef returns "<service>/default", codifying the standard's §2.1
// default credential ref. It is exactly FormatRef(service, DefaultProfile).
func DefaultRef(service string) (string, error) {
	return FormatRef(service, DefaultProfile)
}

// EscapeRefSegment deterministically encodes an arbitrary string into the
// segment charset [A-Za-z0-9_-], for CLIs that derive a profile from a
// richer identifier such as an email address (standard §1.3).
//
// Scheme: bytes in [A-Za-z0-9-] pass through; '_' is the escape byte, so a
// literal '_' becomes "__"; every other byte becomes "_x" followed by two
// lowercase hex digits (per UTF-8 byte). For example "rian@monitapp.io"
// encodes to "rian_x40monitapp_x2eio". The encoding is reversible by
// construction; an Unescape is intentionally not provided until a consumer
// needs it.
//
// EscapeRefSegment("") == "". An empty string is not a valid ref segment;
// that only becomes an error at FormatRef/ParseRef validation time, not
// here.
func EscapeRefSegment(raw string) string {
	const hexdig = "0123456789abcdef"
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= 'A' && c <= 'Z',
			c >= 'a' && c <= 'z',
			c >= '0' && c <= '9',
			c == '-':
			out = append(out, c)
		case c == '_':
			out = append(out, '_', '_')
		default:
			out = append(out, '_', 'x', hexdig[c>>4], hexdig[c&0x0f])
		}
	}
	return string(out)
}

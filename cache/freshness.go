package cache

import (
	"fmt"
	"time"
)

// Status is the coarse freshness classification.
//
// Classify returns ONLY Fresh, Stale, or Manual. Uninitialized is
// caller-derived (a ReadResource ErrCacheMiss — no envelope on disk) and
// Unavailable is registry-derived (a tier-2 concern). Both are defined here
// for jtk parity and a clean tier-2 lift, and are exercised only via String();
// Classify never returns them.
type Status int

const (
	StatusUninitialized Status = iota // no envelope on disk (caller-derived from ErrCacheMiss)
	StatusFresh                       // on disk, FetchedAt + TTL still in the future
	StatusStale                       // on disk, FetchedAt + TTL elapsed (or zero FetchedAt)
	StatusManual                      // TTL == "manual"; never auto-expires
	StatusUnavailable                 // registry-derived; never returned by Classify
)

// String returns the status label.
func (s Status) String() string {
	switch s {
	case StatusUninitialized:
		return "uninitialized"
	case StatusFresh:
		return "fresh"
	case StatusStale:
		return "stale"
	case StatusManual:
		return "manual"
	case StatusUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// ttlManual is the TTL sentinel meaning "never auto-expire".
const ttlManual = "manual"

// parseTTL returns the TTL as a duration. The "manual" sentinel returns
// (0, true, nil).
func parseTTL(ttl string) (time.Duration, bool, error) {
	if ttl == ttlManual {
		return 0, true, nil
	}
	d, err := time.ParseDuration(ttl)
	if err != nil {
		return 0, false, fmt.Errorf("cache: parsing TTL %q: %w", ttl, err)
	}
	return d, false, nil
}

// Classify inspects an envelope's FetchedAt + TTL at now and returns one of
// StatusFresh, StatusStale, or StatusManual. A zero FetchedAt (the
// uninitialized / Touch-ed state) and an unparseable TTL both classify as
// StatusStale — callers that need to distinguish "never fetched" check
// FetchedAt.IsZero() themselves.
func Classify(fetchedAt time.Time, ttl string, now time.Time) Status {
	d, manual, err := parseTTL(ttl)
	if err != nil {
		return StatusStale
	}
	if manual {
		return StatusManual
	}
	if fetchedAt.IsZero() || now.Sub(fetchedAt) >= d {
		return StatusStale
	}
	return StatusFresh
}

// Age returns a short human-readable age ("8h", "3d", "2m", "45s") for status
// output. A zero fetchedAt returns "-"; a negative delta is clamped to 0.
func Age(fetchedAt, now time.Time) string {
	if fetchedAt.IsZero() {
		return "-"
	}
	delta := now.Sub(fetchedAt)
	if delta < 0 {
		delta = 0
	}
	switch {
	case delta >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(delta/(24*time.Hour)))
	case delta >= time.Hour:
		return fmt.Sprintf("%dh", int(delta/time.Hour))
	case delta >= time.Minute:
		return fmt.Sprintf("%dm", int(delta/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(delta/time.Second))
	}
}

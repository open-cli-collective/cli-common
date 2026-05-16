package credstore

// This file implements the package-level redaction helpers per the Open
// CLI Collective Secret-Handling Standard §2.1 (lines 403-411), which
// exist to enforce the §1.12 logging/display/telemetry no-leak contracts.
// A CLI builds a Redactor populated with the secrets it loaded from the
// keyring (plus any obtained at runtime via refresh) and routes its
// --http-debug / --verbose / panic-recovery output through it.
//
// Fail-closed bias (consistent with §1.4's philosophy applied to output):
// redaction errs toward over-redaction and never toward leaking. Redact
// works off interval-union over the *original* input so partially
// overlapping secrets cannot leave an un-scrubbed fragment, and a secret
// that happens to look like placeholder text cannot defeat scrubbing.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Redactor scrubs known secret values out of strings, HTTP headers, and
// writer streams. The zero value is usable (no secrets loaded → input is
// returned unchanged); NewRedactor is the normal constructor. A Redactor
// is safe for concurrent use: CLIs typically Add a refreshed token from
// one goroutine while another logs through a RedactWriter.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

// NewRedactor returns a Redactor pre-loaded with secrets. Empty strings
// are ignored (see Add); duplicates are de-duplicated.
func NewRedactor(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		r.Add(s)
	}
	return r
}

// Add loads an additional secret discovered after construction (e.g. a
// refreshed token). An empty secret is ignored rather than rejected:
// there is no error return, and an empty string would match everywhere
// (strings.Contains(x, "") is always true), corrupting all output. A
// secret already loaded is a no-op (linear-scan dedup).
//
// The secret set is expected to be small and bounded — the handful of
// values a CLI loaded from the keyring plus the occasional refresh — so
// the O(n) dedup scan is intentionally not replaced with a map and the
// list is intentionally uncapped. A caller that adds unbounded distinct
// secrets in a loop is misusing the type.
func (r *Redactor) Add(secret string) {
	if secret == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.secrets {
		if s == secret {
			return
		}
	}
	r.secrets = append(r.secrets, secret)
}

// snapshot returns a copy of the loaded secrets under the read lock so
// callers can scan without holding the lock for the duration.
func (r *Redactor) snapshot() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.secrets) == 0 {
		return nil
	}
	out := make([]string, len(r.secrets))
	copy(out, r.secrets)
	return out
}

// redactedPlaceholder is the standard's length-only replacement token,
// e.g. "<redacted, len=84>" (§1.12).
func redactedPlaceholder(n int) string {
	return fmt.Sprintf("<redacted, len=%d>", n)
}

// containsAnySecret reports whether any loaded secret is a substring of s.
// secrets are guaranteed non-empty (filtered at every ingress).
func containsAnySecret(s string, secrets []string) bool {
	for _, sec := range secrets {
		if strings.Contains(s, sec) {
			return true
		}
	}
	return false
}

// safeReplacement is the best-effort, context-preserving replacement for
// a redacted span. It is normally the standard "<redacted, len=N>"
// placeholder — but that literal itself contains the substrings
// "redacted", "len", and the decimal N, so a loaded secret equal to e.g.
// "len", "redacted", or a digit string would be reintroduced verbatim by
// the placeholder and leak (§1.12). When that would happen the span is
// dropped to the empty string instead (maximal over-redaction). Normal
// token secrets never collide, so the common path always emits the
// standard placeholder. This is per-span only; Redact applies a final
// whole-output guard (a dropped span can let gap text join into a
// different loaded secret across the seam), so correctness does not rely
// on this function alone.
func safeReplacement(n int, secrets []string) string {
	rep := redactedPlaceholder(n)
	if containsAnySecret(rep, secrets) {
		return ""
	}
	return rep
}

// Redact replaces every occurrence of every loaded secret in s with
// "<redacted, len=N>". It scans the original input for all secret
// occurrences as half-open byte intervals, unions intervals that
// strictly overlap (adjacent-but-distinct secrets stay separate
// placeholders, each carrying its own length), and emits one placeholder
// per merged interval where N is the number of original bytes redacted.
// In the common non-overlapping case N == len(secret), matching the
// standard's example exactly; in the overlap corner N is the redacted
// span length — still only a length, never secret material.
//
// Matching against the original input (not iterative replacement) means
// the result is order-independent. Two layers keep it fail-closed: a
// placeholder that would itself contain a loaded secret drops that span
// to "" (safeReplacement), and a final whole-output guard suppresses the
// entire result (returns "") if any loaded secret still appears — which
// can happen when a dropped span lets gap text join across the seam. The
// empty string can contain no non-empty secret, so the result is
// guaranteed secret-free; normal token secrets never hit either layer.
func (r *Redactor) Redact(s string) string {
	return redactWith(s, r.snapshot())
}

// redactWith is the snapshot-free core of Redact. RedactHeaders takes one
// snapshot at entry and threads it through both the always-redact and
// per-value paths so a single invocation cannot straddle two different
// secret sets under a concurrent Add.
func redactWith(s string, secrets []string) string {
	if s == "" {
		return s
	}
	if len(secrets) == 0 {
		return s
	}

	type span struct{ start, end int }
	var spans []span
	for _, sec := range secrets {
		// sec is guaranteed non-empty (filtered at every ingress).
		for i := 0; i <= len(s)-len(sec); {
			j := strings.Index(s[i:], sec)
			if j < 0 {
				break
			}
			start := i + j
			spans = append(spans, span{start, start + len(sec)})
			// Advance one byte past the match start (not by len(sec))
			// so overlapping occurrences of the same secret, e.g. "aaa"
			// in "aaaaa", are all collected; the union dedupes for free.
			i = start + 1
		}
	}
	if len(spans) == 0 {
		return s
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })

	var b strings.Builder
	prev := 0
	curStart, curEnd := spans[0].start, spans[0].end
	flush := func() {
		b.WriteString(s[prev:curStart])
		b.WriteString(safeReplacement(curEnd-curStart, secrets))
		prev = curEnd
	}
	for _, sp := range spans[1:] {
		if sp.start < curEnd { // strictly overlapping → merge
			if sp.end > curEnd {
				curEnd = sp.end
			}
			continue
		}
		// Adjacent (sp.start == curEnd) or disjoint → emit current, start new.
		flush()
		curStart, curEnd = sp.start, sp.end
	}
	flush()
	b.WriteString(s[prev:])

	out := b.String()
	// Final fail-closed guard. Per-span safeReplacement can drop a
	// colliding span to "", which may let the two surrounding gap
	// fragments join into a *different* loaded secret that was not a
	// contiguous occurrence in the original input (e.g. secrets
	// {"X","len","ab"} over "aXb" → "a"+""+"b" == "ab"). If anything
	// loaded still appears, suppress the whole result: "" can contain no
	// non-empty secret. This never triggers on normal token secrets.
	if containsAnySecret(out, secrets) {
		return ""
	}
	return out
}

// alwaysRedactHeaders are unconditionally scrubbed regardless of content
// (§1.12). Compared lower-cased so a non-canonical raw map key still hits.
var alwaysRedactHeaders = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"set-cookie":    {},
}

// RedactHeaders scrubs h in place for HTTP wire logs (§1.12). Every value
// of Authorization, Cookie, and Set-Cookie is replaced wholesale with a
// length-only placeholder; every other header value is run through Redact
// so any header whose value contains a loaded secret ("any custom auth
// headers") is scrubbed by substring. A nil map is a no-op.
//
// Whole-value redaction of the always-redact set is deliberately more
// conservative than §1.12's illustrative scheme-preserving
// "Bearer <redacted, len=84>": preserving the scheme word is a CLI-side
// nicety, out of scope for the shared helper, and never preserving it
// cannot leak.
func (r *Redactor) RedactHeaders(h http.Header) {
	if h == nil {
		return
	}
	secrets := r.snapshot() // one snapshot threaded through both paths
	for name, values := range h {
		if _, force := alwaysRedactHeaders[strings.ToLower(name)]; force {
			for i, v := range values {
				values[i] = safeReplacement(len(v), secrets)
			}
			continue
		}
		for i, v := range values {
			values[i] = redactWith(v, secrets)
		}
	}
}

// redactWriter wraps an io.Writer and scrubs each buffer before forwarding.
type redactWriter struct {
	r *Redactor
	w io.Writer
}

// RedactWriter returns a writer that runs every buffer through Redact
// before forwarding to w, so a CLI's debug-log writer auto-scrubs without
// each call site remembering to.
//
// Limitation: redaction is per-Write. A secret split across two Write
// calls is not caught — there is intentionally no internal buffering
// (buffering a debug stream would itself retain secret bytes and needs a
// flush contract). Debug / wire loggers write whole records per call,
// which is the supported case.
//
// Fail-closed drop: when Redact suppresses the whole buffer (a degenerate
// secret/placeholder collision), the wrapper forwards zero bytes and
// still reports Write success (len(p), nil) — the record is silently
// dropped rather than emitted unredacted. This is intended security
// behavior; it only happens for pathological non-token secrets.
func (r *Redactor) RedactWriter(w io.Writer) io.Writer {
	return &redactWriter{r: r, w: w}
}

// Write honours the io.Writer contract against the caller's logical
// buffer p. The redacted form may differ in length from p, so a partial
// downstream count cannot be mapped back to p meaningfully: on full
// success report len(p); on any failure report nothing durably written.
func (rw *redactWriter) Write(p []byte) (int, error) {
	red := []byte(rw.r.Redact(string(p)))
	n, err := rw.w.Write(red)
	if err != nil {
		return 0, err
	}
	if n != len(red) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

// ErrSecretLeaked is the stable identity of the error NoLeakAssertion
// returns on a leak. errors.Is(err, ErrSecretLeaked) holds regardless of
// the (fail-closed, possibly empty) message text, so callers keep a
// programmatic signal even when the human-readable detail is suppressed.
// This constant is a fixed phrase, not runtime secret material.
var ErrSecretLeaked = errors.New("credstore: secret material leaked into output")

// leakError carries the fail-closed message while reporting the stable
// ErrSecretLeaked identity to errors.Is.
type leakError struct{ msg string }

func (e *leakError) Error() string { return e.msg }

func (e *leakError) Is(target error) bool { return target == ErrSecretLeaked }

// NoLeakAssertion is the test helper backing each CLI's mandatory
// "no-leak" test (§1.12). It returns a non-nil error (matching
// ErrSecretLeaked via errors.Is) when any non-empty secret appears in
// output, naming each leaked secret by its argument ordinal and length
// only — never the value, not even a masked prefix (§1.8/§1.12 treat
// masked secret material as still secret). Empty secrets are skipped.
// Returns nil when output is clean.
//
// The error string is itself fail-closed: the detailed "secret #N
// (len=K)" wording can contain a short or placeholder-shaped secret
// (a secret of "len", "1", or "secret" is a substring of the message),
// so the message is degraded to a guaranteed secret-free form — finally
// the empty string — before being returned. The leak still surfaces as a
// non-nil error matching ErrSecretLeaked; it just never echoes the value
// in the failure path.
func NoLeakAssertion(output []byte, secrets ...string) error {
	var leaked []string
	var nonEmpty []string
	for i, sec := range secrets {
		if sec == "" {
			continue
		}
		nonEmpty = append(nonEmpty, sec)
		if bytes.Contains(output, []byte(sec)) {
			leaked = append(leaked, fmt.Sprintf("secret #%d (len=%d)", i+1, len(sec)))
		}
	}
	if len(leaked) == 0 {
		return nil
	}
	msg := "credstore: secret material leaked into output: " + strings.Join(leaked, ", ")
	return &leakError{msg: safeErrorText(msg, nonEmpty)}
}

// safeErrorText returns msg unless a loaded secret is a substring of it,
// then a progressively less-detailed, guaranteed secret-free fallback,
// finally "" (which can contain no non-empty secret). This applies the
// same fail-closed principle as safeReplacement to the failure path so
// NoLeakAssertion never echoes a value (§1.12). secrets are all non-empty.
func safeErrorText(msg string, secrets []string) string {
	if !containsAnySecret(msg, secrets) {
		return msg
	}
	const generic = "credstore: a loaded value leaked into output (details withheld to avoid echoing it)"
	if !containsAnySecret(generic, secrets) {
		return generic
	}
	return ""
}

package credstore

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestRedactBasic(t *testing.T) {
	r := NewRedactor("s3cr3t")
	if got := r.Redact("token=s3cr3t end"); got != "token=<redacted, len=6> end" {
		t.Fatalf("Redact = %q", got)
	}
	// Multiple occurrences of one secret are all redacted.
	if got := r.Redact("s3cr3t and s3cr3t"); got != "<redacted, len=6> and <redacted, len=6>" {
		t.Fatalf("multi-occurrence = %q", got)
	}
	// No secrets loaded → passthrough; zero-value Redactor → passthrough.
	if got := NewRedactor().Redact("nothing here"); got != "nothing here" {
		t.Fatalf("no-secret passthrough = %q", got)
	}
	var zero Redactor
	if got := zero.Redact("zero value"); got != "zero value" {
		t.Fatalf("zero-value Redactor = %q", got)
	}
}

func TestRedactLenIsOriginalSecretLength(t *testing.T) {
	secret := strings.Repeat("x", 84)
	r := NewRedactor(secret)
	if got := r.Redact("Authorization: Bearer " + secret); got != "Authorization: Bearer <redacted, len=84>" {
		t.Fatalf("len-N = %q", got)
	}
}

func TestRedactNestedSecretNoFragmentLeak(t *testing.T) {
	// "BCD" is wholly inside "ABCDE": the union must redact the whole span,
	// and no raw fragment of either secret may remain.
	r := NewRedactor("ABCDE", "BCD")
	got := r.Redact("xABCDEy")
	if got != "x<redacted, len=5>y" {
		t.Fatalf("nested = %q", got)
	}
	if strings.Contains(got, "BCD") || strings.Contains(got, "ABCDE") {
		t.Fatalf("raw secret fragment leaked: %q", got)
	}
}

func TestRedactPartiallyOverlappingSecrets(t *testing.T) {
	// The case order-based ReplaceAll would leak: shared "123".
	r := NewRedactor("abc123", "123xyz")
	got := r.Redact("abc123xyz")
	if got != "<redacted, len=9>" {
		t.Fatalf("overlap = %q", got)
	}
	for _, frag := range []string{"abc", "xyz", "123"} {
		if strings.Contains(got, frag) {
			t.Fatalf("raw fragment %q leaked: %q", frag, got)
		}
	}
}

func TestRedactAdjacentDistinctSecretsStaySeparate(t *testing.T) {
	// Adjacent (touching, not overlapping) distinct secrets keep their
	// individual lengths rather than merging into one span.
	r := NewRedactor("abc", "def")
	if got := r.Redact("abcdef"); got != "<redacted, len=3><redacted, len=3>" {
		t.Fatalf("adjacent = %q", got)
	}
}

func TestRedactOverlappingSameSecret(t *testing.T) {
	// "aaa" in "aaaaa": scanner must advance by one byte, not len(secret),
	// or the trailing "aa" would survive — but even the union must cover
	// the whole run.
	r := NewRedactor("aaa")
	if got := r.Redact("aaaaa"); got != "<redacted, len=5>" {
		t.Fatalf("overlapping same secret = %q", got)
	}
}

func TestRedactPlaceholderCollisionFailsClosed(t *testing.T) {
	// The placeholder "<redacted, len=N>" literally contains "redacted",
	// "len", and the digits of N. A loaded secret equal to any of those
	// must NOT survive in the output via the placeholder. Fail-closed:
	// the colliding span drops to empty. The authoritative assertion is
	// NoLeakAssertion over the redacted output itself.
	cases := []struct {
		name    string
		secrets []string
		input   string
	}{
		{"secret len", []string{"len"}, "the len value"},
		{"secret redacted", []string{"redacted"}, "say redacted now"},
		{"secret + placeholder-substring secret", []string{"token-value", "len"}, "token-value"},
		{"numeric secret colliding with len=N", []string{"11"}, "abcdefghijk"}, // len 11 → "len=11"
		{"angle bracket secret", []string{">"}, "a>b>c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRedactor(tc.secrets...)
			got := r.Redact(tc.input)
			if err := NoLeakAssertion([]byte(got), tc.secrets...); err != nil {
				t.Fatalf("redacted output still leaks: input=%q got=%q (%v)", tc.input, got, err)
			}
		})
	}
}

func TestAddDedupAndEmptyIgnored(t *testing.T) {
	r := NewRedactor("", "tok") // empty ignored at construction
	r.Add("")                   // ignored
	r.Add("tok")                // duplicate no-op
	r.Add("later")              // post-construction secret
	if got := r.Redact("tok and later"); got != "<redacted, len=3> and <redacted, len=5>" {
		t.Fatalf("Add = %q", got)
	}
	r.mu.RLock()
	n := len(r.secrets)
	r.mu.RUnlock()
	if n != 2 {
		t.Fatalf("secrets = %d, want 2 (no empty, no dup)", n)
	}
}

func TestRedactHeaders(t *testing.T) {
	r := NewRedactor("embedded-secret")
	h := http.Header{
		"Authorization":  {"Bearer abcdef"},
		"Cookie":         {"a=1", "b=2"},
		"Set-Cookie":     {"sid=xyz"},
		"X-Trace":        {"prefix embedded-secret suffix"},
		"X-Clean":        {"nothing sensitive"},
		"X-Raw-Auth-Key": {"value"}, // arbitrary header, not in always-set
	}
	r.RedactHeaders(h)

	if h.Get("Authorization") != "<redacted, len=13>" {
		t.Fatalf("Authorization = %q", h.Get("Authorization"))
	}
	if h["Cookie"][0] != "<redacted, len=3>" || h["Cookie"][1] != "<redacted, len=3>" {
		t.Fatalf("Cookie = %v", h["Cookie"])
	}
	if h.Get("Set-Cookie") != "<redacted, len=7>" {
		t.Fatalf("Set-Cookie = %q", h.Get("Set-Cookie"))
	}
	if h.Get("X-Trace") != "prefix <redacted, len=15> suffix" {
		t.Fatalf("X-Trace = %q", h.Get("X-Trace"))
	}
	if h.Get("X-Clean") != "nothing sensitive" {
		t.Fatalf("X-Clean must be untouched, got %q", h.Get("X-Clean"))
	}
	if h["X-Raw-Auth-Key"][0] != "value" {
		t.Fatalf("non-secret arbitrary header changed: %v", h["X-Raw-Auth-Key"])
	}

	// Non-canonical key in the always-redact set still hits. The raw key
	// is built via a variable so it bypasses http.Header canonicalisation
	// (exercising RedactHeaders' case-insensitive match), which is the
	// whole point of this sub-case.
	rawKey := "authorization"
	h2 := http.Header{}
	h2[rawKey] = []string{"raw"}
	r.RedactHeaders(h2)
	if h2[rawKey][0] != "<redacted, len=3>" {
		t.Fatalf("non-canonical authorization = %v", h2[rawKey])
	}

	r.RedactHeaders(nil) // no panic

	// Always-set placeholder must also fail closed when a secret collides
	// with the placeholder literal (e.g. secret "redacted").
	rc := NewRedactor("redacted")
	hc := http.Header{"Authorization": {"Bearer something"}}
	rc.RedactHeaders(hc)
	if err := NoLeakAssertion([]byte(strings.Join(hc["Authorization"], "")), "redacted"); err != nil {
		t.Fatalf("always-set header leaked via placeholder: %v (%q)", err, hc["Authorization"])
	}
}

type shortWriter struct{ wrote int }

func (s *shortWriter) Write(p []byte) (int, error) { s.wrote = len(p) - 1; return s.wrote, nil }

type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, errors.New("downstream boom") }

func TestRedactWriter(t *testing.T) {
	r := NewRedactor("hunter2")

	var buf strings.Builder
	w := r.RedactWriter(&buf)
	in := []byte("password=hunter2\n")
	n, err := w.Write(in)
	if err != nil {
		t.Fatalf("Write err: %v", err)
	}
	if n != len(in) {
		t.Fatalf("Write n = %d, want %d (caller's logical length)", n, len(in))
	}
	if buf.String() != "password=<redacted, len=7>\n" {
		t.Fatalf("downstream = %q", buf.String())
	}

	// Downstream error → (0, err), never a fictional partial count.
	n, err = r.RedactWriter(errWriter{}).Write([]byte("x"))
	if n != 0 || err == nil {
		t.Fatalf("downstream error = (%d,%v), want (0, non-nil)", n, err)
	}

	// Short underlying write, no error → (0, io.ErrShortWrite).
	n, err = r.RedactWriter(&shortWriter{}).Write([]byte("clean text"))
	if n != 0 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write = (%d,%v), want (0, io.ErrShortWrite)", n, err)
	}
}

func TestRedactWriterCrossWriteSplitIsKnownGap(t *testing.T) {
	// Documented limitation: a secret split across two Write calls is not
	// caught (no internal buffering). Asserting it keeps the gap
	// intentional rather than an accidental regression.
	r := NewRedactor("SPLITME")
	var buf strings.Builder
	w := r.RedactWriter(&buf)
	_, _ = w.Write([]byte("SPL"))
	_, _ = w.Write([]byte("ITME"))
	if buf.String() != "SPLITME" {
		t.Fatalf("expected unscrubbed split (known gap), got %q", buf.String())
	}
}

func TestNoLeakAssertion(t *testing.T) {
	if err := NoLeakAssertion([]byte("totally clean output"), "secret", "tok"); err != nil {
		t.Fatalf("clean output → %v, want nil", err)
	}

	const canary = "xoxb-distinctive-canary-0001"
	err := NoLeakAssertion([]byte("log line with xoxb-distinctive-canary-0001 in it"), "decoy", canary)
	if err == nil {
		t.Fatal("leak not detected")
	}
	msg := err.Error()
	if strings.Contains(msg, canary) {
		t.Fatalf("error must NOT contain the secret value: %q", msg)
	}
	// Named by ordinal + length only.
	if !strings.Contains(msg, "secret #2") || !strings.Contains(msg, fmt.Sprintf("len=%d", len(canary))) {
		t.Fatalf("error must name secret by ordinal+length: %q", msg)
	}

	// Multiple leaks aggregated; empty secret skipped (no false positive).
	err = NoLeakAssertion([]byte("aaa bbb"), "aaa", "", "bbb")
	if err == nil || !strings.Contains(err.Error(), "secret #1") || !strings.Contains(err.Error(), "secret #3") {
		t.Fatalf("aggregate = %v", err)
	}
}

func TestRedactorConcurrent(t *testing.T) {
	r := NewRedactor("seed")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); r.Add(fmt.Sprintf("tok-%d", i)) }(i)
		go func() { defer wg.Done(); _ = r.Redact("logging seed and tok-3 here") }()
	}
	wg.Wait()
	// The race detector is the real assertion; also confirm Add applied
	// under contention (seed + 16 distinct toks, deduped).
	if got := r.Redact("seed"); got != "<redacted, len=4>" {
		t.Fatalf("seed not loaded after concurrent Add: %q", got)
	}
}

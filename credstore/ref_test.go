package credstore

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantService string
		wantProfile string
		wantErr     error // sentinel for errors.Is; nil means success
	}{
		{"valid simple", "atlassian-cli/default", "atlassian-cli", "default", nil},
		{"valid underscores", "slack_chat_api/work_account", "slack_chat_api", "work_account", nil},
		{"valid hyphens digits", "newrelic-cli/staging-2", "newrelic-cli", "staging-2", nil},
		{"empty", "", "", "", ErrRefEmpty},
		{"empty service", "/default", "", "", ErrRefEmpty},
		{"empty profile", "atlassian-cli/", "", "", ErrRefEmpty},
		{"no slash", "atlassiancli", "", "", ErrRefSegmentCount},
		{"two slashes", "atlassian-cli/default/api_token", "", "", ErrRefSegmentCount},
		{"trailing double slash", "svc//", "", "", ErrRefSegmentCount},
		{"space in profile", "svc/my profile", "", "", ErrRefInvalidChar},
		{"dot in service", "svc.io/default", "", "", ErrRefInvalidChar},
		{"at sign", "svc/rian@host", "", "", ErrRefInvalidChar},
		{"unicode", "svc/dÃ©faut", "", "", ErrRefInvalidChar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, prof, err := ParseRef(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseRef(%q) err = %v, want errors.Is %v", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q) unexpected err = %v", tt.in, err)
			}
			if svc != tt.wantService || prof != tt.wantProfile {
				t.Fatalf("ParseRef(%q) = (%q,%q), want (%q,%q)", tt.in, svc, prof, tt.wantService, tt.wantProfile)
			}
		})
	}
}

func TestFormatRef(t *testing.T) {
	tests := []struct {
		name    string
		service string
		profile string
		want    string
		wantErr error
	}{
		{"valid", "atlassian-cli", "default", "atlassian-cli/default", nil},
		{"empty service", "", "default", "", ErrRefEmpty},
		{"empty profile", "atlassian-cli", "", "", ErrRefEmpty},
		{"invalid service char", "svc.io", "default", "", ErrRefInvalidChar},
		{"invalid profile char", "svc", "pro file", "", ErrRefInvalidChar},
		{"slash in segment", "a/b", "default", "", ErrRefInvalidChar},
		// Pin validation order: empty is checked before invalid-char, and
		// service is checked before profile. A reorder must fail these.
		{"both empty", "", "", "", ErrRefEmpty},
		{"empty service beats invalid profile", "", "bad char", "", ErrRefEmpty},
		{"invalid service beats empty profile", "bad.svc", "", "", ErrRefInvalidChar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatRef(tt.service, tt.profile)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("FormatRef(%q,%q) err = %v, want errors.Is %v", tt.service, tt.profile, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FormatRef(%q,%q) unexpected err = %v", tt.service, tt.profile, err)
			}
			if got != tt.want {
				t.Fatalf("FormatRef(%q,%q) = %q, want %q", tt.service, tt.profile, got, tt.want)
			}
		})
	}
}

func TestDefaultRef(t *testing.T) {
	got, err := DefaultRef("atlassian-cli")
	if err != nil {
		t.Fatalf("DefaultRef unexpected err = %v", err)
	}
	if got != "atlassian-cli/default" {
		t.Fatalf("DefaultRef = %q, want %q", got, "atlassian-cli/default")
	}
	if _, err := DefaultRef("bad.service"); !errors.Is(err, ErrRefInvalidChar) {
		t.Fatalf("DefaultRef(bad) err = %v, want ErrRefInvalidChar", err)
	}
	if _, err := DefaultRef(""); !errors.Is(err, ErrRefEmpty) {
		t.Fatalf("DefaultRef(\"\") err = %v, want ErrRefEmpty", err)
	}
}

func TestRefErrorMessageHasNoSecretButNamesRef(t *testing.T) {
	_, _, err := ParseRef("svc/bad char")
	var re *RefError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want *RefError", err)
	}
	if re.Segment != "profile" {
		t.Fatalf("Segment = %q, want %q", re.Segment, "profile")
	}
	// The message must name the (non-secret, §1.2) ref so it is actionable.
	msg := re.Error()
	if !strings.Contains(msg, "svc/bad char") {
		t.Fatalf("Error() = %q, expected it to name the ref %q", msg, "svc/bad char")
	}
}

var segmentCharset = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)

func TestEscapeRefSegment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"already safe", "atlassian-cli", "atlassian-cli"},
		{"digits and hyphen", "staging-2", "staging-2"},
		{"underscore doubles", "a_b", "a__b"},
		{"email", "rian@monitapp.io", "rian_x40monitapp_x2eio"},
		{"space", "a b", "a_x20b"},
		{"slash", "a/b", "a_x2fb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeRefSegment(tt.in)
			if got != tt.want {
				t.Fatalf("EscapeRefSegment(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !segmentCharset.MatchString(got) {
				t.Fatalf("EscapeRefSegment(%q) = %q, not within [A-Za-z0-9_-]*", tt.in, got)
			}
		})
	}
}

func TestEscapeRefSegmentUnicodeStaysInCharset(t *testing.T) {
	// Multi-byte input must still encode entirely within the charset, and a
	// non-empty input must produce non-empty output usable as a segment.
	got := EscapeRefSegment("dÃ©faut Ã±")
	if !segmentCharset.MatchString(got) {
		t.Fatalf("escaped unicode %q not within charset", got)
	}
	if got == "" {
		t.Fatal("non-empty input produced empty escape")
	}
	if _, err := FormatRef("svc", got); err != nil {
		t.Fatalf("escaped segment not accepted by FormatRef: %v", err)
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	pairs := []struct{ service, profile string }{
		{"atlassian-cli", "default"},
		{"slack-chat-api", "work-account"},
		{"newrelic_cli", "staging-2"},
		{"a", "b"},
	}
	for _, p := range pairs {
		ref, err := FormatRef(p.service, p.profile)
		if err != nil {
			t.Fatalf("FormatRef(%q,%q) err = %v", p.service, p.profile, err)
		}
		svc, prof, err := ParseRef(ref)
		if err != nil {
			t.Fatalf("ParseRef(%q) err = %v", ref, err)
		}
		if svc != p.service || prof != p.profile {
			t.Fatalf("round trip = (%q,%q), want (%q,%q)", svc, prof, p.service, p.profile)
		}
	}
}

func FuzzParseRef(f *testing.F) {
	seeds := []string{
		"", "a", "a/b", "/b", "a/", "a/b/c", "a//b",
		"atlassian-cli/default", "svc/rian@host", "a b/c d",
		"//", "///", "_/_", "-/-",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, ref string) {
		svc, prof, err := ParseRef(ref) // must never panic
		if err != nil {
			return
		}
		// On success: segments must be valid and slash-free, and the pair
		// must FormatRef back to exactly the input.
		if !validSegment(svc) || !validSegment(prof) {
			t.Fatalf("ParseRef(%q) returned invalid segments (%q,%q)", ref, svc, prof)
		}
		round, ferr := FormatRef(svc, prof)
		if ferr != nil {
			t.Fatalf("FormatRef(%q,%q) err = %v after successful ParseRef(%q)", svc, prof, ferr, ref)
		}
		if round != ref {
			t.Fatalf("round trip mismatch: ParseRef(%q) -> FormatRef -> %q", ref, round)
		}
	})
}

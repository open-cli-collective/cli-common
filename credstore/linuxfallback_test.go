package credstore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestClassifySecretServiceErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ssClass
	}{
		{"nil is reachable", nil, ssReachable},
		{"service unknown", &dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}, ssUnavailable},
		{"name has no owner", &dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}, ssUnavailable},
		{"spawn not found", &dbus.Error{Name: "org.freedesktop.DBus.Error.Spawn.ServiceNotFound"}, ssUnavailable},
		{"is locked", &dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}, ssDenied},
		{"no session", &dbus.Error{Name: "org.freedesktop.Secret.Error.NoSession"}, ssDenied},
		{"access denied", &dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"}, ssDenied},
		{"no reply is ambiguous", &dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"}, ssAmbiguous},
		{"unknown dbus name is ambiguous", &dbus.Error{Name: "org.example.Whatever"}, ssAmbiguous},
		{"session bus phrase", errors.New("dbus: couldn't determine address of session bus"), ssUnavailable},
		{"DBUS_SESSION_BUS_ADDRESS phrase", errors.New("dbus: DBUS_SESSION_BUS_ADDRESS not set"), ssUnavailable},
		{"bare ENOENT is NOT unavailable", errors.New("open /run/user/1000/bus: no such file or directory"), ssAmbiguous},
		{"locked session-bus phrase stays ambiguous", errors.New("session bus is locked and requires authentication"), ssAmbiguous},
		{"opaque error is ambiguous", errors.New("totally opaque"), ssAmbiguous},
		{"wrapped typed error still classified", fmt.Errorf("probe failed: %w", &dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}), ssDenied},
		// godbus returns dbus.Error by value at real call sites, so the
		// value-type errors.As branch must classify too (not only the
		// *dbus.Error pointer form).
		{"value dbus.Error unavailable", dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}, ssUnavailable},
		{"value dbus.Error denied", dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}, ssDenied},
		{"wrapped value dbus.Error", fmt.Errorf("probe: %w", dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}), ssUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySecretServiceErr(tc.err); got != tc.want {
				t.Fatalf("classifySecretServiceErr(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestLinuxAutoBackend(t *testing.T) {
	const envVar = "ATLASSIAN_CLI_KEYRING_BACKEND"
	if b, err := linuxAutoBackend(func() error { return nil }, envVar); err != nil || b != BackendSecretService {
		t.Fatalf("reachable → (%q,%v), want (secret-service,nil)", b, err)
	}
	if b, err := linuxAutoBackend(func() error {
		return &dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}
	}, envVar); err != nil || b != BackendFile {
		t.Fatalf("unavailable → (%q,%v), want (file,nil)", b, err)
	}

	for _, tc := range []struct {
		name    string
		probe   func() error
		mustSay string
	}{
		{"denied", func() error { return &dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"} }, "locked or denied"},
		{"ambiguous", func() error { return errors.New("opaque") }, "could not confirm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := linuxAutoBackend(tc.probe, envVar)
			if !errors.Is(err, ErrSecretServiceFailClosed) {
				t.Fatalf("err = %v, want ErrSecretServiceFailClosed", err)
			}
			if b != "" {
				t.Fatalf("no backend may be returned on fail-closed, got %q", b)
			}
			msg := err.Error()
			if !strings.Contains(msg, "secret-service") || !strings.Contains(msg, "list keys") {
				t.Fatalf("error must name backend + probe op: %v", err)
			}
			// Remediation must be copy-pasteable: the resolved env var,
			// never the un-substituted "<SERVICE>" template.
			if !strings.Contains(msg, envVar) || strings.Contains(msg, "<SERVICE>") {
				t.Fatalf("error must name the resolved env var %q and not the placeholder: %v", envVar, err)
			}
			for _, tool := range []string{"gnome-keyring-daemon", "seahorse", "kwalletmanager"} {
				if !strings.Contains(msg, tool) {
					t.Fatalf("error must offer remediation %q: %v", tool, err)
				}
			}
			if !strings.Contains(msg, tc.mustSay) {
				t.Fatalf("error must distinguish the class (%q): %v", tc.mustSay, err)
			}
		})
	}
}

// recordingProbe returns a probe func plus a pointer to a bool that is
// set true if the probe is ever invoked.
func recordingProbe(ret error) (func(string, func(string) string) error, *bool) {
	called := new(bool)
	return func(string, func(string) string) error {
		*called = true
		return ret
	}, called
}

func TestResolveBackendTriggerMatrix(t *testing.T) {
	const svc = "rb-svc"
	dbErr := func(n string) error { return &dbus.Error{Name: n} }

	tests := []struct {
		name      string
		opts      *Options
		env       map[string]string
		goos      string
		probeRet  error
		wantKind  Backend
		wantSrc   Source
		wantErrIs error // nil = success
		wantProbe bool  // must probe have been called?
	}{
		{"linux auto reachable", &Options{}, nil, "linux", nil, BackendSecretService, SourceAuto, nil, true},
		{"linux auto unavailable→file", &Options{}, nil, "linux", dbErr("org.freedesktop.DBus.Error.ServiceUnknown"), BackendFile, SourceAuto, nil, true},
		{"linux auto denied→fail closed", &Options{}, nil, "linux", dbErr("org.freedesktop.Secret.Error.IsLocked"), "", "", ErrSecretServiceFailClosed, true},
		{"linux auto ambiguous→fail closed", &Options{}, nil, "linux", errors.New("opaque"), "", "", ErrSecretServiceFailClosed, true},
		{"explicit secret-service: no probe", &Options{Backend: BackendSecretService}, nil, "linux", errors.New("would fail"), BackendSecretService, SourceExplicit, nil, false},
		{"env secret-service: no probe", &Options{}, map[string]string{"RB_SVC_KEYRING_BACKEND": "secret-service"}, "linux", errors.New("would fail"), BackendSecretService, SourceEnv, nil, false},
		{"config secret-service: no probe", &Options{ConfigBackend: BackendSecretService}, nil, "linux", errors.New("would fail"), BackendSecretService, SourceConfig, nil, false},
		{"darwin auto: no probe", &Options{}, nil, "darwin", errors.New("would fail"), BackendKeychain, SourceAuto, nil, false},
		{"windows auto: no probe", &Options{}, nil, "windows", errors.New("would fail"), BackendWinCred, SourceAuto, nil, false},
		{"explicit file: no probe", &Options{Backend: BackendFile}, nil, "linux", errors.New("would fail"), BackendFile, SourceExplicit, nil, false},
		{"explicit memory: no probe", &Options{Backend: BackendMemory}, nil, "linux", errors.New("would fail"), BackendMemory, SourceExplicit, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			probe, called := recordingProbe(tc.probeRet)
			kind, src, err := resolveBackend(svc, tc.opts, envFrom(tc.env), tc.goos, probe)
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("err = %v, want errors.Is %v", err, tc.wantErrIs)
				}
				if kind != "" || src != "" {
					t.Fatalf("nothing resolved on error, got (%q,%q)", kind, src)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if kind != tc.wantKind || src != tc.wantSrc {
					t.Fatalf("= (%q,%q), want (%q,%q)", kind, src, tc.wantKind, tc.wantSrc)
				}
			}
			if *called != tc.wantProbe {
				t.Fatalf("probe called = %v, want %v", *called, tc.wantProbe)
			}
		})
	}
}

// TestOpenWithDepsLinuxAutoFallback drives the real Open path
// (openWithDeps) for the only construction worth e2e-ing: linux auto +
// unavailable Secret Service → the encrypted file backend. The file
// backend is CI-safe on every OS; getenv/goos/probe are injected so no
// D-Bus is touched.
func TestOpenWithDepsLinuxAutoFallback(t *testing.T) {
	const svc = "credstore-llbfb"
	unavailable := func(string, func(string) string) error {
		return &dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}
	}

	t.Run("with passphrase → functional file backend", func(t *testing.T) {
		dir := t.TempDir()
		env := envFrom(map[string]string{
			"XDG_DATA_HOME":                      dir,
			"CREDSTORE_LLBFB_KEYRING_PASSPHRASE": "pp",
		})
		s, err := openWithDeps(svc, &Options{AllowedKeys: []string{"tok"}}, env, "linux", unavailable)
		if err != nil {
			t.Fatalf("openWithDeps: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		if b, src := s.Backend(); b != BackendFile || src != SourceAuto {
			t.Fatalf("Backend() = (%q,%q), want (file,auto)", b, src)
		}
		if err := s.Set("default", "tok", "v", WithOverwrite()); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if got, err := s.Get("default", "tok"); err != nil || got != "v" {
			t.Fatalf("Get = (%q,%v), want (v,nil)", got, err)
		}
	})

	t.Run("no passphrase → fail closed, not a silent downgrade", func(t *testing.T) {
		env := envFrom(map[string]string{"XDG_DATA_HOME": t.TempDir()})
		_, err := openWithDeps(svc, &Options{}, env, "linux", unavailable)
		if !errors.Is(err, ErrFilePassphraseRequired) {
			t.Fatalf("err = %v, want ErrFilePassphraseRequired", err)
		}
	})

	t.Run("denied → fail closed through Open", func(t *testing.T) {
		denied := func(string, func(string) string) error {
			return &dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}
		}
		_, err := openWithDeps(svc, &Options{}, envFrom(nil), "linux", denied)
		if !errors.Is(err, ErrSecretServiceFailClosed) {
			t.Fatalf("err = %v, want ErrSecretServiceFailClosed", err)
		}
	})

	t.Run("ambiguous → fail closed through Open", func(t *testing.T) {
		ambiguous := func(string, func(string) string) error {
			return errors.New("opaque probe failure")
		}
		_, err := openWithDeps(svc, &Options{}, envFrom(nil), "linux", ambiguous)
		if !errors.Is(err, ErrSecretServiceFailClosed) {
			t.Fatalf("err = %v, want ErrSecretServiceFailClosed", err)
		}
	})
}

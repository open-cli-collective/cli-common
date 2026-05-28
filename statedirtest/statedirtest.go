// Package statedirtest provides the single hermetic environment helper for
// state-component tests (working-with-state.md §3.1). It points the full
// 8-var env set at a per-test temp dir so resolution for config and cache
// (the pillars with implemented resolvers today) never reaches the
// developer's real directories on any OS. Env coverage for the data
// pillar is ready ahead of the resolver API — XDG_STATE_HOME and
// %LOCALAPPDATA% are already pinned so that Data() (when it lands —
// working-with-state.md §7 rollout step 7) will be hermetic from day
// one. Secrets are keyring-backed and location-independent. (The set
// was 7-var at INT-310 delivery and grew to 8 on 2026-05-28 when the
// Data pillar's Path A backing added XDG_STATE_HOME.)
//
// HOME-only isolation is a Windows real-dir leak: os.UserConfigDir reads
// %AppData% and os.UserCacheDir reads %LocalAppData%, neither of which is
// derived from %USERPROFILE%/HOME. The data pillar (§5.2) reads
// %LOCALAPPDATA% on Windows and $XDG_STATE_HOME on Linux; both are pinned
// here. This helper is the one definition of the list; no CLI re-derives it.
//
// Not usable under t.Parallel: t.Setenv mutates process-global env and Go
// panics if it is called on a parallel test. Per-instance overrides are the
// parallel-safe alternative and remain a per-port choice.
package statedirtest

import (
	"path/filepath"
	"testing"
)

// envSubdir maps each isolated env var to a subdirectory of the test temp
// root. HOME and USERPROFILE share one logical home; the rest are distinct so
// config/cache/data cannot collide. XDG_DATA_HOME is included alongside
// XDG_STATE_HOME because XDG-aware dev envs may set either; both must be
// pinned so neither bleeds into a Linux test run.
var envSubdir = map[string]string{
	"HOME":            "home",
	"USERPROFILE":     "home",
	"AppData":         "appdata",
	"LocalAppData":    "localappdata",
	"XDG_CONFIG_HOME": "xdgconfig",
	"XDG_CACHE_HOME":  "xdgcache",
	"XDG_DATA_HOME":   "xdgdata",
	"XDG_STATE_HOME":  "xdgstate",
}

// Hermetic isolates the full §3.1 8-var env set under t.TempDir() and returns
// the temp root. Every override is restored by t.Setenv's own cleanup.
//
// Must NOT be called from a test that has called t.Parallel: t.Setenv mutates
// process-global env and Go panics in that case. Use a per-instance override
// for parallel tests instead.
func Hermetic(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for env, sub := range envSubdir {
		t.Setenv(env, filepath.Join(root, sub))
	}
	return root
}

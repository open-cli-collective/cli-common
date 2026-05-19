// Package statedirtest provides the single hermetic environment helper for
// state-component tests (working-with-state.md §3.1). It points the full
// 7-var env set at a per-test temp dir so os.UserConfigDir / os.UserCacheDir
// never resolve to the developer's real directories on any OS.
//
// HOME-only isolation is a Windows real-dir leak: os.UserConfigDir reads
// %AppData% and os.UserCacheDir reads %LocalAppData%, neither of which is
// derived from %USERPROFILE%/HOME. This helper is the one definition of the
// list; no CLI re-derives it.
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
// config/cache/data cannot collide.
var envSubdir = map[string]string{
	"HOME":            "home",
	"USERPROFILE":     "home",
	"AppData":         "appdata",
	"LocalAppData":    "localappdata",
	"XDG_CONFIG_HOME": "xdgconfig",
	"XDG_CACHE_HOME":  "xdgcache",
	"XDG_DATA_HOME":   "xdgdata",
}

// Hermetic isolates the full §3.1 7-var env set under t.TempDir() and returns
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

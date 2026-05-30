package statedir

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDirForPlatforms(t *testing.T) {
	tests := []struct {
		name string
		goos string
		env  map[string]string
		home string
		want string
	}{
		{
			name: "linux xdg state",
			goos: "linux",
			env:  map[string]string{"XDG_STATE_HOME": "/xdgstate"},
			home: "/home/rian",
			want: filepath.Join("/xdgstate", "cr"),
		},
		{
			name: "linux home fallback",
			goos: "linux",
			env:  map[string]string{},
			home: "/home/rian",
			want: filepath.Join("/home/rian", ".local", "state", "cr"),
		},
		{
			name: "darwin application support data",
			goos: "darwin",
			env:  map[string]string{},
			home: "/Users/rian",
			want: filepath.Join("/Users/rian", "Library", "Application Support", "cr", "data"),
		},
		{
			name: "windows local app data",
			goos: "windows",
			env:  map[string]string{"LocalAppData": "/localappdata"},
			home: "/unused",
			want: filepath.Join("/localappdata", "cr", "data"),
		},
		{
			name: "windows uppercase local app data",
			goos: "windows",
			env:  map[string]string{"LOCALAPPDATA": "/localappdata"},
			home: "/unused",
			want: filepath.Join("/localappdata", "cr", "data"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dataDirFor("cr", tt.goos, mapGetter(tt.env), homeFunc(tt.home, nil))
			if err != nil {
				t.Fatalf("dataDirFor: %v", err)
			}
			if got != tt.want {
				t.Fatalf("dataDirFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDataDirForFailurePaths(t *testing.T) {
	homeErr := errors.New("home unavailable")
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		home    string
		homeErr error
		wantErr string
	}{
		{
			name:    "linux relative xdg state",
			goos:    "linux",
			env:     map[string]string{"XDG_STATE_HOME": "relative"},
			home:    "/home/rian",
			wantErr: "XDG_STATE_HOME",
		},
		{
			name:    "linux home error",
			goos:    "linux",
			env:     map[string]string{},
			homeErr: homeErr,
			wantErr: "home unavailable",
		},
		{
			name:    "darwin home error",
			goos:    "darwin",
			env:     map[string]string{},
			homeErr: homeErr,
			wantErr: "home unavailable",
		},
		{
			name:    "windows missing local app data",
			goos:    "windows",
			env:     map[string]string{},
			home:    "/unused",
			wantErr: "LocalAppData",
		},
		{
			name:    "unsupported goos",
			goos:    "plan9",
			env:     map[string]string{},
			home:    "/home/rian",
			wantErr: "unsupported GOOS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dataDirFor("cr", tt.goos, mapGetter(tt.env), homeFunc(tt.home, tt.homeErr))
			if err == nil {
				t.Fatalf("dataDirFor error = nil, want substring %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("dataDirFor error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func mapGetter(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func homeFunc(home string, err error) func() (string, error) {
	return func() (string, error) {
		return home, err
	}
}

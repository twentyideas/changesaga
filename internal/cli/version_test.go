package cli

import (
	"runtime/debug"
	"testing"
)

func TestVersionStringUsesInjectedMetadata(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.8.7"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Key: "vcs.time", Value: "2025-01-02T03:04:05Z"},
		},
	}
	want := "1.2.3 (0123456789ab) built 2026-02-03T04:05:06Z"
	if got := versionString("1.2.3", "0123456789ab", "2026-02-03T04:05:06Z", info); got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
}

func TestVersionStringUsesModuleBuildInfoForGoInstall(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.4.2"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
		},
	}
	want := "1.4.2 (0123456789ab) built 2026-01-02T03:04:05Z"
	if got := versionString("0.2.0-dev", "", "", info); got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
}

func TestVersionStringKeepsDevelopmentDefaultForLocalBuild(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	if got := versionString("0.2.0-dev", "", "", info); got != "0.2.0-dev" {
		t.Fatalf("versionString() = %q, want local development version", got)
	}
}

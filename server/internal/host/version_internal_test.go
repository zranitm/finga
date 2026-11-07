package host

import (
	"runtime/debug"
	"testing"
)

func TestTheVersionFallsBackToTheRecordedCommit(t *testing.T) {
	commit := func(rev, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}}
	}
	tests := []struct {
		name   string
		linked string
		info   *debug.BuildInfo
		want   string
	}{
		{"a linked version wins", "v1.2.0-3-gabc", commit("0123456789abcdef0123", "false"), "v1.2.0-3-gabc"},
		{"a dev build names its commit", "dev", commit("0123456789abcdef0123", "false"), "0123456789ab"},
		{"with changes not committed", "dev", commit("0123456789abcdef0123", "true"), "0123456789ab-dirty"},
		{"a build outside git stays dev", "dev", &debug.BuildInfo{}, "dev"},
		{"no build info stays dev", "dev", nil, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionOf(tt.linked, tt.info); got != tt.want {
				t.Fatalf("version %q, want %q", got, tt.want)
			}
		})
	}
}

package cli_test

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sikhlana/pkg/cli"
)

func TestResolveBuildInfo(t *testing.T) {
	vcs := map[string]string{
		"vcs.revision": "abc123",
		"vcs.time":     "2026-10-16T09:00:00Z",
		"vcs.modified": "false",
	}

	tests := []struct {
		name    string
		linked  cli.BuildInfo
		stamped *debug.BuildInfo
		want    cli.BuildInfo
	}{
		{
			name:    "link-time values win",
			linked:  cli.BuildInfo{Version: "1.0.0-rc1", Revision: "def456", Time: "yesterday"},
			stamped: stamped("v1.2.3", vcs),
			want:    cli.BuildInfo{Version: "1.0.0-rc1", Revision: "def456", Time: "yesterday"},
		},
		{
			name:    "stamped values fill the rest",
			linked:  cli.BuildInfo{Version: "1.0.0-rc1"},
			stamped: stamped("v1.2.3", vcs),
			want:    cli.BuildInfo{Version: "1.0.0-rc1", Revision: "abc123", Time: "2026-10-16T09:00:00Z"},
		},
		{
			name:    "stamped values alone",
			stamped: stamped("v1.2.3", vcs),
			want:    cli.BuildInfo{Version: "v1.2.3", Revision: "abc123", Time: "2026-10-16T09:00:00Z"},
		},
		{
			name:    "devel version is unset",
			stamped: stamped("(devel)", vcs),
			want:    cli.BuildInfo{Version: "dev", Revision: "abc123", Time: "2026-10-16T09:00:00Z"},
		},
		{
			name:    "no VCS keys",
			linked:  cli.BuildInfo{Version: "1.0.0"},
			stamped: stamped("(devel)", nil),
			want:    cli.BuildInfo{Version: "1.0.0"},
		},
		{
			name: "no stamped info",
			want: cli.BuildInfo{Version: "dev"},
		},
		{
			name:    "modified",
			stamped: stamped("v1.2.3+dirty", map[string]string{"vcs.modified": "true"}),
			want:    cli.BuildInfo{Version: "v1.2.3+dirty", Modified: true},
		},
		{
			name:   "modified ignores link-time values",
			linked: cli.BuildInfo{Modified: true},
			want:   cli.BuildInfo{Version: "dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cli.ResolveBuildInfo(tt.linked, tt.stamped))
		})
	}
}

func TestReadBuildInfo(t *testing.T) {
	assert.NotEmpty(t, cli.ReadBuildInfo().Version)
}

func TestBuildInfoString(t *testing.T) {
	tests := []struct {
		name string
		info cli.BuildInfo
		want string
	}{
		{
			name: "all details",
			info: cli.BuildInfo{Version: "v1.2.3", Revision: "abc123", Time: "2026-10-16T09:00:00Z", Modified: true},
			want: "v1.2.3 (abc123, 2026-10-16T09:00:00Z, modified)",
		},
		{
			name: "revision only",
			info: cli.BuildInfo{Version: "v1.2.3", Revision: "abc123"},
			want: "v1.2.3 (abc123)",
		},
		{
			name: "modified only",
			info: cli.BuildInfo{Version: "v1.2.3", Modified: true},
			want: "v1.2.3 (modified)",
		},
		{
			name: "no details",
			info: cli.BuildInfo{Version: "dev"},
			want: "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.String())
		})
	}
}

func stamped(version string, settings map[string]string) *debug.BuildInfo {
	info := &debug.BuildInfo{Main: debug.Module{Version: version}}

	for key, value := range settings {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
	}

	return info
}

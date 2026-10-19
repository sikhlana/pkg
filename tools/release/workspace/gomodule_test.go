package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func TestModuleTagAndFile(t *testing.T) {
	m := workspace.Module{Dir: "http/server", Path: fixture.ModulePrefix + "/http/server"}

	assert.Equal(t, "http/server", m.TagPrefix())
	assert.Equal(t, "http/server/v0.2.0", m.Tag("0.2.0"))
	assert.Equal(t, "http/server/v0.2.0", m.Tag("v0.2.0"))
	assert.Equal(t, "http/server/VERSION", m.File(workspace.VersionFile))
}

func TestMajorVersionTagsAndOwnership(t *testing.T) {
	v1 := workspace.Module{Dir: "logger", Path: fixture.ModulePrefix + "/logger"}
	v2 := workspace.Module{Dir: "logger/v2", Path: fixture.ModulePrefix + "/logger/v2"}
	root := workspace.Module{Dir: "v3", Path: fixture.ModulePrefix + "/v3"}

	assert.Empty(t, v1.PathMajor())
	assert.Equal(t, "/v2", v2.PathMajor())

	assert.Equal(t, "logger", v2.TagPrefix(), "the go command drops the major directory from tags")
	assert.Equal(t, "logger/v2.1.0", v2.Tag("2.1.0"))
	assert.Equal(t, "v3.0.0", root.Tag("3.0.0"))

	assert.Equal(t, "v0.1.0", v1.FirstVersion())
	assert.Equal(t, "v2.0.0", v2.FirstVersion())

	tags := []string{"v0.1.0", "v1.4.0", "v2.0.0", "v2.1.0", "v3.0.0"}
	assert.Equal(t, []string{"v0.1.0", "v1.4.0"}, v1.Releases(tags))
	assert.Equal(t, []string{"v2.0.0", "v2.1.0"}, v2.Releases(tags))
	assert.True(t, v1.Owns("v1.4.0"))
	assert.False(t, v1.Owns("v2.0.0"))
	assert.False(t, v2.Owns("v1.4.0"))
}

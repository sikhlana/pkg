package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func TestLoad(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.3.0")
	f.AddModule("http/server", "")

	ws := load(t, f)

	assert.Equal(t, fixture.ModulePrefix, ws.RepoPath)
	assert.Equal(t, fixture.GoVersion, ws.GoVersion)
	assert.Equal(t, fixture.GoVersion, ws.WorkGoVersion)
	require.Len(t, ws.Modules, 2)

	assert.Equal(t, workspace.Module{
		Dir:       "http/server",
		Path:      fixture.ModulePrefix + "/http/server",
		GoVersion: fixture.GoVersion,
		HasReadme: true,
	}, ws.Modules[0])
	assert.Equal(t, workspace.Module{
		Dir:        "logger",
		Path:       fixture.ModulePrefix + "/logger",
		GoVersion:  fixture.GoVersion,
		Version:    "0.3.0\n",
		HasVersion: true,
		HasReadme:  true,
	}, ws.Modules[1])
}

func TestToolingIsNotALibraryModule(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Write("tools/release/go.mod", "module "+fixture.ModulePrefix+"/tools/release\n\ngo 1.27\n")

	ws := load(t, f)

	assert.Equal(t, []string{"logger"}, dirs(ws.Modules))
	assert.Equal(t, []string{"logger"}, ws.GoModDirs)
	assert.Empty(t, ws.Check())
}

func TestFindGoModDirsSkipsIgnoredDirectories(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Write("logger/testdata/mod/go.mod", "module example.com/fixture\n")
	f.Write("_scratch/go.mod", "module example.com/scratch\n")
	f.Write(".hidden/go.mod", "module example.com/hidden\n")

	ws := load(t, f)

	assert.Equal(t, []string{"logger"}, ws.GoModDirs)
	assert.Empty(t, ws.Check())
}

func TestOwnerAndNested(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("http", "0.1.0")
	f.AddModule("http/server", "0.1.0")
	f.AddModule("httpx", "0.1.0")

	ws := load(t, f)

	owner, ok := ws.Owner("http/server/server.go")
	require.True(t, ok)
	assert.Equal(t, "http/server", owner.Dir)

	owner, ok = ws.Owner("http/client.go")
	require.True(t, ok)
	assert.Equal(t, "http", owner.Dir)

	owner, ok = ws.Owner("httpx/x.go")
	require.True(t, ok)
	assert.Equal(t, "httpx", owner.Dir)

	_, ok = ws.Owner("README.md")
	assert.False(t, ok)

	_, ok = ws.Owner("http")
	assert.False(t, ok)

	assert.Equal(t, []string{"http/server"}, ws.Nested(ws.Modules[0]))
	assert.Empty(t, ws.Nested(ws.Modules[1]))
}

func TestParseUses(t *testing.T) {
	uses, err := workspace.ParseUses([]byte("go 1.27\n\nuse (\n\t./metrics\n\t./tools/release\n\t./http/server\n)\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"http/server", "metrics"}, uses)

	_, err = workspace.ParseUses([]byte("use (\n"))
	require.ErrorContains(t, err, "parsing go.work")
}

func TestLoadErrors(t *testing.T) {
	f := fixture.New(t)
	f.Remove("go.work")

	_, err := workspace.Load(f.Root)
	require.ErrorContains(t, err, "reading go.work")

	f.Write("go.work", "go 1.27\n")
	f.Remove(".go-version")

	_, err = workspace.Load(f.Root)
	require.ErrorContains(t, err, "reading .go-version")

	f.Write(".go-version", "1.27\n")
	f.Write("tools/release/go.mod", "module example.com/elsewhere\n\ngo 1.27\n")

	_, err = workspace.Load(f.Root)
	require.ErrorIs(t, err, workspace.ErrToolModule)

	f.Remove("tools/release/go.mod")

	_, err = workspace.Load(f.Root)
	require.ErrorContains(t, err, "reading tools/release/go.mod")
}

package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func load(t *testing.T, f fixture.Repo) workspace.Workspace {
	t.Helper()

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	return ws
}

func dirs(modules []workspace.Module) []string {
	var out []string

	for _, m := range modules {
		out = append(out, m.Dir)
	}

	return out
}

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

func TestToolsEntryInGoWork(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("tools/release", "")

	ws := load(t, f)

	assert.Empty(t, ws.Modules)
	assert.Equal(t, []report.Problem{{
		File:    "go.work",
		Message: "go.work lists tools/release, but tools/ holds repo tooling, not library modules",
	}}, ws.Check())
}

func TestModuleMissingFromGoWork(t *testing.T) {
	f := fixture.New(t)
	f.Write("cache/go.mod", "module "+fixture.ModulePrefix+"/cache\n\ngo 1.27\n")

	ws := load(t, f)

	assert.Empty(t, ws.Modules)
	assert.Equal(t, []report.Problem{{
		File:    "go.work",
		Message: "cache has a go.mod but isn't registered in go.work",
	}}, ws.Check())
}

func TestStaleGoWorkEntry(t *testing.T) {
	f := fixture.New(t)
	f.Use("old")

	ws := load(t, f)

	assert.Empty(t, ws.Modules)
	assert.Equal(t, []string{"old"}, ws.Uses)
	assert.Equal(t, []report.Problem{{
		File:    "go.work",
		Message: "go.work lists old, which has no go.mod",
	}}, ws.Check())
}

func TestGoVersionDrift(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Write("logger/go.mod", "module "+fixture.ModulePrefix+"/logger\n\ngo 1.26\n")
	f.Write("go.work", "go 1.26\n\nuse ./logger\n")

	ws := load(t, f)

	assert.Equal(t, []report.Problem{
		{File: "go.work", Message: "go.work declares go 1.26, want 1.27 from .go-version"},
		{
			File:    "logger/go.mod",
			Module:  "logger",
			Message: "logger/go.mod declares go 1.26, want 1.27 from .go-version",
		},
	}, ws.Check())
}

func TestGoPatchVersionMatches(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Write("logger/go.mod", "module "+fixture.ModulePrefix+"/logger\n\ngo 1.27.1\n")
	f.Write("go.work", "go 1.27.0\n\nuse ./logger\n")

	assert.Empty(t, load(t, f).Check(), "a dependency can force a patch release onto the go line")
}

func TestModulePathMatchesDirectory(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Write("logger/go.mod", "module "+fixture.ModulePrefix+"/log\n\ngo 1.27\n")

	assert.Equal(t, []report.Problem{{
		File:   "logger/go.mod",
		Module: "logger",
		Message: "logger/go.mod declares module " + fixture.ModulePrefix + "/log, want " +
			fixture.ModulePrefix + "/logger",
	}}, load(t, f).Check())
}

func TestMajorVersionDirectory(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "1.2.0")
	f.AddModule("logger/v2", "2.0.0")

	assert.Empty(t, load(t, f).Check())
}

func TestMajorVersionInPlaceIsRejected(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "2.0.0")
	f.Write("logger/go.mod", "module "+fixture.ModulePrefix+"/logger/v2\n\ngo 1.27\n")

	problems := load(t, f).Check()

	require.Len(t, problems, 1)
	assert.Equal(t, "logger/go.mod declares module "+fixture.ModulePrefix+"/logger/v2, want "+
		fixture.ModulePrefix+"/logger", problems[0].Message)
}

func TestInvalidModulePath(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger/v1", "1.0.0")

	problems := load(t, f).Check()

	require.Len(t, problems, 1)
	assert.Equal(t, "logger/v1/go.mod", problems[0].File)
	assert.Contains(t, problems[0].Message, "logger/v1/go.mod declares an invalid module path")
}

func TestMissingReadmeAndVersion(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "")
	f.Remove("logger/README.md")

	ws := load(t, f)

	assert.Equal(t, []report.Problem{
		{File: "logger/README.md", Module: "logger", Message: "logger has no README.md"},
		{File: "logger/VERSION", Module: "logger", Message: "logger has no VERSION file"},
	}, ws.Check())
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

func TestToolDirsSkipsWhatTheGoCommandSkips(t *testing.T) {
	root := t.TempDir()

	for _, dir := range []string{"release", "vet", "_attic", ".cache", "testdata"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, workspace.ToolsDir, dir), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, workspace.ToolsDir, dir, workspace.GoModFile), nil, 0o600))
	}

	require.NoError(t, os.MkdirAll(filepath.Join(root, workspace.ToolsDir, "notes"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, workspace.ToolsDir, "README.md"), nil, 0o600))

	dirs, err := workspace.ToolDirs(root)
	require.NoError(t, err)

	assert.Equal(t, []string{"tools/release", "tools/vet"}, dirs)
}

func TestToolDirsWithoutATools(t *testing.T) {
	dirs, err := workspace.ToolDirs(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, dirs)
}

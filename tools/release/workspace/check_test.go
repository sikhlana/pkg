package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/report"
)

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

package plan_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/plan"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

// setup returns a repository with logger, metrics and http/server committed
// on main, checked out on a feature branch.
func setup(t *testing.T) fixture.Repo {
	t.Helper()

	f := fixture.New(t)
	f.AddModule("logger", "0.3.0")
	f.AddModule("metrics", "0.1.0")
	f.AddModule("http", "0.1.0")
	f.AddModule("http/server", "0.1.0")
	f.Commit("Add modules")
	f.Git("switch", "--quiet", "--create", "feature")

	return f
}

// diff commits the working tree and returns the plan inputs for the branch.
func diff(t *testing.T, f fixture.Repo) (workspace.Workspace, plan.Change) {
	t.Helper()

	f.Commit("Change")

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	change, err := plan.Diff(t.Context(), repo, "main")
	require.NoError(t, err)

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	return ws, change
}

func dirs(modules []workspace.Module) []string {
	out := []string{}

	for _, m := range modules {
		out = append(out, m.Dir)
	}

	return out
}

func TestOneModuleChanged(t *testing.T) {
	f := setup(t)
	f.Write("logger/README.md", "# logger, reworded\n")
	f.Write("logger/extra.go", "package logger\n")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"logger"}, dirs(plan.Select(ws, change)))
	assert.Equal(t, []string{"logger"}, dirs(plan.Changed(ws, change)))
}

func TestRootConfigurationChanged(t *testing.T) {
	f := setup(t)
	f.Write(".golangci.yml", "version: \"2\"\n")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"http", "http/server", "logger", "metrics"}, dirs(plan.Select(ws, change)))
	assert.Empty(t, plan.Changed(ws, change), "root configuration needs no VERSION bump")
}

func TestRootConfigurationAndModuleChanged(t *testing.T) {
	f := setup(t)
	f.Write(".go-version", "1.27\n\n")
	f.Write("metrics/extra.go", "package metrics\n")

	ws, change := diff(t, f)

	assert.Len(t, plan.Select(ws, change), 4)
	assert.Equal(t, []string{"metrics"}, dirs(plan.Changed(ws, change)))
}

func TestNoModuleChanged(t *testing.T) {
	f := setup(t)
	f.Write("docs/release.md", "# Release\n")
	f.Write("tools/release/main.go", "package main\n")

	ws, change := diff(t, f)

	assert.Empty(t, plan.Select(ws, change))
	assert.Empty(t, plan.Changed(ws, change))
}

func TestNestedModuleOwnsItsFiles(t *testing.T) {
	f := setup(t)
	f.Write("http/server/handler.go", "package server\n")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"http/server"}, dirs(plan.Changed(ws, change)))
}

func TestMovedFileChangesBothModules(t *testing.T) {
	f := setup(t)
	f.Git("mv", "metrics/metrics.go", "logger/metrics.go")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"logger", "metrics"}, dirs(plan.Changed(ws, change)))
}

func TestCarvedOutNestedModuleChangesItsParent(t *testing.T) {
	f := setup(t)
	f.Write("logger/sub/sub.go", "package sub\n")
	f.Commit("Add logger/sub as a package of logger")
	f.Git("switch", "--quiet", "main")
	f.Git("merge", "--quiet", "--ff-only", "feature")
	f.Git("switch", "--quiet", "feature")
	f.Write("logger/sub/go.mod", "module "+fixture.ModulePrefix+"/logger/sub\n\ngo 1.27\n")
	f.Write("logger/sub/VERSION", "0.1.0\n")
	f.Write("logger/sub/README.md", "# sub\n")
	f.Use("logger/sub")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"logger", "logger/sub"}, dirs(plan.Changed(ws, change)),
		"logger loses package sub, so it changes too")
}

func TestDeletedNestedModuleLeavesItsParent(t *testing.T) {
	f := setup(t)
	f.Remove("http/server")
	f.Unuse("http/server")

	ws, change := diff(t, f)

	assert.Empty(t, plan.Changed(ws, change))
	assert.Len(t, plan.Select(ws, change), 3, "go.work changed, so every module is linted and tested")
}

func TestFoldedBackNestedModuleChangesItsParent(t *testing.T) {
	f := setup(t)
	f.Remove("http/server/go.mod")
	f.Remove("http/server/VERSION")
	f.Remove("http/server/README.md")
	f.Unuse("http/server")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"http"}, dirs(plan.Changed(ws, change)),
		"http gains package server, so it changes")
}

func TestDiffUsesMergeBase(t *testing.T) {
	f := setup(t)
	f.Write("logger/extra.go", "package logger\n")
	f.Commit("Change logger")
	f.Git("switch", "--quiet", "main")
	f.Write("metrics/extra.go", "package metrics\n")
	f.Commit("Change metrics on main")
	f.Git("switch", "--quiet", "feature")

	ws, change := diff(t, f)

	assert.Equal(t, []string{"logger"}, dirs(plan.Changed(ws, change)))
}

func TestMatrix(t *testing.T) {
	matrix, err := plan.Matrix([]workspace.Module{{Dir: "http/server"}, {Dir: "logger"}})
	require.NoError(t, err)
	assert.JSONEq(t, `["http/server","logger"]`, matrix)

	matrix, err = plan.Matrix(nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", matrix)
}

package check_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/check"
	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/gorelease"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

var errBuild = errors.New("building gorelease failed")

// setup returns a repository with logger at 0.3.0 committed on main and
// checked out on a feature branch.
func setup(t *testing.T) fixture.Repo {
	t.Helper()

	f := fixture.New(t)
	f.AddModule("logger", "0.3.0")
	f.Commit("Add logger")
	f.Git("switch", "--quiet", "--create", "feature")

	return f
}

// run commits the working tree and checks the branch against main, without
// the API comparison.
func run(t *testing.T, f fixture.Repo) check.Report {
	t.Helper()

	f.Commit("Change")

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	r, err := check.Run(t.Context(), check.Options{Repo: repo, Workspace: ws, Base: "main", Jobs: 4})
	require.NoError(t, err)

	return r
}

func messages(r check.Report) []string {
	var out []string

	for _, p := range r.Problems {
		out = append(out, p.Message)
	}

	return out
}

func TestMissingVersion(t *testing.T) {
	f := setup(t)
	f.Remove("logger/VERSION")

	r := run(t, f)

	assert.Equal(t, []report.Problem{{
		File: "logger/VERSION", Module: "logger", Message: "logger has no VERSION file",
	}}, r.Problems)
	assert.True(t, r.Failed())
}

func TestMalformedVersion(t *testing.T) {
	for _, value := range []string{"v0.2.0", "0.2"} {
		t.Run(value, func(t *testing.T) {
			f := setup(t)
			f.Write("logger/VERSION", value+"\n")

			r := run(t, f)

			require.Len(t, r.Problems, 1)
			assert.Equal(t, "logger/VERSION", r.Problems[0].File)
			assert.Contains(t, r.Problems[0].Message, "logger/VERSION")
			assert.Contains(t, r.Problems[0].Message, `"`+value+`"`)
		})
	}
}

func TestChangeWithoutBump(t *testing.T) {
	f := setup(t)
	f.Write("logger/README.md", "# logger, reworded\n")

	r := run(t, f)

	assert.Equal(t, []report.Problem{{
		File:    "logger/VERSION",
		Module:  "logger",
		Message: "logger changed without a VERSION bump: still 0.3.0, as on main",
	}}, r.Problems)
}

func TestVersionMovesBackwards(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.2.9\n")

	r := run(t, f)

	assert.Equal(t, []string{"logger VERSION 0.2.9 is not newer than 0.3.0 on main"}, messages(r))
}

func TestVersionBumped(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.3.1\n")

	r := run(t, f)

	assert.False(t, r.Failed())
	assert.Equal(t, []check.Module{{Dir: "logger", BaseVersion: "0.3.0", Version: "0.3.1"}}, r.Modules)
}

func TestNewModuleAtFirstVersion(t *testing.T) {
	f := setup(t)
	f.AddModule("metrics", "0.1.0")

	r := run(t, f)

	assert.False(t, r.Failed())
	assert.Equal(t, []check.Module{{Dir: "metrics", New: true, Version: "0.1.0"}}, r.Modules)
}

func TestNewModuleSkipsAhead(t *testing.T) {
	f := setup(t)
	f.AddModule("metrics", "0.2.0")

	r := run(t, f)

	assert.Equal(t, []string{"metrics is new, and a new module starts at 0.1.0, not 0.2.0"}, messages(r))
}

func TestTagAlreadyTaken(t *testing.T) {
	f := setup(t)
	f.Tag("logger/v0.3.1")
	f.Write("logger/VERSION", "0.3.1\n")

	r := run(t, f)

	assert.Equal(t, []report.Problem{{
		File: "logger/VERSION", Module: "logger", Message: "the tag logger/v0.3.1 already exists",
	}}, r.Problems)
}

func TestRootConfigurationNeedsNoBump(t *testing.T) {
	f := setup(t)
	f.Write(".golangci.yml", "version: \"2\"\n")

	r := run(t, f)

	assert.False(t, r.Failed())
	assert.Empty(t, r.Modules)
	assert.Contains(t, r.Markdown(), "No library module changed.")
}

func TestInvariantsRunWhateverChanged(t *testing.T) {
	f := setup(t)
	f.Commit("Nothing")
	f.Write("cache/go.mod", "module "+fixture.ModulePrefix+"/cache\n\ngo 1.27\n")
	f.Write("docs/notes.md", "notes\n")

	r := run(t, f)

	assert.Empty(t, r.Modules)
	assert.Equal(t, []string{"cache has a go.mod but isn't registered in go.work"}, messages(r))
	assert.Contains(t, r.Markdown(), "### Repository\n\n- cache has a go.mod but isn't registered in go.work\n")
}

func TestMixedResults(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.3.1\n")
	f.AddModule("metrics", "0.2.0")

	r := run(t, f)

	assert.True(t, r.Failed())

	summary := r.Markdown()
	assert.Contains(t, summary, "| logger | 0.3.0 | 0.3.1 | pass |\n")
	assert.Contains(t, summary,
		"| metrics | new | 0.2.0 | fail: metrics is new, and a new module starts at 0.1.0, not 0.2.0 |\n")

	var out bytes.Buffer

	env := map[string]string{"GITHUB_ACTIONS": "true"}

	report.New(&out, func(k string) string { return env[k] }).Errors(r.Problems)
	assert.Equal(t,
		"::error file=metrics/VERSION::metrics is new, and a new module starts at 0.1.0, not 0.2.0\n",
		out.String())
}

func TestChangedModuleInvariantShowsInItsRow(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.3.1\n")
	f.Remove("logger/README.md")

	r := run(t, f)

	summary := r.Markdown()
	assert.Contains(t, summary, "| logger | 0.3.0 | 0.3.1 | fail: logger has no README.md |\n")
	assert.NotContains(t, summary, "### Repository")
}

func TestMajorMismatch(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "2.0.0\n")

	r := run(t, f)

	assert.Equal(t, []report.Problem{{
		File:   "logger/VERSION",
		Module: "logger",
		Message: "logger VERSION 2.0.0 is a v2, but module path " + fixture.ModulePrefix +
			"/logger only takes v0 and v1 versions; v2 lives in logger/v2",
	}}, r.Problems)
}

func TestNewMajorVersionDirectory(t *testing.T) {
	f := setup(t)
	f.Tag("logger/v1.4.0")
	f.AddModule("logger/v2", "2.0.0")

	r := run(t, f)

	assert.False(t, r.Failed(), messages(r))
	assert.Equal(t, []check.Module{{Dir: "logger/v2", New: true, Version: "2.0.0"}}, r.Modules,
		"logger's own v1.4.0 doesn't make logger/v2 a released module")
}

func TestNewMajorVersionDirectoryAtTheWrongMajor(t *testing.T) {
	f := setup(t)
	f.AddModule("logger/v2", "0.1.0")

	r := run(t, f)

	assert.Equal(t, []string{
		"logger/v2 VERSION 0.1.0 is a v0, but module path " + fixture.ModulePrefix + "/logger/v2 only takes v2 versions",
	}, messages(r))
}

// restored returns a repository where metrics was released at 0.5.0, then
// removed from main, checked out on a feature branch that restores it at v.
func restored(t *testing.T, v string) fixture.Repo {
	t.Helper()

	f := fixture.New(t)
	f.AddModule("metrics", "0.5.0")
	f.Commit("Add metrics")
	f.Tag("metrics/v0.5.0")
	f.Remove("metrics")
	f.Unuse("metrics")
	f.Commit("Remove metrics")
	f.Git("switch", "--quiet", "--create", "feature")
	f.AddModule("metrics", v)

	return f
}

func TestRestoredModuleContinuesFromItsLastRelease(t *testing.T) {
	r := run(t, restored(t, "0.6.0"))

	assert.False(t, r.Failed(), messages(r))
	assert.Equal(t, []check.Module{{Dir: "metrics", Restored: true, BaseVersion: "0.5.0", Version: "0.6.0"}}, r.Modules)
	assert.Contains(t, r.Markdown(), "| metrics | 0.5.0 (last release) | 0.6.0 | pass |\n")
}

func TestRestoredModuleCantReuseAVersion(t *testing.T) {
	r := run(t, restored(t, "0.5.0"))

	assert.Equal(t, []string{
		"metrics was released up to 0.5.0 before it was removed, so VERSION must be newer, not 0.5.0",
	}, messages(r), "one problem: no redundant 'tag already exists'")
}

func TestVersionRuleFailureSkipsTheTagCheck(t *testing.T) {
	f := setup(t)
	f.Tag("logger/v0.3.0")
	f.Write("logger/README.md", "# logger, reworded\n")

	r := run(t, f)

	assert.Equal(t, []string{"logger changed without a VERSION bump: still 0.3.0, as on main"}, messages(r))
}

func TestEveryModuleThatCantBeCheckedIsReported(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.3.1\n")
	f.AddModule("metrics", "0.1.0")
	f.Commit("Change")

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	var builds atomic.Int64

	r, err := check.Run(t.Context(), check.Options{
		Repo:      repo,
		Workspace: ws,
		Base:      "main",
		Jobs:      2,
		Gorelease: func(context.Context) (gorelease.Runner, error) {
			builds.Add(1)
			return gorelease.Runner{}, errBuild
		},
	})

	require.ErrorIs(t, err, errBuild)
	require.ErrorContains(t, err, "checking logger: "+errBuild.Error())
	require.ErrorContains(t, err, "checking metrics: "+errBuild.Error())
	assert.Equal(t, check.Report{}, r)
	assert.Equal(t, int64(1), builds.Load(), "gorelease is built once, whichever module needs it first")
}

// released returns a repository whose go command fetches fixture modules
// locally, with logger released at v on main, checked out on a feature
// branch.
func released(t *testing.T, v string) fixture.Repo {
	t.Helper()

	f := fixture.New(t)
	f.ServeModules()
	f.AddModule("logger", v)
	f.Commit("Release logger")
	f.Tag("logger/v" + v)
	f.Git("switch", "--quiet", "--create", "feature")

	return f
}

// runAPI commits the working tree and checks the branch against main with
// the API comparison.
func runAPI(t *testing.T, f fixture.Repo, runner gorelease.Runner) check.Report {
	t.Helper()

	f.Commit("Change")

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	r, err := check.Run(t.Context(), check.Options{
		Repo:      repo,
		Workspace: ws,
		Base:      "main",
		Gorelease: func(context.Context) (gorelease.Runner, error) { return runner, nil },
		Jobs:      4,
	})
	require.NoError(t, err)

	return r
}

func loggerSource(exports ...string) string {
	var b strings.Builder

	b.WriteString("package logger\n")

	for _, name := range exports {
		fmt.Fprintf(&b, "\n// %s says hello.\nfunc %s() string { return \"hello\" }\n", name, name)
	}

	return b.String()
}

func TestAPIMinimum(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs gorelease")
	}

	toolDir, err := filepath.Abs("..")
	require.NoError(t, err)

	runner, err := gorelease.Build(t.Context(), toolDir, t.TempDir())
	require.NoError(t, err)

	t.Run("no API change", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/README.md", "# logger, reworded\n")
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		require.Len(t, r.Modules, 1)
		assert.Equal(t, "v0.3.0", r.Modules[0].APIBase)
		assert.Contains(t, r.Modules[0].APIReport, "Suggested version: v0.3.1")
	})

	t.Run("new export with a patch bump", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/logger.go", loggerSource("Hello", "Goodbye"))
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		assert.Equal(
			t,
			[]string{"logger's API changes since v0.3.0 need at least 0.4.0, but VERSION is 0.3.1"},
			messages(r),
		)
		assert.Equal(t, "logger/VERSION", r.Problems[0].File)
	})

	t.Run("breaking change before 1.0", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/logger.go", loggerSource())
		f.Write("logger/VERSION", "0.4.0\n")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		assert.Contains(t, r.Markdown(), "## incompatible changes\nHello: removed")
	})

	t.Run("breaking change after 1.0", func(t *testing.T) {
		for _, v := range []string{"1.2.1", "1.3.0"} {
			t.Run(v, func(t *testing.T) {
				f := released(t, "1.2.0")
				f.Write("logger/logger.go", loggerSource())
				f.Write("logger/VERSION", v+"\n")

				r := runAPI(t, f, runner)

				assert.Equal(t, []string{
					"logger has incompatible API changes since v1.2.0, which need a new major module path (logger/v2)",
				}, messages(r))
				assert.Equal(t, "logger/go.mod", r.Problems[0].File)
			})
		}
	})

	t.Run("breaking change after 1.0, VERSION 2.0.0 in place", func(t *testing.T) {
		f := released(t, "1.2.0")
		f.Write("logger/logger.go", loggerSource())
		f.Write("logger/VERSION", "2.0.0\n")

		r := runAPI(t, f, runner)

		require.Len(t, r.Problems, 1)
		assert.Contains(t, r.Problems[0].Message, "v2 lives in logger/v2")
		assert.Empty(t, r.Modules[0].APIReport, "the major check fails before gorelease runs")
	})

	t.Run("new major version directory", func(t *testing.T) {
		f := released(t, "1.2.0")
		f.AddModule("logger/v2", "2.0.0")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		require.Len(t, r.Modules, 1)
		assert.Equal(t, gorelease.BaseNone, r.Modules[0].APIBase)
		assert.Contains(t, r.Modules[0].APIReport, "Suggested version: v2.0.0 (with tag logger/v2.0.0)")
	})

	t.Run("unpublished sibling requirement", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Git("switch", "--quiet", "main")
		f.AddModule("kernel", "0.1.0")
		f.Commit("Release kernel")
		f.Tag("kernel/v0.1.0")
		f.Git("switch", "--quiet", "--force-create", "feature")
		f.Write("logger/go.mod", fmt.Sprintf(
			"module %s/logger\n\ngo 1.27\n\nrequire %s/kernel v0.2.0\n", fixture.ModulePrefix, fixture.ModulePrefix))
		f.Write("logger/logger.go", fmt.Sprintf(
			"package logger\n\nimport %q\n\n// Hello says hello.\nfunc Hello() string { return kernel.Hello() }\n",
			fixture.ModulePrefix+"/kernel"))
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		require.Len(t, r.Problems, 1)
		assert.Equal(t, "logger/go.mod", r.Problems[0].File)
		assert.Equal(t, "the API comparison against v0.3.0 failed", r.Problems[0].Message)
		assert.Contains(t, r.Problems[0].Detail, "kernel@v0.2.0")
	})

	t.Run("the comparison itself fails", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/broken.go", "package logger\n\nfunc broken() { undefined() }\n")
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		require.Len(t, r.Problems, 1)
		assert.Equal(t, "the API comparison against v0.3.0 failed", r.Problems[0].Message)
		assert.Contains(t, r.Problems[0].Detail, "undefined: undefined")
		assert.Contains(t, r.Markdown(), "undefined: undefined")
	})

	t.Run("module with no release", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.AddModule("metrics", "0.1.0")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		require.Len(t, r.Modules, 1)
		assert.Equal(t, gorelease.BaseNone, r.Modules[0].APIBase)
	})
}
